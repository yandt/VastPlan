package credentials

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) StageManaged(ctx context.Context, call *contractv1.CallContext, purpose, resource string, value []byte) (pluginconfig.StagedCredential, error) {
	t, err := tenant(call)
	if err != nil {
		return pluginconfig.StagedCredential{}, err
	}
	owner, err := managedOwner(call)
	if err != nil {
		return pluginconfig.StagedCredential{}, err
	}
	if strings.TrimSpace(purpose) == "" || strings.TrimSpace(resource) == "" || len(purpose) > 160 || len(resource) > 320 || len(value) == 0 || len(value) > 4<<20 {
		return pluginconfig.StagedCredential{}, errors.New("托管凭证 purpose、resource 和 value 均不能为空")
	}
	ciphertext, err := s.transit.Encrypt(ctx, value)
	if err != nil {
		return pluginconfig.StagedCredential{}, err
	}
	stageID, err := opaqueID("stage-")
	if err != nil {
		return pluginconfig.StagedCredential{}, err
	}
	handle, err := opaqueID("credential://managed/")
	if err != nil {
		return pluginconfig.StagedCredential{}, err
	}
	now := s.now().UTC()
	ref := pluginconfig.ManagedCredentialRef{Handle: handle, Scope: "tenant", Owner: owner, Purpose: purpose, Version: 1}
	record := ManagedRecord{StageID: stageID, Ref: ref, Resource: resource, State: managedPreparing, CreatedAt: now, UpdatedAt: now, Ciphertext: ciphertext}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.managedRecords(t)[stageID] = record
	previousAudit := cloneManagedAuditState(s.managedAuditStateLocked(t))
	s.appendManagedAuditLocked(t, "managed.staged", record, now)
	if err := s.save(); err != nil {
		delete(s.managedRecords(t), stageID)
		s.data.ManagedAudit[t] = previousAudit
		return pluginconfig.StagedCredential{}, err
	}
	return pluginconfig.StagedCredential{ID: stageID, Ref: ref}, nil
}

func (s *Service) managedTransition(call *contractv1.CallContext, stageID, target string) (pluginconfig.ManagedCredentialRef, error) {
	t, err := tenant(call)
	if err != nil {
		return pluginconfig.ManagedCredentialRef{}, err
	}
	owner, err := managedOwner(call)
	if err != nil {
		return pluginconfig.ManagedCredentialRef{}, err
	}
	if !strings.HasPrefix(stageID, "stage-") {
		return pluginconfig.ManagedCredentialRef{}, errors.New("托管凭证 stageId 无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.managedRecords(t)[stageID]
	if !ok {
		return pluginconfig.ManagedCredentialRef{}, os.ErrNotExist
	}
	if record.Ref.Owner != owner {
		return pluginconfig.ManagedCredentialRef{}, errors.New("托管凭证不属于当前插件")
	}
	if record.Coordinator != "" {
		return pluginconfig.ManagedCredentialRef{}, errors.New("委托凭证只能由配置协调器转换")
	}
	if record.State == target {
		return record.Ref, nil
	}
	switch target {
	case managedActive:
		if record.State != managedPreparing && record.State != managedActive {
			return pluginconfig.ManagedCredentialRef{}, errors.New("只有 Preparing 凭证可以激活")
		}
	case managedAborted:
		if record.State == managedActive || record.State == managedRetired {
			return pluginconfig.ManagedCredentialRef{}, errors.New("已激活凭证不能终止候选")
		}
		record.Ciphertext = ""
	default:
		return pluginconfig.ManagedCredentialRef{}, errors.New("未知托管凭证状态")
	}
	previousRecord := record
	previousAudit := cloneManagedAuditState(s.managedAuditStateLocked(t))
	record.State = target
	record.UpdatedAt = s.now().UTC()
	s.managedRecords(t)[stageID] = record
	action := "managed.activated"
	if target == managedAborted {
		action = "managed.aborted"
	}
	s.appendManagedAuditLocked(t, action, record, record.UpdatedAt)
	if err := s.save(); err != nil {
		s.managedRecords(t)[stageID] = previousRecord
		s.data.ManagedAudit[t] = previousAudit
		return pluginconfig.ManagedCredentialRef{}, err
	}
	return record.Ref, nil
}

func (s *Service) ActivateManaged(call *contractv1.CallContext, stageID string) (pluginconfig.ManagedCredentialRef, error) {
	return s.managedTransition(call, stageID, managedActive)
}

func (s *Service) AbortManaged(call *contractv1.CallContext, stageID string) (pluginconfig.ManagedCredentialRef, error) {
	return s.managedTransition(call, stageID, managedAborted)
}

func (s *Service) RetireManaged(call *contractv1.CallContext, handle string) (pluginconfig.ManagedCredentialRef, error) {
	t, err := tenant(call)
	if err != nil {
		return pluginconfig.ManagedCredentialRef{}, err
	}
	owner, err := managedOwner(call)
	if err != nil {
		return pluginconfig.ManagedCredentialRef{}, err
	}
	if !strings.HasPrefix(handle, "credential://managed/") || len(handle) > 256 {
		return pluginconfig.ManagedCredentialRef{}, errors.New("托管凭证 handle 无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, record := range s.managedRecords(t) {
		if record.Ref.Handle != handle {
			continue
		}
		if record.Ref.Owner != owner {
			return pluginconfig.ManagedCredentialRef{}, errors.New("托管凭证不可由当前插件退役")
		}
		if record.State == managedRetired {
			return record.Ref, nil
		}
		if record.State != managedActive {
			return pluginconfig.ManagedCredentialRef{}, errors.New("只有 Active 托管凭证可以退役")
		}
		previousRecord := record
		previousAudit := cloneManagedAuditState(s.managedAuditStateLocked(t))
		record.State, record.Ciphertext, record.UpdatedAt = managedRetired, "", s.now().UTC()
		s.managedRecords(t)[id] = record
		s.appendManagedAuditLocked(t, "managed.retired", record, record.UpdatedAt)
		if err := s.save(); err != nil {
			s.managedRecords(t)[id] = previousRecord
			s.data.ManagedAudit[t] = previousAudit
			return pluginconfig.ManagedCredentialRef{}, err
		}
		return record.Ref, nil
	}
	return pluginconfig.ManagedCredentialRef{}, os.ErrNotExist
}

// IssueMaterialLease decrypts only for an authenticated kernel identity and
// immediately reseals the material to the requester's one-use X25519 key.
// Plaintext is never returned in a protocol payload.
func (s *Service) IssueMaterialLease(ctx context.Context, call *contractv1.CallContext, request credentiallease.Request) (credentiallease.Envelope, error) {
	if err := credentiallease.ValidateRequest(request); err != nil {
		return credentiallease.Envelope{}, err
	}
	t, err := tenant(call)
	if err != nil {
		return credentiallease.Envelope{}, err
	}
	audience := strings.TrimSpace(call.GetCaller().GetId())
	if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_SYSTEM || audience == "" {
		return credentiallease.Envelope{}, errors.New("material lease 只接受已认证可信宿主")
	}
	select {
	case s.leaseSlots <- struct{}{}:
		defer func() { <-s.leaseSlots }()
	case <-ctx.Done():
		return credentiallease.Envelope{}, ctx.Err()
	}
	s.mu.Lock()
	var matched ManagedRecord
	for _, record := range s.managedRecords(t) {
		if record.Ref.Handle == request.Ref.Handle {
			matched = record
			break
		}
	}
	if matched.StageID == "" || (matched.State != managedCandidate && matched.State != managedActive) || matched.Ref != request.Ref || matched.Ciphertext == "" {
		s.mu.Unlock()
		return credentiallease.Envelope{}, errors.New("托管凭证不存在、未激活或引用不匹配")
	}
	ciphertext := matched.Ciphertext
	s.mu.Unlock()
	material, err := s.transit.Decrypt(ctx, ciphertext)
	if err != nil {
		return credentiallease.Envelope{}, err
	}
	defer func() {
		for index := range material {
			material[index] = 0
		}
	}()
	// A revoke/retire racing the KMS request wins. Shared State 模式下必须重新
	// 读取最新 Root，而不能只复核当前进程的缓存。
	s.mu.Lock()
	session := s.session
	s.mu.Unlock()
	current := ManagedRecord{}
	ok := false
	if session != nil {
		latest, _, loadErr := session.repository.load(ctx, call)
		if loadErr != nil {
			return credentiallease.Envelope{}, loadErr
		}
		current, ok = latest.Managed[matched.StageID]
	} else {
		s.mu.Lock()
		current, ok = s.managedRecords(t)[matched.StageID]
		s.mu.Unlock()
	}
	stillUsable := ok && (current.State == managedCandidate || current.State == managedActive) && current.Ref == matched.Ref && current.Ciphertext == ciphertext
	if !stillUsable {
		return credentiallease.Envelope{}, errors.New("托管凭证在 lease 签发期间已变化")
	}
	return credentiallease.Seal(request, credentiallease.Claims{TenantID: t, Audience: audience, Ref: matched.Ref}, material, time.Now().UTC(), credentiallease.DefaultTTL)
}

func decodeMaterialLeaseRequest(payload []byte) (credentiallease.Request, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var request credentiallease.Request
	if err := decoder.Decode(&request); err != nil {
		return credentiallease.Request{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return credentiallease.Request{}, errors.New("material lease 请求只能包含一个 JSON 文档")
	}
	return request, nil
}

func (s *Service) MaterialLeaseHandler(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte, operation string) (*contractv1.CallResult, []byte, error) {
	var result *contractv1.CallResult
	var raw []byte
	var handlerErr error
	err := s.withTenantState(ctx, host, call, func() error {
		result, raw, handlerErr = s.materialLeaseLoaded(ctx, call, payload, operation)
		return handlerErr
	})
	if err != nil {
		return credentialDomainError(err)
	}
	return result, raw, handlerErr
}

func (s *Service) materialLeaseLoaded(ctx context.Context, call *contractv1.CallContext, payload []byte, operation string) (*contractv1.CallResult, []byte, error) {
	if operation != "issue" {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "platform.credentials.material_lease.invalid", Message: "不支持的 material lease 操作"}}, nil, nil
	}
	request, err := decodeMaterialLeaseRequest(payload)
	if err != nil {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "platform.credentials.material_lease.invalid", Message: err.Error()}}, nil, nil
	}
	envelope, err := s.IssueMaterialLease(ctx, call, request)
	if err != nil {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "platform.credentials.material_lease.denied", Message: err.Error()}}, nil, nil
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}
