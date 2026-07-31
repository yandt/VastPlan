package contentstaging

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
)

const (
	uploadRecordFormatVersion     = 1
	protectionRecordFormatVersion = 1
)

type ManagerOptions struct {
	Limits Limits
	Now    func() time.Time
}

type Manager struct {
	mu          sync.Mutex
	provider    Provider
	admission   Admission
	limits      Limits
	now         func() time.Time
	uploads     map[string]*uploadRecord
	protections map[string]*protectionRecord
	writers     map[string]struct{}
}

func NewManager(ctx context.Context, provider Provider, admission Admission, options ManagerOptions) (*Manager, error) {
	if provider == nil || admission == nil {
		return nil, errors.New("Content Staging Provider 与 Admission 不能为空")
	}
	if err := options.Limits.Validate(); err != nil {
		return nil, err
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	records, err := provider.LoadUploads(ctx)
	if err != nil {
		return nil, fmt.Errorf("加载 Content Staging 状态: %w", err)
	}
	protectionRecords, err := provider.LoadProtections(ctx)
	if err != nil {
		return nil, fmt.Errorf("加载 Content Reference 状态: %w", err)
	}
	manager := &Manager{provider: provider, admission: admission, limits: options.Limits, now: options.Now, uploads: map[string]*uploadRecord{}, protections: map[string]*protectionRecord{}, writers: map[string]struct{}{}}
	for _, record := range records {
		if err := validateStoredRecord(record); err != nil {
			return nil, fmt.Errorf("加载 Content Staging Lease %q: %w", record.Upload.ID, err)
		}
		if _, duplicate := manager.uploads[record.Upload.ID]; duplicate {
			return nil, fmt.Errorf("Content Staging Lease %q 重复", record.Upload.ID)
		}
		copy := cloneRecord(record)
		manager.uploads[record.Upload.ID] = &copy
	}
	for _, record := range protectionRecords {
		if err := validateStoredProtection(record); err != nil {
			return nil, fmt.Errorf("加载 Content Reference %q: %w", record.Protection.ID, err)
		}
		if _, duplicate := manager.protections[record.Protection.ID]; duplicate {
			return nil, fmt.Errorf("Content Reference %q 重复", record.Protection.ID)
		}
		copy := cloneProtectionRecord(record)
		manager.protections[record.Protection.ID] = &copy
	}
	if _, err := manager.Reclaim(ctx); err != nil {
		return nil, fmt.Errorf("启动时回收 Content Staging Lease: %w", err)
	}
	if err := manager.verifyReadyContent(ctx); err != nil {
		return nil, err
	}
	return manager, nil
}

func (m *Manager) Begin(ctx context.Context, scope Scope, idempotencyKey string, request stagingv1.BeginUploadRequest) (stagingv1.UploadStatusResult, error) {
	if err := scope.Validate(); err != nil {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorInvalidRequest, false, err)
	}
	if strings.TrimSpace(idempotencyKey) == "" || len(idempotencyKey) > 160 {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorInvalidRequest, false, errors.New("开始上传必须提供有界 idempotency key"))
	}
	if err := stagingv1.ValidateBeginUploadRequest(request); err != nil {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorInvalidRequest, false, err)
	}
	if request.ExpectedSize > m.limits.MaxFileBytes || request.LeaseSeconds > m.limits.MaxLeaseSeconds {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorLimitExceeded, false, errors.New("上传声明超过服务限制"))
	}
	now := m.now()
	beginDigest := beginOperationDigest(scope, idempotencyKey)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.uploads {
		if existing.Owner == scope && existing.BeginDigest == beginDigest {
			if !sameBeginRequest(existing, request) {
				return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorLeaseConflict, false, errors.New("idempotency key 已绑定不同上传声明"))
			}
			return existing.result(), nil
		}
	}
	if err := m.requireCapacityLocked(scope.TenantID, request.ExpectedSize); err != nil {
		return stagingv1.UploadStatusResult{}, err
	}
	id, err := m.uniqueUploadIDLocked()
	if err != nil {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorStorageUnavailable, true, err)
	}
	record := uploadRecord{
		FormatVersion:     uploadRecordFormatVersion,
		Owner:             scope,
		BeginDigest:       beginDigest,
		SessionRevision:   request.ExpectedSessionRevision,
		BeginLeaseSeconds: request.LeaseSeconds,
		Upload: stagingv1.UploadLease{
			Protocol: stagingv1.Protocol, ID: id, SessionID: request.SessionID, EnvironmentDigest: request.EnvironmentDigest,
			Resource: request.Resource, Path: request.Path, MediaType: request.MediaType, ExpectedDigest: request.ExpectedDigest,
			ExpectedSize: request.ExpectedSize, State: stagingv1.StatePending, Revision: 1,
			CreatedAt: now, UpdatedAt: now, LeaseExpiresAt: now.Add(time.Duration(request.LeaseSeconds) * time.Second),
		},
	}
	if err := m.provider.SaveUpload(ctx, record); err != nil {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorStorageUnavailable, true, err)
	}
	m.uploads[id] = &record
	return record.result(), nil
}

func (m *Manager) Status(ctx context.Context, scope Scope, request stagingv1.UploadStatusRequest) (stagingv1.UploadStatusResult, error) {
	if err := scope.Validate(); err != nil {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorInvalidRequest, false, err)
	}
	if err := stagingv1.ValidateUploadStatusRequest(request); err != nil {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorInvalidRequest, false, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.ownedRecordLocked(scope, request.UploadID)
	if err != nil {
		return stagingv1.UploadStatusResult{}, err
	}
	if _, err := m.expireIfNeededLocked(ctx, record); err != nil {
		return stagingv1.UploadStatusResult{}, err
	}
	return record.result(), nil
}

func (m *Manager) Renew(ctx context.Context, scope Scope, request stagingv1.RenewUploadRequest) (stagingv1.UploadStatusResult, error) {
	if err := scope.Validate(); err != nil {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorInvalidRequest, false, err)
	}
	if err := stagingv1.ValidateRenewUploadRequest(request); err != nil {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorInvalidRequest, false, err)
	}
	if request.LeaseSeconds > m.limits.MaxLeaseSeconds {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorLimitExceeded, false, errors.New("续租超过服务上限"))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.ownedRecordLocked(scope, request.UploadID)
	if err != nil {
		return stagingv1.UploadStatusResult{}, err
	}
	if expired, err := m.expireIfNeededLocked(ctx, record); err != nil {
		return stagingv1.UploadStatusResult{}, err
	} else if expired {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorLeaseExpired, false, errors.New("Content Staging Lease 已过期"))
	}
	if !protectedState(record.Upload.State) || record.Upload.State == stagingv1.StateVerifying {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorLeaseConflict, false, errors.New("当前 Upload 状态不能续租"))
	}
	if record.Upload.Revision != request.ExpectedRevision {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorLeaseConflict, false, errors.New("Upload revision CAS 冲突"))
	}
	before := cloneRecord(*record)
	record.Upload.Revision++
	record.Upload.UpdatedAt = m.now()
	record.Upload.LeaseExpiresAt = record.Upload.UpdatedAt.Add(time.Duration(request.LeaseSeconds) * time.Second)
	if err := m.saveOrRollbackLocked(ctx, record, before); err != nil {
		return stagingv1.UploadStatusResult{}, err
	}
	return record.result(), nil
}

func (m *Manager) ownedRecordLocked(scope Scope, id string) (*uploadRecord, error) {
	record, ok := m.uploads[id]
	if !ok || record.Owner != scope {
		return nil, stagingError(stagingv1.ErrorLeaseNotFound, false, errors.New("Content Staging Lease 不存在"))
	}
	return record, nil
}

func (m *Manager) saveOrRollbackLocked(ctx context.Context, record *uploadRecord, before uploadRecord) error {
	if err := m.provider.SaveUpload(ctx, *record); err != nil {
		*record = before
		return stagingError(stagingv1.ErrorStorageUnavailable, true, err)
	}
	return nil
}

func (m *Manager) requireCapacityLocked(tenantID string, requested int64) error {
	var tenantBytes, totalBytes int64
	active := 0
	for _, record := range m.uploads {
		if !protectedState(record.Upload.State) {
			continue
		}
		totalBytes += record.Upload.ExpectedSize
		if record.Owner.TenantID == tenantID {
			tenantBytes += record.Upload.ExpectedSize
			if activeUploadState(record.Upload.State) {
				active++
			}
		}
	}
	if active >= m.limits.MaxActiveUploadsPerTenant || requested > m.limits.MaxTenantBytes-tenantBytes || requested > m.limits.MaxTotalBytes-totalBytes {
		return stagingError(stagingv1.ErrorLimitExceeded, false, errors.New("Content Staging 容量或并发配额已满"))
	}
	return nil
}

func (m *Manager) uniqueUploadIDLocked() (string, error) {
	for attempt := 0; attempt < 8; attempt++ {
		raw := make([]byte, 24)
		if _, err := rand.Read(raw); err != nil {
			return "", err
		}
		id := "stg_" + base64.RawURLEncoding.EncodeToString(raw)
		if _, exists := m.uploads[id]; !exists {
			return id, nil
		}
	}
	return "", errors.New("无法分配唯一 Upload ID")
}

func beginOperationDigest(scope Scope, idempotencyKey string) string {
	sum := sha256.Sum256([]byte(scope.TenantID + "\x00" + scope.ActorID + "\x00" + idempotencyKey))
	return hex.EncodeToString(sum[:])
}

func sameBeginRequest(record *uploadRecord, request stagingv1.BeginUploadRequest) bool {
	upload := record.Upload
	return record.SessionRevision == request.ExpectedSessionRevision && record.BeginLeaseSeconds == request.LeaseSeconds &&
		upload.SessionID == request.SessionID && upload.EnvironmentDigest == request.EnvironmentDigest && upload.Resource == request.Resource &&
		upload.Path == request.Path && upload.MediaType == request.MediaType && upload.ExpectedDigest == request.ExpectedDigest && upload.ExpectedSize == request.ExpectedSize
}
