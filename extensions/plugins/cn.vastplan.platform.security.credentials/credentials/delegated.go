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

	"cdsoft.com.cn/VastPlan/core/shared/go/configurationauthority"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) StageDelegated(ctx context.Context, host sdk.Host, call *contractv1.CallContext, authorityToken string, value []byte) (pluginconfig.StagedCredential, error) {
	tenantID, err := tenant(call)
	if err != nil {
		return pluginconfig.StagedCredential{}, err
	}
	if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || call.GetCaller().GetId() != configurationauthority.CoordinatorPluginID || host == nil || len(value) == 0 || len(value) > 4<<20 {
		return pluginconfig.StagedCredential{}, errors.New("委托暂存只接受配置协调器和非空秘密")
	}
	claims, err := consumeConfigurationAuthority(ctx, host, call, authorityToken)
	if err != nil {
		return pluginconfig.StagedCredential{}, err
	}
	if err := claims.Validate(time.Now().UTC(), tenantID); err != nil || claims.Owner == PluginID || claims.Owner == configurationauthority.CoordinatorPluginID {
		return pluginconfig.StagedCredential{}, errors.New("配置授权 claims 无效")
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
	now := time.Now().UTC()
	ref := pluginconfig.ManagedCredentialRef{Handle: handle, Scope: "tenant", Owner: claims.Owner, Purpose: claims.Purpose, Version: 1}
	record := ManagedRecord{
		StageID: stageID, Ref: ref, Resource: claims.Resource, State: managedPreparing,
		CreatedAt: now, UpdatedAt: now, Ciphertext: ciphertext,
		AuthorityID: claims.AuthorityID, Coordinator: configurationauthority.CoordinatorPluginID,
		CandidateID: claims.CandidateID, ConfigurationID: claims.ConfigurationID, FieldID: claims.FieldID,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.managedRecords(tenantID)[stageID] = record
	if err := s.save(); err != nil {
		delete(s.managedRecords(tenantID), stageID)
		return pluginconfig.StagedCredential{}, err
	}
	return pluginconfig.StagedCredential{ID: stageID, Ref: ref}, nil
}

func consumeConfigurationAuthority(ctx context.Context, host sdk.Host, call *contractv1.CallContext, token string) (configurationauthority.Claims, error) {
	if !strings.HasPrefix(token, configurationauthority.TokenPrefix) || len(token) > 128 {
		return configurationauthority.Claims{}, configurationauthority.ErrInvalid
	}
	payload, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		return configurationauthority.Claims{}, err
	}
	defer zeroBytes(payload)
	operation := "consume"
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: configurationauthority.KernelConsumeService, Operation: &operation}, call, payload)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return configurationauthority.Claims{}, errors.New("配置授权消费失败")
	}
	var claims configurationauthority.Claims
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&claims); err != nil {
		return claims, errors.New("配置授权响应无效")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return claims, errors.New("配置授权响应包含多余内容")
	}
	return claims, nil
}

func (s *Service) delegatedTransition(call *contractv1.CallContext, stageID, candidateID, target string) (pluginconfig.ManagedCredentialRef, error) {
	tenantID, err := tenant(call)
	if err != nil {
		return pluginconfig.ManagedCredentialRef{}, err
	}
	if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || call.GetCaller().GetId() != configurationauthority.CoordinatorPluginID || !strings.HasPrefix(stageID, "stage-") || !strings.HasPrefix(candidateID, "pcfg_") {
		return pluginconfig.ManagedCredentialRef{}, errors.New("委托凭证转换请求无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.managedRecords(tenantID)[stageID]
	if !ok {
		return pluginconfig.ManagedCredentialRef{}, os.ErrNotExist
	}
	if record.Coordinator != configurationauthority.CoordinatorPluginID || record.CandidateID != candidateID {
		return pluginconfig.ManagedCredentialRef{}, errors.New("委托凭证不属于当前配置候选")
	}
	switch target {
	case managedActive:
		if record.State != managedPreparing && record.State != managedActive {
			return pluginconfig.ManagedCredentialRef{}, errors.New("只有 Preparing 委托凭证可以激活")
		}
	case managedAborted:
		if record.State == managedActive || record.State == managedRetired {
			return pluginconfig.ManagedCredentialRef{}, errors.New("已激活委托凭证不能终止候选")
		}
		record.Ciphertext = ""
	default:
		return pluginconfig.ManagedCredentialRef{}, errors.New("未知委托凭证状态")
	}
	record.State, record.UpdatedAt = target, time.Now().UTC()
	s.managedRecords(tenantID)[stageID] = record
	if err := s.save(); err != nil {
		return pluginconfig.ManagedCredentialRef{}, err
	}
	return record.Ref, nil
}

func (s *Service) ActivateDelegated(call *contractv1.CallContext, stageID, candidateID string) (pluginconfig.ManagedCredentialRef, error) {
	return s.delegatedTransition(call, stageID, candidateID, managedActive)
}

func (s *Service) AbortDelegated(call *contractv1.CallContext, stageID, candidateID string) (pluginconfig.ManagedCredentialRef, error) {
	return s.delegatedTransition(call, stageID, candidateID, managedAborted)
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
