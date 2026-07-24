package profileactivation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformprofileactivation"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

func buildProfileCandidate(catalog backendcompositionv1.BackendPlatformCatalog, tenantID, deploymentName string, definition pluginconfiguration.Definition, request platformprofileactivation.PrepareRequest) (backendcompositionv1.PlatformProfile, compositioncommonv1.Ref, error) {
	profile, previous, err := catalog.Resolve(tenantID, deploymentName)
	if err != nil {
		return backendcompositionv1.PlatformProfile{}, compositioncommonv1.Ref{}, err
	}
	profile = cloneJSON(profile)
	maxRevision := profile.Revision
	for _, historical := range catalog.Profiles {
		if historical.ID == profile.ID && historical.Revision > maxRevision {
			maxRevision = historical.Revision
		}
	}
	profile.Revision = maxRevision + 1
	for index := range profile.Services {
		unit := &profile.Services[index]
		if definition.ServiceBaselineID != "" || unit.ID != definition.UnitID {
			continue
		}
		unit.Config, err = updatePlatformOwnedConfig(unit.Config, unit.Plugins, definition, request)
		if err != nil {
			return backendcompositionv1.PlatformProfile{}, compositioncommonv1.Ref{}, err
		}
		validated, err := backendcompositionv1.ValidatePlatformProfile(profile)
		if err != nil {
			return backendcompositionv1.PlatformProfile{}, compositioncommonv1.Ref{}, fmt.Errorf("验证 Platform Profile 配置候选: %w", err)
		}
		return validated, previous, nil
	}
	if definition.ServiceBaselineID != "" {
		for index := range profile.ServiceBaselines {
			baseline := &profile.ServiceBaselines[index]
			if baseline.ID != definition.ServiceBaselineID {
				continue
			}
			var err error
			baseline.Config, err = updatePlatformOwnedConfig(baseline.Config, baseline.Plugins, definition, request)
			if err != nil {
				return backendcompositionv1.PlatformProfile{}, compositioncommonv1.Ref{}, err
			}
			validated, err := backendcompositionv1.ValidatePlatformProfile(profile)
			if err != nil {
				return backendcompositionv1.PlatformProfile{}, compositioncommonv1.Ref{}, fmt.Errorf("验证公共 Service Baseline 配置候选: %w", err)
			}
			return validated, previous, nil
		}
		return backendcompositionv1.PlatformProfile{}, compositioncommonv1.Ref{}, errors.New("配置定义引用的公共 Service Baseline 不存在")
	}
	return backendcompositionv1.PlatformProfile{}, compositioncommonv1.Ref{}, errors.New("配置定义不属于 Platform Profile 的独立 service")
}

func updatePlatformOwnedConfig(current map[string]any, plugins []deploymentv1.PluginRef, definition pluginconfiguration.Definition, request platformprofileactivation.PrepareRequest) (map[string]any, error) {
	installed := make([]string, 0, len(plugins))
	found := false
	for _, plugin := range plugins {
		installed = append(installed, plugin.ID)
		if plugin.ID == definition.PluginID && plugin.Version == definition.Artifact.Version && normalizeChannel(plugin.Channel) == normalizeChannel(definition.Artifact.Channel) {
			found = true
		}
	}
	if !found {
		return nil, errors.New("Platform Profile 未安装配置目录锁定的精确插件")
	}
	envelope, err := pluginconfig.Parse(current, installed)
	if err != nil {
		return nil, err
	}
	var values map[string]any
	if json.Unmarshal(request.Values, &values) != nil || values == nil {
		return nil, errors.New("Platform Profile 配置 values 无效")
	}
	envelope.Plugins[definition.PluginID] = values
	if envelope.ManagedCredentials[definition.PluginID] == nil {
		envelope.ManagedCredentials[definition.PluginID] = map[string]pluginconfig.ManagedCredentialRef{}
	}
	for fieldID, ref := range request.Credentials {
		envelope.ManagedCredentials[definition.PluginID][fieldID] = ref
	}
	if err := validateCredentialRefs(definition, envelope.ManagedCredentials[definition.PluginID]); err != nil {
		return nil, err
	}
	return envelope.Map(), nil
}

func validateCredentialRefs(definition pluginconfiguration.Definition, refs map[string]pluginconfig.ManagedCredentialRef) error {
	declared := make(map[string]pluginv1.ManagedCredentialField, len(definition.ManagedCredentials))
	for _, field := range definition.ManagedCredentials {
		declared[field.ID] = field
		ref, ok := refs[field.ID]
		if field.Required && !ok {
			return errors.New("Platform Profile 配置缺少必填托管凭证")
		}
		if ok && (ref.Owner != definition.PluginID || ref.Purpose != field.Purpose) {
			return errors.New("Platform Profile 托管凭证与签名声明不匹配")
		}
	}
	for fieldID := range refs {
		if _, ok := declared[fieldID]; !ok {
			return errors.New("Platform Profile 配置包含未声明托管凭证")
		}
	}
	return nil
}

func buildCatalogCandidate(active backendcompositionv1.BackendPlatformCatalog, tenantID, deploymentName string, profile backendcompositionv1.PlatformProfile) (backendcompositionv1.BackendPlatformCatalog, error) {
	next := cloneJSON(active)
	next.Revision++
	next.Profiles = append(next.Profiles, profile)
	ref := profileRef(profile)
	bound := false
	for index := range next.Bindings {
		if next.Bindings[index].TenantID == tenantID && next.Bindings[index].DeploymentName == deploymentName {
			next.Bindings[index].PlatformProfile = ref
			bound = true
			break
		}
	}
	if !bound {
		return backendcompositionv1.BackendPlatformCatalog{}, errors.New("Platform Catalog 不存在目标 binding")
	}
	return backendcompositionv1.ValidateBackendPlatformCatalog(next)
}

func profileRef(profile backendcompositionv1.PlatformProfile) compositioncommonv1.Ref {
	return compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()}
}

func cloneJSON[T any](value T) T {
	raw, _ := json.Marshal(value)
	var out T
	_ = json.Unmarshal(raw, &out)
	return out
}

func jsonEqual(left, right []byte) bool {
	var a, b any
	if json.Unmarshal(left, &a) != nil || json.Unmarshal(right, &b) != nil {
		return false
	}
	leftRaw, _ := json.Marshal(a)
	rightRaw, _ := json.Marshal(b)
	return bytes.Equal(leftRaw, rightRaw)
}
