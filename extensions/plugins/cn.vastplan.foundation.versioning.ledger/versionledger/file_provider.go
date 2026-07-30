package versionledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

type FileProvider struct {
	mu   sync.RWMutex
	root string
	now  func() time.Time
}

func OpenFileProvider(root string) (*FileProvider, error) {
	return openFileProvider(root, func() time.Time { return time.Now().UTC() })
}

func openFileProvider(root string, now func() time.Time) (*FileProvider, error) {
	root, err := ensureProviderRoot(root)
	if err != nil {
		return nil, err
	}
	if now == nil {
		return nil, errors.New("File Version Provider clock 不能为空")
	}
	return &FileProvider{root: root, now: now}, nil
}

func (p *FileProvider) Descriptor() versioningv1.ProviderDescriptor {
	return versioningv1.ProviderDescriptor{
		ID: "file", Protocol: versioningv1.StorageProtocolFile, Version: "1.0.0", DisplayName: "Local file",
		Consistency: versioningv1.ConsistencySingleWriter, Durability: versioningv1.DurabilityLocal,
		MaxContentBytes:     versioningv1.MaxContentBytes,
		ConfigurationSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["root"],"properties":{"root":{"type":"string","minLength":1}}}`),
		Capabilities:        versioningv1.ProviderCapabilities{DetachedVersions: true, NamedHeads: true, StableHistory: true},
	}
}

func (p *FileProvider) PutVersion(ctx context.Context, scope Scope, request versioningv1.ProviderPutVersionRequest) (versioningv1.PutVersionResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.PutVersionResult{}, err
	}
	if err := validateProviderRequest(scope, &request); err != nil {
		return versioningv1.PutVersionResult{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	streamDir, err := p.streamDirectory(scope, request.Candidate.Stream, true)
	if err != nil {
		return versioningv1.PutVersionResult{}, fileProviderError(err)
	}
	loaded, err := loadFileStream(streamDir, request.Candidate.Stream)
	if err != nil {
		return versioningv1.PutVersionResult{}, providerError(versioningv1.ErrorCorrupted, false, err)
	}
	versionID := deterministicVersionID(scope, request.Candidate.Stream, request.IdempotencyKey)
	if record, exists := loaded.versions[versionID]; exists {
		if !sameCandidate(record, request.Candidate) {
			return versioningv1.PutVersionResult{}, providerError(versioningv1.ErrorConflict, false, errors.New("idempotencyKey 已绑定不同版本候选"))
		}
		return versioningv1.PutVersionResult{Version: cloneRecord(record), Reused: true}, nil
	}
	if loaded.sequence == math.MaxUint64 {
		return versioningv1.PutVersionResult{}, providerError(versioningv1.ErrorLimitExceeded, false, errors.New("版本 sequence 已耗尽"))
	}
	next := loaded.sequence + 1
	if err := requireStoredParent(loaded.versions, next, request.Candidate); err != nil {
		return versioningv1.PutVersionResult{}, err
	}
	contentDigest, err := versioningv1.ContentDigest(request.Candidate.Content)
	if err != nil {
		return versioningv1.PutVersionResult{}, providerError(versioningv1.ErrorInvalidRequest, false, err)
	}
	record := versioningv1.VersionRecord{
		Protocol: versioningv1.Protocol,
		Ref:      versioningv1.VersionRef{Stream: request.Candidate.Stream, VersionID: versionID, Sequence: next, ContentDigest: contentDigest},
		Parent:   request.Candidate.Parent, Content: append([]byte(nil), request.Candidate.Content...), Message: request.Candidate.Message,
		Labels: cloneLabels(request.Candidate.Labels), ActorID: request.Candidate.ActorID, CreatedAt: p.now(),
	}
	if err := versioningv1.ValidateVersionRecord(record); err != nil {
		return versioningv1.PutVersionResult{}, providerError(versioningv1.ErrorCorrupted, false, err)
	}
	digest, err := candidateDigest(request.Candidate)
	if err != nil {
		return versioningv1.PutVersionResult{}, providerError(versioningv1.ErrorInvalidRequest, false, err)
	}
	versionsDir, err := secureChildDirectory(streamDir, "versions")
	if err != nil {
		return versioningv1.PutVersionResult{}, fileProviderError(err)
	}
	finalName := versionID + ".json"
	if _, err := os.Lstat(filepath.Join(versionsDir, finalName)); err == nil {
		return versioningv1.PutVersionResult{}, providerError(versioningv1.ErrorConflict, false, errors.New("版本文件已存在但未进入有效索引"))
	} else if !errors.Is(err, os.ErrNotExist) {
		return versioningv1.PutVersionResult{}, fileProviderError(err)
	}
	envelope := fileVersionEnvelope{FormatVersion: fileProviderFormatVersion, CandidateDigest: digest, Record: record}
	if err := writeCreateOnlyJSON(versionsDir, ".version-*", finalName, envelope); err != nil {
		return versioningv1.PutVersionResult{}, fileProviderError(err)
	}
	return versioningv1.PutVersionResult{Version: cloneRecord(record)}, nil
}

func (p *FileProvider) GetVersion(ctx context.Context, scope Scope, request versioningv1.GetVersionRequest) (versioningv1.GetVersionResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.GetVersionResult{}, err
	}
	if err := validateScopedRequest(scope, versioningv1.OperationGetVersion, request); err != nil {
		return versioningv1.GetVersionResult{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	loaded, err := p.loadExistingStream(scope, request.Ref.Stream)
	if err != nil {
		return versioningv1.GetVersionResult{}, err
	}
	record, ok := loaded.versions[request.Ref.VersionID]
	if !ok || record.Ref != request.Ref {
		return versioningv1.GetVersionResult{}, providerError(versioningv1.ErrorNotFound, false, errors.New("版本不存在"))
	}
	return versioningv1.GetVersionResult{Version: cloneRecord(record)}, nil
}

func (p *FileProvider) ListHistory(ctx context.Context, scope Scope, request versioningv1.ListHistoryRequest) (versioningv1.ListHistoryResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.ListHistoryResult{}, err
	}
	if err := validateScopedRequest(scope, versioningv1.OperationListHistory, request); err != nil {
		return versioningv1.ListHistoryResult{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	loaded, err := p.loadExistingStream(scope, request.Stream)
	if err != nil {
		if providerCode(err) == versioningv1.ErrorNotFound {
			return versioningv1.ListHistoryResult{Versions: []versioningv1.VersionRecord{}}, nil
		}
		return versioningv1.ListHistoryResult{}, err
	}
	if len(loaded.versions) == 0 {
		return versioningv1.ListHistoryResult{Versions: []versioningv1.VersionRecord{}}, nil
	}
	current, ok := historyStart(loaded.versions, request)
	if !ok {
		return versioningv1.ListHistoryResult{}, providerError(versioningv1.ErrorNotFound, false, errors.New("历史起点不存在"))
	}
	result := versioningv1.ListHistoryResult{Versions: make([]versioningv1.VersionRecord, 0, request.Limit)}
	for len(result.Versions) < request.Limit {
		result.Versions = append(result.Versions, cloneRecord(current))
		if current.Parent == nil {
			break
		}
		next, exists := loaded.versions[current.Parent.VersionID]
		if !exists || next.Ref != *current.Parent {
			return versioningv1.ListHistoryResult{}, providerError(versioningv1.ErrorCorrupted, false, errors.New("版本父链损坏"))
		}
		if len(result.Versions) == request.Limit {
			result.NextCursor = next.Ref.VersionID
			break
		}
		current = next
	}
	return result, nil
}

func (p *FileProvider) GetHead(ctx context.Context, scope Scope, request versioningv1.GetHeadRequest) (versioningv1.GetHeadResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.GetHeadResult{}, err
	}
	if err := validateScopedRequest(scope, versioningv1.OperationGetHead, request); err != nil {
		return versioningv1.GetHeadResult{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	streamDir, err := p.streamDirectory(scope, request.Stream, false)
	if err != nil {
		return versioningv1.GetHeadResult{}, notFoundOrFileError(err, "Version Head 不存在")
	}
	loaded, err := loadFileStream(streamDir, request.Stream)
	if err != nil {
		return versioningv1.GetHeadResult{}, providerError(versioningv1.ErrorCorrupted, false, err)
	}
	head, err := readFileHead(streamDir, request)
	if err != nil {
		return versioningv1.GetHeadResult{}, err
	}
	if target, ok := loaded.versions[head.Target.VersionID]; !ok || target.Ref != head.Target {
		return versioningv1.GetHeadResult{}, providerError(versioningv1.ErrorCorrupted, false, errors.New("Version Head 指向不存在的版本"))
	}
	return versioningv1.GetHeadResult{Head: head}, nil
}

func (p *FileProvider) MoveHead(ctx context.Context, scope Scope, request versioningv1.MoveHeadRequest) (versioningv1.MoveHeadResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.MoveHeadResult{}, err
	}
	if err := validateScopedRequest(scope, versioningv1.OperationMoveHead, request); err != nil {
		return versioningv1.MoveHeadResult{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	streamDir, err := p.streamDirectory(scope, request.Stream, false)
	if err != nil {
		return versioningv1.MoveHeadResult{}, notFoundOrFileError(err, "目标 stream 不存在")
	}
	loaded, err := loadFileStream(streamDir, request.Stream)
	if err != nil {
		return versioningv1.MoveHeadResult{}, providerError(versioningv1.ErrorCorrupted, false, err)
	}
	target, ok := loaded.versions[request.Target.VersionID]
	if !ok || target.Ref != request.Target {
		return versioningv1.MoveHeadResult{}, providerError(versioningv1.ErrorNotFound, false, errors.New("Head 目标版本不存在"))
	}
	current, exists, err := readOptionalFileHead(streamDir, request.Stream, request.Name)
	if err != nil {
		return versioningv1.MoveHeadResult{}, err
	}
	if exists {
		if currentTarget, ok := loaded.versions[current.Target.VersionID]; !ok || currentTarget.Ref != current.Target {
			return versioningv1.MoveHeadResult{}, providerError(versioningv1.ErrorCorrupted, false, errors.New("当前 Version Head 指向不存在的版本"))
		}
	}
	if (!exists && request.ExpectedRevision != 0) || (exists && current.Revision != request.ExpectedRevision) {
		return versioningv1.MoveHeadResult{}, providerError(versioningv1.ErrorConflict, false, errors.New("Version Head CAS 冲突"))
	}
	revision := uint64(1)
	if exists {
		if current.Revision == math.MaxUint64 {
			return versioningv1.MoveHeadResult{}, providerError(versioningv1.ErrorLimitExceeded, false, errors.New("Head revision 已耗尽"))
		}
		revision = current.Revision + 1
	}
	head := versioningv1.Head{Protocol: versioningv1.Protocol, Stream: request.Stream, Name: request.Name, Target: request.Target, Revision: revision, UpdatedAt: p.now()}
	headsDir, err := secureChildDirectory(streamDir, "heads")
	if err != nil {
		return versioningv1.MoveHeadResult{}, fileProviderError(err)
	}
	if err := writeAtomicJSON(headsDir, ".head-*", request.Name+".json", fileHeadEnvelope{FormatVersion: fileProviderFormatVersion, Head: head}); err != nil {
		return versioningv1.MoveHeadResult{}, fileProviderError(err)
	}
	return versioningv1.MoveHeadResult{Head: head}, nil
}

func (p *FileProvider) loadExistingStream(scope Scope, stream versioningv1.StreamKey) (loadedFileStream, error) {
	streamDir, err := p.streamDirectory(scope, stream, false)
	if err != nil {
		return loadedFileStream{}, notFoundOrFileError(err, "版本 stream 不存在")
	}
	loaded, err := loadFileStream(streamDir, stream)
	if err != nil {
		return loadedFileStream{}, providerError(versioningv1.ErrorCorrupted, false, err)
	}
	return loaded, nil
}

func readFileHead(streamDir string, request versioningv1.GetHeadRequest) (versioningv1.Head, error) {
	head, exists, err := readOptionalFileHead(streamDir, request.Stream, request.Name)
	if err != nil {
		return versioningv1.Head{}, err
	}
	if !exists {
		return versioningv1.Head{}, providerError(versioningv1.ErrorNotFound, false, errors.New("Version Head 不存在"))
	}
	return head, nil
}

func readOptionalFileHead(streamDir string, stream versioningv1.StreamKey, name string) (versioningv1.Head, bool, error) {
	headsDir := filepath.Join(streamDir, "heads")
	if err := validatePrivateDirectory(headsDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return versioningv1.Head{}, false, nil
		}
		return versioningv1.Head{}, false, fileProviderError(err)
	}
	var envelope fileHeadEnvelope
	if err := readPrivateJSON(filepath.Join(headsDir, name+".json"), maxHeadFileBytes, &envelope); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return versioningv1.Head{}, false, nil
		}
		return versioningv1.Head{}, false, providerError(versioningv1.ErrorCorrupted, false, err)
	}
	if envelope.FormatVersion != fileProviderFormatVersion || envelope.Head.Stream != stream || envelope.Head.Name != name {
		return versioningv1.Head{}, false, providerError(versioningv1.ErrorCorrupted, false, errors.New("Version Head 文件身份不一致"))
	}
	if err := versioningv1.ValidateHead(envelope.Head); err != nil {
		return versioningv1.Head{}, false, providerError(versioningv1.ErrorCorrupted, false, err)
	}
	return envelope.Head, true, nil
}

func fileProviderError(err error) error {
	return providerError(versioningv1.ErrorProviderUnavailable, true, fmt.Errorf("File Version Provider: %w", err))
}

func notFoundOrFileError(err error, message string) error {
	if errors.Is(err, os.ErrNotExist) {
		return providerError(versioningv1.ErrorNotFound, false, errors.New(message))
	}
	return fileProviderError(err)
}

func providerCode(err error) string {
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Code
	}
	return ""
}
