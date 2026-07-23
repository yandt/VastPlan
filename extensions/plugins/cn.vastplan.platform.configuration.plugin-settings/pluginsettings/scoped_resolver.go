package pluginsettings

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	configurationscopedv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationscoped/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const scopedResolverDescriptor = `{"title":"Tenant/User Scoped Configuration Resolver","protocol":"configuration.scoped.v1"}`

func ScopedContribution(service *Service) sdk.Contribution {
	return sdk.Contribution{
		ExtensionPoint: configurationscopedv1.ExtensionPoint,
		ID:             configurationscopedv1.Capability,
		Descriptor:     []byte(scopedResolverDescriptor),
		Handlers: map[string]sdk.Handler{
			configurationscopedv1.OperationResolve: func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
				return service.handleScopedResolve(ctx, host, call, payload)
			},
			configurationscopedv1.OperationWatchRevision: func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
				return service.handleScopedWatch(ctx, host, call, payload)
			},
		},
	}
}

func (s *Service) handleScopedResolve(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	parsed, err := configurationscopedv1.ParseRequest(configurationscopedv1.OperationResolve, payload)
	if err != nil {
		return scopedError("configuration.scoped.invalid_request", err)
	}
	_ = parsed.(*configurationscopedv1.ResolveRequest)
	resolution, _, err := s.resolveScoped(ctx, host, call)
	if err != nil {
		return scopedError(scopedErrorCode(err), err)
	}
	raw, err := json.Marshal(resolution)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func (s *Service) handleScopedWatch(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	parsed, err := configurationscopedv1.ParseRequest(configurationscopedv1.OperationWatchRevision, payload)
	if err != nil {
		return scopedError("configuration.scoped.invalid_request", err)
	}
	request := parsed.(*configurationscopedv1.WatchRevisionRequest)
	resolution, key, err := s.resolveScoped(ctx, host, call)
	if err != nil {
		return scopedError(scopedErrorCode(err), err)
	}
	changed := resolution.Revision != request.AfterRevision || resolution.Digest != request.AfterDigest
	if !changed {
		timeout := time.Duration(request.TimeoutMS) * time.Millisecond
		if timeout == 0 {
			timeout = time.Duration(configurationscopedv1.MaxWatchTimeoutMS) * time.Millisecond
		}
		tenant := call.GetTenantId()
		s.mu.Lock()
		state := s.tenantLocked(tenant)
		current := scopedActiveReference{}
		if active, exists := state.ScopedActives[key]; exists {
			current = active.reference()
		} else {
			current.Digest = resolution.Digest
		}
		if current.Revision != request.AfterRevision || current.Digest != request.AfterDigest {
			changed = true
			s.mu.Unlock()
		} else {
			updates := s.scopedChangeChannelLocked(tenant, key)
			s.mu.Unlock()
			timer := time.NewTimer(timeout)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, nil, ctx.Err()
			case <-updates:
				timer.Stop()
			case <-timer.C:
			}
			resolution, _, err = s.resolveScoped(ctx, host, call)
			if err != nil {
				return scopedError(scopedErrorCode(err), err)
			}
			changed = resolution.Revision != request.AfterRevision || resolution.Digest != request.AfterDigest
		}
	}
	observation := configurationscopedv1.RevisionObservation{
		Protocol: configurationscopedv1.Protocol, ConfigurationID: resolution.ConfigurationID, Changed: changed,
		Revision: resolution.Revision, Digest: resolution.Digest, ObservedAt: s.now(),
	}
	if err := configurationscopedv1.ValidateRevisionObservation(observation); err != nil {
		return nil, nil, err
	}
	raw, err := json.Marshal(observation)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func (s *Service) resolveScoped(ctx context.Context, host sdk.Host, call *contractv1.CallContext) (configurationscopedv1.Resolution, string, error) {
	if call == nil || call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || call.GetCaller().GetId() == "" || call.GetTenantId() == "" {
		return configurationscopedv1.Resolution{}, "", errors.New("Scoped Configuration 只接受认证插件调用")
	}
	if err := s.ensureConfigured(ctx, host, call); err != nil {
		return configurationscopedv1.Resolution{}, "", err
	}
	catalogs, err := s.catalogs(ctx, host, call)
	if err != nil {
		return configurationscopedv1.Resolution{}, "", err
	}
	var matches []pluginconfiguration.Definition
	for _, catalog := range catalogs {
		for _, definition := range catalog.Items {
			if definition.ApplyPath == pluginconfiguration.ApplyHotScoped && definition.PluginID == call.GetCaller().GetId() {
				matches = append(matches, definition)
			}
		}
	}
	if len(matches) != 1 {
		return configurationscopedv1.Resolution{}, "", errors.New("Scoped Configuration 要求当前 caller 只有一个活动定义")
	}
	definition := matches[0]
	configurationID := definition.ID
	subjectID := ""
	if definition.Scope == string(configurationscopedv1.ScopeUser) {
		subjectID = call.GetPrincipal().GetUserId()
		if subjectID == "" {
			return configurationscopedv1.Resolution{}, "", errors.New("user scoped 配置要求认证 subject")
		}
	}
	key := scopedRecordKey(configurationID, subjectID)
	s.mu.Lock()
	active, exists := s.tenantLocked(call.GetTenantId()).ScopedActives[key]
	s.mu.Unlock()
	resolution := configurationscopedv1.Resolution{
		Protocol: configurationscopedv1.Protocol, ConfigurationID: configurationID, Scope: configurationscopedv1.Scope(definition.Scope),
		SchemaDigest: definition.SchemaDigest, ArtifactSHA256: definition.Artifact.SHA256, ObservedAt: s.now(),
	}
	if exists {
		if active.ConfigurationID != definition.ID || active.PluginID != definition.PluginID || active.Scope != definition.Scope ||
			active.SubjectID != subjectID || active.SchemaDigest != definition.SchemaDigest || active.ArtifactSHA256 != definition.Artifact.SHA256 {
			return configurationscopedv1.Resolution{}, "", errors.New("Scoped Configuration Active 与当前签名定义不一致")
		}
		if err := pluginconfiguration.ValidateValues(definition, active.Values); err != nil {
			return configurationscopedv1.Resolution{}, "", errors.New("Scoped Configuration Active 不符合当前签名 Schema")
		}
		resolution.Revision, resolution.Digest, resolution.Values, resolution.Source = active.Revision, active.Digest, append([]byte(nil), active.Values...), "active"
	} else {
		digest, digestErr := configurationscopedv1.DigestValues(definition.Values)
		if digestErr != nil {
			return configurationscopedv1.Resolution{}, "", digestErr
		}
		resolution.Digest, resolution.Values, resolution.Source = digest, append([]byte(nil), definition.Values...), "seed"
	}
	if err := configurationscopedv1.ValidateResolution(resolution); err != nil {
		return configurationscopedv1.Resolution{}, "", err
	}
	return resolution, key, nil
}

func scopedErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrNotFound):
		return "configuration.scoped.not_found"
	case errors.Is(err, ErrConflict):
		return "configuration.scoped.conflict"
	default:
		return "configuration.scoped.denied"
	}
}

func scopedError(code string, err error) (*contractv1.CallResult, []byte, error) {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error()}}, nil, nil
}
