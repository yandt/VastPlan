package contentstaging

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const maxUploadRecordBytes = 64 << 10

type FileProvider struct {
	mu   sync.Mutex
	root string
}

func OpenFileProvider(root string) (*FileProvider, error) {
	root, err := ensurePrivateRoot(root)
	if err != nil {
		return nil, err
	}
	return &FileProvider{root: root}, nil
}

func (p *FileProvider) LoadUploads(ctx context.Context) ([]uploadRecord, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	tenants := filepath.Join(p.root, "tenants")
	entries, err := os.ReadDir(tenants)
	if errors.Is(err, os.ErrNotExist) {
		return []uploadRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	if err := validatePrivateDirectory(tenants); err != nil {
		return nil, err
	}
	records := []uploadRecord{}
	for _, tenant := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !tenant.IsDir() || len(tenant.Name()) != 64 || !isLowerHex(tenant.Name()) {
			return nil, fmt.Errorf("Content Staging tenants 包含无效条目 %q", tenant.Name())
		}
		uploads := filepath.Join(tenants, tenant.Name(), "uploads")
		items, err := os.ReadDir(uploads)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := validateExistingDirectoryChain(p.root, []string{"tenants", tenant.Name(), "uploads"}); err != nil {
			return nil, err
		}
		for _, item := range items {
			if strings.HasPrefix(item.Name(), ".upload-") {
				continue
			}
			if item.IsDir() || !strings.HasPrefix(item.Name(), "stg_") || !strings.HasSuffix(item.Name(), ".json") {
				return nil, fmt.Errorf("Content Staging uploads 包含无效条目 %q", item.Name())
			}
			var record uploadRecord
			if err := readPrivateJSON(filepath.Join(uploads, item.Name()), maxUploadRecordBytes, &record); err != nil {
				return nil, err
			}
			if item.Name() != record.Upload.ID+".json" || pathDigest(record.Owner.TenantID) != tenant.Name() {
				return nil, errors.New("Content Staging Upload 文件身份不一致")
			}
			records = append(records, record)
		}
	}
	return records, nil
}

func (p *FileProvider) SaveUpload(ctx context.Context, record uploadRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateStoredRecord(record); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	directory, err := p.tenantChild(record.Owner, "uploads", true)
	if err != nil {
		return err
	}
	return writeAtomicJSON(directory, ".upload-*", record.Upload.ID+".json", record)
}

func (p *FileProvider) DeleteUpload(ctx context.Context, scope Scope, uploadID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateUploadID(uploadID); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	directory, err := p.tenantChild(scope, "uploads", false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return removeRegularFile(filepath.Join(directory, uploadID+".json"))
}
