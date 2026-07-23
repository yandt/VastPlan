package pluginsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/configurationauthority"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const credentialCapability = "platform.credentials"

func credentialStatuses(definition pluginconfiguration.Definition, secrets map[string]string) ([]pluginconfiguration.ManagedCredentialStatus, error) {
	return credentialStatusesFor(definition.ManagedCredentials, definition.CredentialStates, secrets)
}

func credentialStatusesFor(fields []pluginv1.ManagedCredentialField, states []pluginconfiguration.CredentialState, secrets map[string]string) ([]pluginconfiguration.ManagedCredentialStatus, error) {
	declared := make(map[string]bool, len(fields))
	configured := make(map[string]bool, len(states))
	for _, state := range states {
		configured[state.FieldID] = state.Configured
	}
	statuses := make([]pluginconfiguration.ManagedCredentialStatus, 0, len(fields))
	total := 0
	for _, field := range fields {
		declared[field.ID] = true
		value := secrets[field.ID]
		if field.Required && value == "" && !configured[field.ID] {
			return nil, fmt.Errorf("%w: 必须提供托管凭证字段 %s", ErrInvalid, field.ID)
		}
		if len(value) > 4<<20 {
			return nil, fmt.Errorf("%w: 托管凭证字段 %s 超过大小上限", ErrInvalid, field.ID)
		}
		total += len(value)
		state := "Missing"
		if configured[field.ID] {
			state = "Retained"
		}
		if value != "" {
			state = "Pending"
		}
		statuses = append(statuses, pluginconfiguration.ManagedCredentialStatus{FieldID: field.ID, State: state})
	}
	if total > 8<<20 {
		return nil, fmt.Errorf("%w: 托管凭证总大小超过上限", ErrInvalid)
	}
	for fieldID, value := range secrets {
		if !declared[fieldID] || value == "" {
			return nil, fmt.Errorf("%w: 未声明或空的托管凭证字段 %s", ErrInvalid, fieldID)
		}
	}
	return statuses, nil
}

func (s *Service) stageSecrets(ctx context.Context, host sdk.Host, call *contractv1.CallContext, definition pluginconfiguration.Definition, candidateID, catalogDigest string, secrets map[string]string, checkpoint func(string, pluginconfig.StagedCredential) error) ([]credentialStage, error) {
	return s.stageSecretsFor(ctx, host, call, credentialStagingTarget{
		ConfigurationID: definition.ID, PluginID: definition.PluginID, Fields: definition.ManagedCredentials,
	}, candidateID, catalogDigest, secrets, checkpoint)
}

type credentialStagingTarget struct {
	ConfigurationID      string
	ResourceCollectionID string
	ResourceID           string
	PluginID             string
	Fields               []pluginv1.ManagedCredentialField
}

func (s *Service) stageSecretsFor(ctx context.Context, host sdk.Host, call *contractv1.CallContext, target credentialStagingTarget, candidateID, catalogDigest string, secrets map[string]string, checkpoint func(string, pluginconfig.StagedCredential) error) ([]credentialStage, error) {
	fieldIDs := make([]string, 0, len(secrets))
	for fieldID := range secrets {
		fieldIDs = append(fieldIDs, fieldID)
	}
	sort.Strings(fieldIDs)
	staged := make([]credentialStage, 0, len(fieldIDs))
	for _, fieldID := range fieldIDs {
		issued, err := issueAuthority(ctx, host, call, configurationauthority.IssueRequest{
			ConfigurationID: target.ConfigurationID, ResourceCollectionID: target.ResourceCollectionID, ResourceID: target.ResourceID,
			CatalogDigest: catalogDigest, CandidateID: candidateID, FieldID: fieldID,
		})
		if err != nil {
			return staged, err
		}
		var result pluginconfig.StagedCredential
		if err := callCredentials(ctx, host, call, "stageDelegated", map[string]string{"authority": issued.Token, "value": secrets[fieldID]}, &result); err != nil {
			return staged, err
		}
		purpose := ""
		for _, field := range target.Fields {
			if field.ID == fieldID {
				purpose = field.Purpose
				break
			}
		}
		if result.ID == "" || result.Ref.Handle == "" || result.Ref.Owner != target.PluginID || result.Ref.Purpose != purpose || result.Ref.Scope != "tenant" || result.Ref.Version < 1 {
			_ = callCredentials(ctx, host, call, "abortDelegated", map[string]string{"stageId": result.ID, "candidateId": candidateID}, nil)
			return staged, errors.New("凭证插件返回了不符合配置授权边界的委托引用")
		}
		binding := credentialStage{FieldID: fieldID, Stage: result}
		if err := checkpoint(fieldID, result); err != nil {
			_ = callCredentials(ctx, host, call, "abortDelegated", map[string]string{"stageId": result.ID, "candidateId": candidateID}, nil)
			return staged, err
		}
		staged = append(staged, binding)
	}
	return staged, nil
}

func issueAuthority(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request configurationauthority.IssueRequest) (configurationauthority.Issued, error) {
	if host == nil {
		return configurationauthority.Issued{}, errors.New("插件配置协调器缺少可信宿主")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return configurationauthority.Issued{}, err
	}
	defer zero(payload)
	operation := "issue"
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: configurationauthority.KernelIssueService, Operation: &operation}, call, payload)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return configurationauthority.Issued{}, errors.New("可信宿主拒绝签发配置授权")
	}
	var issued configurationauthority.Issued
	if err := decodeStrict(raw, &issued); err != nil || !strings.HasPrefix(issued.Token, configurationauthority.TokenPrefix) {
		return configurationauthority.Issued{}, errors.New("可信宿主返回的配置授权无效")
	}
	return issued, nil
}

func callCredentials(ctx context.Context, host sdk.Host, call *contractv1.CallContext, operation string, input any, output any) error {
	if host == nil {
		return errors.New("插件配置协调器缺少可信宿主")
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	defer zero(payload)
	logicalService, routingDomain := "platform.credentials", "platform"
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: credentialCapability, Operation: &operation, LogicalService: &logicalService, RoutingDomain: &routingDomain}, call, payload)
	if err != nil {
		return err
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		if result != nil && result.Error != nil {
			return errors.New(result.Error.Message)
		}
		return errors.New("凭证插件拒绝委托凭证操作")
	}
	if output != nil {
		return decodeStrict(raw, output)
	}
	return nil
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
