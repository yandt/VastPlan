package authorizationpolicy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type SnapshotWriter interface {
	Write(authorizationv1.SignedPolicySnapshot) error
}

type FileSnapshotWriter struct{ Path string }

func (w FileSnapshotWriter) Write(snapshot authorizationv1.SignedPolicySnapshot) error {
	return WriteSignedSnapshot(w.Path, snapshot)
}

type ServiceOptions struct {
	Store           Store
	StoreFactory    StoreFactory
	BootstrapState  *State
	Signer          SnapshotSigner
	SnapshotWriter  SnapshotWriter
	Catalog         pluginv1.PermissionCatalog
	ProviderProfile authorizationv1.ProviderProfile
	Domains         []authorizationv1.PolicyDomain
	DefaultAudience []string
	DefaultTTL      time.Duration
	Now             func() time.Time
}

type Service struct {
	store           Store
	storeFactory    StoreFactory
	bootstrapState  *State
	catalog         pluginv1.PermissionCatalog
	providerProfile authorizationv1.ProviderProfile
	domains         []authorizationv1.PolicyDomain
	signer          SnapshotSigner
	snapshotWriter  SnapshotWriter
	defaultAudience []string
	defaultTTL      time.Duration
	now             func() time.Time
	mu              sync.Mutex
}

func NewService(options ServiceOptions) (*Service, error) {
	if (options.Store == nil) == (options.StoreFactory == nil) || options.Signer == nil || options.SnapshotWriter == nil {
		return nil, errors.New("Authorization Policy 需要且只能配置一个 Store/StoreFactory，并配置 Signer 与 SnapshotWriter")
	}
	if _, err := pluginv1.ParsePermissionCatalog(mustJSON(options.Catalog)); err != nil {
		return nil, fmt.Errorf("Authorization Policy 权限目录无效: %w", err)
	}
	if err := authorizationv1.ValidateProviderProfile(options.ProviderProfile); err != nil {
		return nil, fmt.Errorf("Authorization Policy Provider Profile 无效: %w", err)
	}
	if len(options.Domains) == 0 || rootDomainID(options.Domains) == "" {
		return nil, errors.New("Authorization Policy 缺少根 Domain")
	}
	if options.DefaultTTL <= 0 {
		options.DefaultTTL = 5 * time.Minute
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	bootstrapState, err := cloneState(options.BootstrapState)
	if err != nil {
		return nil, fmt.Errorf("复制 Authorization Policy bootstrap state: %w", err)
	}
	service := &Service{
		store: options.Store, storeFactory: options.StoreFactory, bootstrapState: bootstrapState,
		catalog: options.Catalog, providerProfile: options.ProviderProfile, domains: append([]authorizationv1.PolicyDomain(nil), options.Domains...),
		signer: options.Signer, snapshotWriter: options.SnapshotWriter,
		defaultAudience: append([]string(nil), options.DefaultAudience...), defaultTTL: options.DefaultTTL, now: options.Now,
	}
	if service.store != nil {
		if err := service.initialize(service.store); err != nil {
			return nil, err
		}
	}
	return service, nil
}

func (s *Service) handle(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, operation string, raw []byte) (*contractv1.CallResult, []byte, error) {
	subject, err := managementSubject(callCtx)
	if err != nil {
		return policyFailure("platform.authorization.forbidden", err), nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	store := s.store
	if s.storeFactory != nil {
		store, err = s.storeFactory(ctx, host, callCtx)
		if err != nil {
			return policyFailure("platform.authorization.unavailable", err), nil, nil
		}
	}
	if err := s.initialize(store); err != nil {
		return policyFailure("platform.authorization.unavailable", err), nil, nil
	}
	value, err := s.execute(store, subject, operation, raw)
	if err != nil {
		if errors.Is(err, ErrStoreUnavailable) {
			return policyFailure("platform.authorization.unavailable", err), nil, nil
		}
		return policyFailure("platform.authorization.rejected", err), nil, nil
	}
	response, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, response, nil
}

func (s *Service) execute(store Store, subject, operation string, raw []byte) (any, error) {
	switch operation {
	case "get":
		return store.Load()
	case "listAudit":
		state, err := store.Load()
		return state.Audit, err
	case "createRole":
		request, err := decodeRequest[CreateRoleRequest](raw)
		return s.createRole(store, subject, request, err)
	case "updateRole":
		request, err := decodeRequest[UpdateRoleRequest](raw)
		return s.updateRole(store, subject, request, err)
	case "submitRole", "approveRole", "publishRole", "retireRole":
		request, err := decodeRequest[TransitionRequest](raw)
		return s.transitionRole(store, subject, operation, request, err)
	case "createBinding":
		request, err := decodeRequest[CreateBindingRequest](raw)
		return s.createBinding(store, subject, request, err)
	case "updateBinding":
		request, err := decodeRequest[UpdateBindingRequest](raw)
		return s.updateBinding(store, subject, request, err)
	case "submitBinding", "approveBinding", "publishBinding", "retireBinding":
		request, err := decodeRequest[TransitionRequest](raw)
		return s.transitionBinding(store, subject, operation, request, err)
	case "revoke":
		request, err := decodeRequest[RevokeRequest](raw)
		return s.revoke(store, subject, request, err)
	case "publishSnapshot":
		request, err := decodeRequest[PublishSnapshotRequest](raw)
		return s.publishSnapshot(store, subject, request, err)
	default:
		return nil, fmt.Errorf("未知 Authorization Policy 操作 %s", operation)
	}
}

func managementSubject(callCtx *contractv1.CallContext) (string, error) {
	if callCtx == nil || callCtx.Caller == nil || callCtx.Caller.Kind != contractv1.CallerKind_CALLER_KIND_USER || callCtx.Principal == nil || callCtx.Principal.UserId == "" || callCtx.Caller.Id != callCtx.Principal.UserId {
		return "", errors.New("只有经验证用户可管理授权策略")
	}
	return callCtx.Principal.UserId, nil
}

func decodeRequest[T any](raw []byte) (T, error) {
	var target T
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&target); err != nil {
		return target, fmt.Errorf("解析请求: %w", err)
	}
	return target, nil
}

func mustJSON(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}

func policyFailure(code string, err error) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error(), Retryable: code == "platform.authorization.unavailable"}}
}
