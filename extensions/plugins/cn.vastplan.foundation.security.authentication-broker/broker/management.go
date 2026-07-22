package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const ManagementCapability = "foundation.security.authentication-provider-catalog"

type ManagementService struct {
	store    ManagementStore
	verifier AssertionVerifier
	now      func() time.Time
}

func NewManagementService(store ManagementStore, verifiers ...AssertionVerifier) (*ManagementService, error) {
	if store == nil {
		return nil, errors.New("Provider Management Store 不能为空")
	}
	var verifier AssertionVerifier
	if len(verifiers) > 0 {
		verifier = verifiers[0]
	}
	return &ManagementService{store: store, verifier: verifier, now: func() time.Time { return time.Now().UTC() }}, nil
}

func ManagementDescriptor() []byte {
	return []byte(`{"title":"认证 Provider 目录管理","subcommands":[{"name":"get","description":"读取 Provider 管理状态"},{"name":"createDraft","description":"创建不可变 Provider Profile 草稿"},{"name":"validate","description":"验证 Provider Profile"},{"name":"recordTest","description":"记录隔离认证测试"},{"name":"approve","description":"批准已测试 Provider"},{"name":"publish","description":"原子发布 Provider Catalog 与门户 Binding"},{"name":"retire","description":"退役 Provider"}]}`)
}

func (s *ManagementService) Contribution() sdk.Contribution {
	handlers := map[string]sdk.Handler{}
	for _, operation := range []string{"get", "createDraft", "validate", "recordTest", "approve", "publish", "retire"} {
		op := operation
		handlers[op] = func(_ context.Context, _ sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return s.handle(callCtx, op, payload)
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: ManagementCapability, Descriptor: ManagementDescriptor(), Handlers: handlers}
}

func (s *ManagementService) handle(callCtx *contractv1.CallContext, operation string, payload []byte) (*contractv1.CallResult, []byte, error) {
	actor := ""
	if callCtx != nil && callCtx.Principal != nil {
		actor = callCtx.Principal.GetUserId()
	}
	var value any
	var err error
	switch operation {
	case "get":
		value, err = s.store.LoadState()
	case "createDraft":
		var request CreateDraftRequest
		err = strictJSON(payload, &request)
		if err == nil {
			value, err = s.createDraft(request)
		}
	case "validate":
		var request ProviderActionRequest
		err = strictJSON(payload, &request)
		if err == nil {
			value, err = s.transition(request, authenticationv1.ProviderValidated)
		}
	case "recordTest":
		var request RecordTestRequest
		err = strictJSON(payload, &request)
		if err == nil {
			request.Actor = actor
			value, err = s.recordTest(request)
		}
	case "approve":
		var request ApproveRequest
		err = strictJSON(payload, &request)
		if err == nil {
			request.Actor = actor
			value, err = s.approve(request)
		}
	case "publish":
		var request PublishRequest
		err = strictJSON(payload, &request)
		if err == nil {
			value, err = s.publish(request)
		}
	case "retire":
		var request ProviderActionRequest
		err = strictJSON(payload, &request)
		if err == nil {
			value, err = s.transition(request, authenticationv1.ProviderRetired)
		}
	default:
		err = errors.New("未知 Provider 管理操作")
	}
	if err != nil {
		return brokerError("foundation.authentication.management_failed", err), nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	return okResult(), raw, nil
}

func (s *ManagementService) createDraft(request CreateDraftRequest) (ManagementState, error) {
	raw, _ := json.Marshal(request.Profile)
	profile, err := authenticationv1.ParseAuthenticationProviderProfile(raw)
	if err != nil {
		return ManagementState{}, err
	}
	state, err := s.store.LoadState()
	if err != nil {
		return ManagementState{}, err
	}
	if state.Generation != request.ExpectedGeneration {
		return ManagementState{}, errors.New("Provider Management generation 已变化")
	}
	for _, existing := range state.Providers {
		if existing.Profile.ID == profile.ID {
			return ManagementState{}, errors.New("Provider Profile ID 已存在；请创建新 revision ID")
		}
	}
	now := s.now()
	lifecycle := authenticationv1.AuthenticationProviderLifecycle{SchemaVersion: authenticationv1.SchemaVersion, Profile: compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()}, State: authenticationv1.ProviderDraft, Readiness: authenticationv1.ProviderUnknown, UnmetCapabilities: []string{}, UpdatedAt: now}
	state.Providers = append(state.Providers, ManagedProvider{Profile: profile, Lifecycle: lifecycle})
	return s.save(state)
}

func (s *ManagementService) transition(request ProviderActionRequest, target authenticationv1.ProviderLifecycleState) (ManagementState, error) {
	state, err := s.store.LoadState()
	if err != nil {
		return ManagementState{}, err
	}
	if state.Generation != request.ExpectedGeneration {
		return ManagementState{}, errors.New("Provider Management generation 已变化")
	}
	provider, err := findManaged(&state, request.ProviderID)
	if err != nil {
		return ManagementState{}, err
	}
	if !authenticationv1.CanTransitionProvider(provider.Lifecycle.State, target) {
		return ManagementState{}, fmt.Errorf("Provider 状态不能从 %s 转到 %s", provider.Lifecycle.State, target)
	}
	provider.Lifecycle.State = target
	provider.Lifecycle.UpdatedAt = s.now()
	if target == authenticationv1.ProviderRetired && state.Catalog != nil {
		for _, entry := range state.Catalog.Providers {
			if entry.Profile.ID == request.ProviderID {
				return ManagementState{}, errors.New("仍在已发布 Catalog 中的 Provider 不能退役")
			}
		}
	}
	return s.save(state)
}

func (s *ManagementService) recordTest(request RecordTestRequest) (ManagementState, error) {
	state, err := s.store.LoadState()
	if err != nil {
		return ManagementState{}, err
	}
	if state.Generation != request.ExpectedGeneration {
		return ManagementState{}, errors.New("Provider Management generation 已变化")
	}
	provider, err := findManaged(&state, request.ProviderID)
	if err != nil {
		return ManagementState{}, err
	}
	if provider.Lifecycle.State != authenticationv1.ProviderValidated {
		return ManagementState{}, errors.New("只有 Validated Provider 可以记录测试")
	}
	if strings.TrimSpace(request.Actor) == "" {
		return ManagementState{}, errors.New("测试人不能为空")
	}
	if s.verifier == nil {
		return ManagementState{}, errors.New("Provider 测试 Assertion verifier 未配置")
	}
	if err := s.verifier.Verify(request.Assertion); err != nil {
		return ManagementState{}, err
	}
	assertion := request.Assertion.Payload
	if assertion.ProviderProfileID != request.ProviderID || assertion.ProviderID != provider.Profile.ContributionID || assertion.Audience != "authentication-provider-test" || !assertion.ExpiresAt.After(s.now()) || assertion.Subject.ID == "" {
		return ManagementState{}, errors.New("Provider 测试 Assertion 未绑定当前 Profile、测试 audience 或有效主体")
	}
	now := s.now()
	provider.TestedBy = request.Actor
	provider.Lifecycle.TestedAt = &now
	provider.Lifecycle.UpdatedAt = now
	provider.Lifecycle.UnmetCapabilities = []string{}
	provider.Lifecycle.State = authenticationv1.ProviderTested
	provider.Lifecycle.Readiness = authenticationv1.ProviderReady
	return s.save(state)
}

func (s *ManagementService) reconcileReadiness(expected uint64, providerID string, unmet []string) (ManagementState, error) {
	state, err := s.store.LoadState()
	if err != nil {
		return ManagementState{}, err
	}
	if state.Generation != expected {
		return ManagementState{}, errors.New("Provider Management generation 已变化")
	}
	provider, err := findManaged(&state, providerID)
	if err != nil {
		return ManagementState{}, err
	}
	provider.Lifecycle.UnmetCapabilities = append([]string{}, unmet...)
	if len(unmet) > 0 {
		provider.Lifecycle.Readiness = authenticationv1.ProviderBlocked
	} else if provider.Lifecycle.State == authenticationv1.ProviderTested || provider.Lifecycle.State == authenticationv1.ProviderApproved || provider.Lifecycle.State == authenticationv1.ProviderPublished {
		provider.Lifecycle.Readiness = authenticationv1.ProviderReady
	} else {
		provider.Lifecycle.Readiness = authenticationv1.ProviderUnknown
	}
	provider.Lifecycle.UpdatedAt = s.now()
	return s.save(state)
}

func (s *ManagementService) approve(request ApproveRequest) (ManagementState, error) {
	state, err := s.store.LoadState()
	if err != nil {
		return ManagementState{}, err
	}
	if state.Generation != request.ExpectedGeneration {
		return ManagementState{}, errors.New("Provider Management generation 已变化")
	}
	provider, err := findManaged(&state, request.ProviderID)
	if err != nil {
		return ManagementState{}, err
	}
	if provider.Lifecycle.State != authenticationv1.ProviderTested || provider.Lifecycle.Readiness != authenticationv1.ProviderReady {
		return ManagementState{}, errors.New("只有 Ready 的 Tested Provider 可以批准")
	}
	if strings.TrimSpace(request.Actor) == "" || request.Actor == provider.TestedBy {
		return ManagementState{}, errors.New("批准人必须与测试人不同")
	}
	now := s.now()
	provider.ApprovedBy = request.Actor
	provider.Lifecycle.State = authenticationv1.ProviderApproved
	provider.Lifecycle.ApprovedAt = &now
	provider.Lifecycle.UpdatedAt = now
	return s.save(state)
}

func (s *ManagementService) publish(request PublishRequest) (ManagementState, error) {
	state, err := s.store.LoadState()
	if err != nil {
		return ManagementState{}, err
	}
	if state.Generation != request.ExpectedGeneration {
		return ManagementState{}, errors.New("Provider Management generation 已变化")
	}
	entries := []authenticationv1.ProviderCatalogEntry{}
	now := s.now()
	for i := range state.Providers {
		provider := &state.Providers[i]
		if provider.Lifecycle.State != authenticationv1.ProviderApproved && provider.Lifecycle.State != authenticationv1.ProviderPublished {
			continue
		}
		if provider.Lifecycle.Readiness != authenticationv1.ProviderReady {
			continue
		}
		profile := provider.Profile
		entries = append(entries, authenticationv1.ProviderCatalogEntry{Profile: provider.Lifecycle.Profile, ContributionID: profile.ContributionID, Purposes: append([]authenticationv1.ProviderPurpose{}, profile.Purposes...), Methods: append([]string{}, profile.Methods...), SubjectNamespace: profile.SubjectNamespace, RequiredCapabilities: append([]string{}, profile.RequiredCapabilities...)})
	}
	if len(entries) == 0 {
		return ManagementState{}, errors.New("没有 Approved 且 Ready 的 Provider 可发布")
	}
	catalog := authenticationv1.AuthenticationProviderCatalog{Document: compositioncommonv1.Document{Version: 1, Revision: request.CatalogRevision, ID: request.CatalogID}, Providers: entries, Bindings: request.Bindings}
	raw, _ := json.Marshal(catalog)
	parsed, err := authenticationv1.ParseAuthenticationProviderCatalog(raw)
	if err != nil {
		return ManagementState{}, err
	}
	accessRaw, _ := json.Marshal(request.AccessCatalog)
	accessCatalog, err := authenticationv1.ParseAccessProfileCatalog(accessRaw)
	if err != nil {
		return ManagementState{}, err
	}
	for _, profile := range accessCatalog.Profiles {
		for _, method := range profile.Authentication.AllowedMethods {
			if _, found := parsed.Resolve(profile.TenantID, profile.PortalID, method); !found {
				return ManagementState{}, fmt.Errorf("Access Profile %s 的 method %s 未绑定已发布 Provider", profile.ID, method)
			}
		}
	}
	for i := range state.Providers {
		provider := &state.Providers[i]
		for _, entry := range parsed.Providers {
			if entry.Profile.ID == provider.Profile.ID && provider.Lifecycle.State == authenticationv1.ProviderApproved {
				provider.Lifecycle.State = authenticationv1.ProviderPublished
				provider.Lifecycle.PublishedAt = &now
				provider.Lifecycle.UpdatedAt = now
			}
		}
	}
	state.Catalog = &parsed
	state.AccessCatalog = &accessCatalog
	return s.save(state)
}

func (s *ManagementService) save(state ManagementState) (ManagementState, error) {
	state.Generation++
	state.Version = managementStateVersion
	state.UpdatedAt = s.now()
	sort.Slice(state.Providers, func(i, j int) bool { return state.Providers[i].Profile.ID < state.Providers[j].Profile.ID })
	return s.store.UpdateState(state.Generation-1, state)
}
func findManaged(state *ManagementState, id string) (*ManagedProvider, error) {
	for i := range state.Providers {
		if state.Providers[i].Profile.ID == id {
			return &state.Providers[i], nil
		}
	}
	return nil, errors.New("Provider Profile 不存在")
}
