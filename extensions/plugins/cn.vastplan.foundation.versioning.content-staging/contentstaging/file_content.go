package contentstaging

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
)

func (p *FileProvider) WriteStaged(ctx context.Context, scope Scope, uploadID string, maxBytes int64, reader io.Reader) (WriteResult, error) {
	if maxBytes < 0 || reader == nil {
		return WriteResult{}, errors.New("Content Staging 写入参数无效")
	}
	if err := validateUploadID(uploadID); err != nil {
		return WriteResult{}, err
	}
	directory, err := p.tenantChild(scope, "staging", true)
	if err != nil {
		return WriteResult{}, err
	}
	temporary, err := os.CreateTemp(directory, ".stream-*")
	if err != nil {
		return WriteResult{}, err
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return WriteResult{}, err
	}
	hash := sha256.New()
	limited := io.LimitReader(contextReader{ctx: ctx, reader: reader}, maxBytes+1)
	written, copyErr := io.CopyBuffer(io.MultiWriter(temporary, hash), limited, make([]byte, 128<<10))
	if copyErr != nil {
		return WriteResult{}, copyErr
	}
	if written > maxBytes {
		return WriteResult{}, errStreamLimitExceeded
	}
	if err := errors.Join(temporary.Sync(), temporary.Close()); err != nil {
		return WriteResult{}, err
	}
	target := filepath.Join(directory, uploadID+".part")
	if err := rejectNonRegularTarget(target); err != nil {
		return WriteResult{}, err
	}
	if err := os.Rename(temporaryName, target); err != nil {
		return WriteResult{}, err
	}
	committed = true
	if err := syncDirectory(directory); err != nil {
		return WriteResult{}, err
	}
	return WriteResult{Size: written, Digest: hex.EncodeToString(hash.Sum(nil))}, nil
}

func (p *FileProvider) OpenStaged(ctx context.Context, scope Scope, uploadID string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateUploadID(uploadID); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	directory, err := p.tenantChild(scope, "staging", false)
	if err != nil {
		return nil, err
	}
	return openPrivateRegular(filepath.Join(directory, uploadID+".part"))
}

func (p *FileProvider) Promote(ctx context.Context, scope Scope, uploadID string, descriptor stagingv1.ContentDescriptor) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := stagingv1.ValidateContentDescriptor(descriptor); err != nil {
		return err
	}
	if err := validateUploadID(uploadID); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	staging, err := p.tenantChild(scope, "staging", false)
	if err != nil {
		return err
	}
	objects, err := p.tenantObjectDirectory(scope, true)
	if err != nil {
		return err
	}
	source := filepath.Join(staging, uploadID+".part")
	if err := verifyFile(source, descriptor.Digest, descriptor.Size); err != nil {
		return err
	}
	target := filepath.Join(objects, descriptor.Digest)
	if err := os.Link(source, target); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		if err := verifyFile(target, descriptor.Digest, descriptor.Size); err != nil {
			return fmt.Errorf("已有 CAS 对象与摘要身份冲突: %w", err)
		}
	}
	return syncDirectory(objects)
}

func (p *FileProvider) RemoveStaged(ctx context.Context, scope Scope, uploadID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateUploadID(uploadID); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	directory, err := p.tenantChild(scope, "staging", false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return removeRegularFile(filepath.Join(directory, uploadID+".part"))
}

func (p *FileProvider) VerifyContent(ctx context.Context, scope Scope, descriptor stagingv1.ContentDescriptor) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	directory, err := p.tenantObjectDirectory(scope, false)
	if err != nil {
		return err
	}
	return verifyFile(filepath.Join(directory, descriptor.Digest), descriptor.Digest, descriptor.Size)
}

func (p *FileProvider) OpenContent(ctx context.Context, scope Scope, descriptor stagingv1.ContentDescriptor) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := stagingv1.ValidateContentDescriptor(descriptor); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	directory, err := p.tenantObjectDirectory(scope, false)
	if err != nil {
		return nil, err
	}
	file, err := openPrivateRegular(filepath.Join(directory, descriptor.Digest))
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || info.Size() != descriptor.Size {
		_ = file.Close()
		return nil, errors.New("Content Staging CAS 对象大小不匹配")
	}
	return file, nil
}

func (p *FileProvider) RemoveContent(ctx context.Context, scope Scope, digest string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validSHA256(digest) {
		return errors.New("Content Staging 对象摘要无效")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	directory, err := p.tenantObjectDirectory(scope, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return removeRegularFile(filepath.Join(directory, digest))
}
