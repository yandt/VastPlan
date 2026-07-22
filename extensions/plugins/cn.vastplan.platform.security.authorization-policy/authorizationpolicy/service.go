package authorizationpolicy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
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
	signer          SnapshotSigner
	snapshotWriter  SnapshotWriter
	defaultAudience []string
	defaultTTL      time.Duration
	now             func() time.Time
	mu              sync.Mutex
}

func NewService(options ServiceOptions) (*Service, error) {
	if options.Store == nil || options.Signer == nil || options.SnapshotWriter == nil {
		return nil, errors.New("Authorization Policy 需要 Store、Signer 与 SnapshotWriter")
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
	service := &Service{store: options.Store, signer: options.Signer, snapshotWriter: options.SnapshotWriter, defaultAudience: append([]string(nil), options.DefaultAudience...), defaultTTL: options.DefaultTTL, now: options.Now}
	if err := service.initialize(options.Catalog, options.ProviderProfile, options.Domains); err != nil {
		return nil, err
	}
	return service, nil
}

func (s *Service) initialize(catalog pluginv1.PermissionCatalog, profile authorizationv1.ProviderProfile, domains []authorizationv1.PolicyDomain) error {
	state, err := s.store.Load()
	if err != nil {
		return err
	}
	if state.Generation != 0 {
		if state.Catalog.Digest == catalog.Digest {
			return nil
		}
		known := catalogPermissions(catalog)
		for _, role := range state.Roles {
			if role.State == StateRetired {
				continue
			}
			for _, statement := range role.Statements {
				for _, permission := range statement.Permissions {
					if _, exists := known[permission]; !exists {
						return fmt.Errorf("新权限目录移除了活动 Role %s 使用的权限 %s", role.ID, permission)
					}
				}
			}
		}
		state.Catalog = catalog
		state.ProviderProfile = profile
		state.Domains = append([]authorizationv1.PolicyDomain(nil), domains...)
		state.Generation++
		state.Audit = append(state.Audit, AuditEvent{ID: randomID("audit"), Action: "catalogUpdate", ObjectKind: "catalog", ObjectID: catalog.Digest, Revision: state.Generation, SubjectID: "trusted-host", OccurredAt: s.now().UTC()})
		_, err := s.store.CompareAndSwap(state.Generation-1, state)
		return err
	}
	state.Generation = 1
	state.Catalog = catalog
	state.ProviderProfile = profile
	state.Domains = append([]authorizationv1.PolicyDomain(nil), domains...)
	_, err = s.store.CompareAndSwap(0, state)
	return err
}

func (s *Service) handle(_ context.Context, callCtx *contractv1.CallContext, operation string, raw []byte) (*contractv1.CallResult, []byte, error) {
	subject, err := managementSubject(callCtx)
	if err != nil {
		return policyFailure("platform.authorization.forbidden", err), nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, err := s.execute(subject, operation, raw)
	if err != nil {
		return policyFailure("platform.authorization.rejected", err), nil, nil
	}
	response, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, response, nil
}

func (s *Service) execute(subject, operation string, raw []byte) (any, error) {
	switch operation {
	case "get":
		return s.store.Load()
	case "listAudit":
		state, err := s.store.Load()
		return state.Audit, err
	case "createRole":
		request, err := decodeRequest[CreateRoleRequest](raw)
		return s.createRole(subject, request, err)
	case "updateRole":
		request, err := decodeRequest[UpdateRoleRequest](raw)
		return s.updateRole(subject, request, err)
	case "submitRole", "approveRole", "publishRole", "retireRole":
		request, err := decodeRequest[TransitionRequest](raw)
		return s.transitionRole(subject, operation, request, err)
	case "createBinding":
		request, err := decodeRequest[CreateBindingRequest](raw)
		return s.createBinding(subject, request, err)
	case "updateBinding":
		request, err := decodeRequest[UpdateBindingRequest](raw)
		return s.updateBinding(subject, request, err)
	case "submitBinding", "approveBinding", "publishBinding", "retireBinding":
		request, err := decodeRequest[TransitionRequest](raw)
		return s.transitionBinding(subject, operation, request, err)
	case "revoke":
		request, err := decodeRequest[RevokeRequest](raw)
		return s.revoke(subject, request, err)
	case "publishSnapshot":
		request, err := decodeRequest[PublishSnapshotRequest](raw)
		return s.publishSnapshot(subject, request, err)
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
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error()}}
}
