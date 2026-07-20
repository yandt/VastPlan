package databaseruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	runtimeLogicalService = "foundation.data.relational.runtime"
	runtimeRoutingDomain  = "platform"
	managerLogicalService = "platform.database"
)

type MaterialSourceFactory func(sdk.Host, string, commonv1.ManagedCredentialRef) (MaterialSource, error)

// Service owns the public Database Runtime data plane. Transaction operations
// remain closed until A4 adds signed instance-affine handles.
type Service struct {
	registry     *Registry
	manager      *PoolManager
	material     MaterialSourceFactory
	validationMu sync.Mutex
	validations  map[string]*definitionValidation
}

type definitionValidation struct {
	mu         sync.Mutex
	validUntil time.Time
}

const definitionLeaseTTL = time.Second

func NewService(registry *Registry) (*Service, error) {
	manager, err := NewPoolManager(registry, DefaultManagerPolicy())
	if err != nil {
		return nil, err
	}
	return NewServiceWithManager(registry, manager, func(host sdk.Host, tenant string, ref commonv1.ManagedCredentialRef) (MaterialSource, error) {
		return NewRuntimeMaterialSourceFromEnvironment(host, tenant, ref)
	})
}

func NewServiceWithManager(registry *Registry, manager *PoolManager, material MaterialSourceFactory) (*Service, error) {
	if registry == nil || manager == nil || material == nil {
		return nil, fmt.Errorf("Database Runtime service 依赖不能为空")
	}
	return &Service{registry: registry, manager: manager, material: material, validations: map[string]*definitionValidation{}}, nil
}

func (s *Service) Providers(payload []byte) (databasev1.ProviderListResult, error) {
	if _, err := databasev1.ParseRequest(databasev1.OperationProviders, payload); err != nil {
		return databasev1.ProviderListResult{}, err
	}
	return databasev1.ProviderListResult{Providers: s.registry.Descriptors()}, nil
}

func requestScope(call *contractv1.CallContext, requireCaller bool) (RequestScope, error) {
	if call == nil || call.GetTenantId() == "" {
		return RequestScope{}, errors.New("Database Runtime 调用必须携带 tenant")
	}
	scope := RequestScope{TenantID: call.GetTenantId(), ProjectID: call.GetProjectId()}
	if call.GetCaller() != nil {
		scope.CallerID = call.GetCaller().GetId()
	}
	if err := scope.validate(requireCaller); err != nil {
		return RequestScope{}, err
	}
	return scope, nil
}

func requireManager(call *contractv1.CallContext) error {
	if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN ||
		call.GetCaller().GetId() != databasev1.ConnectionManagerPluginID {
		return errors.New("只有数据库连接管理插件可以发布连接定义")
	}
	return nil
}

func requireExecutor(call *contractv1.CallContext, ref databasev1.ConnectionRef) error {
	if call == nil || call.GetCaller().GetId() == "" {
		return errors.New("数据库执行调用缺少可信 caller")
	}
	if call.GetCaller().GetKind() == contractv1.CallerKind_CALLER_KIND_SYSTEM {
		return nil
	}
	switch call.GetCaller().GetKind() {
	case contractv1.CallerKind_CALLER_KIND_PLUGIN, contractv1.CallerKind_CALLER_KIND_AGENT, contractv1.CallerKind_CALLER_KIND_RUNNER:
		grant := "database.connection/" + ref.ResourceID
		for _, credential := range call.GetCredentials() {
			if credential.GetName() == grant && (credential.GetScope() == "" || credential.GetScope() == "tenant" || credential.GetScope() == "project") {
				return nil
			}
		}
		return errors.New("调用方没有该数据库连接的宿主投影授权")
	default:
		return errors.New("用户不能直接调用底层数据库执行能力")
	}
}

func (s *Service) materialFor(host sdk.Host, tenant string, ref commonv1.ManagedCredentialRef) (MaterialSource, error) {
	return s.material(host, tenant, ref)
}

func (s *Service) probe(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request *databasev1.ProbeRequest) (databasev1.ProbeResult, error) {
	if err := requireManager(call); err != nil {
		return databasev1.ProbeResult{}, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err)
	}
	scope, err := requestScope(call, false)
	if err != nil {
		return databasev1.ProbeResult{}, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err)
	}
	material, err := s.materialFor(host, scope.TenantID, request.Connection.Credentials)
	if err != nil {
		return databasev1.ProbeResult{}, NewRuntimeError(databasev1.ErrorConnectionUnavailable, false, err)
	}
	started := time.Now()
	pool, err := s.registry.OpenPool(ctx, request.Connection, material)
	if err != nil {
		return databasev1.ProbeResult{}, err
	}
	defer pool.Close()
	if err := pool.Probe(ctx); err != nil {
		return databasev1.ProbeResult{}, NewRuntimeError(databasev1.ErrorConnectionUnavailable, true, err)
	}
	return databasev1.ProbeResult{Ready: true, ProviderID: request.Connection.ProviderID, LatencyMS: time.Since(started).Milliseconds()}, nil
}

func (s *Service) activate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request *databasev1.ActivateRequest) (databasev1.ActivateResult, error) {
	if err := requireManager(call); err != nil {
		return databasev1.ActivateResult{}, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err)
	}
	scope, err := requestScope(call, false)
	if err != nil {
		return databasev1.ActivateResult{}, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err)
	}
	material, err := s.materialFor(host, scope.TenantID, request.Connection.Credentials)
	if err != nil {
		return databasev1.ActivateResult{}, NewRuntimeError(databasev1.ErrorConnectionUnavailable, false, err)
	}
	result, err := s.manager.Activate(ctx, scope, request.Connection, material)
	if err == nil {
		s.markDefinitionCurrent(scope.TenantID, request.Connection.Ref)
	}
	return result, err
}

func validationKey(tenant string, ref databasev1.ConnectionRef) string {
	return tenant + "\x00" + ref.ResourceID + fmt.Sprintf("\x00%d", ref.Revision)
}

func (s *Service) validation(tenant string, ref databasev1.ConnectionRef) *definitionValidation {
	key := validationKey(tenant, ref)
	s.validationMu.Lock()
	defer s.validationMu.Unlock()
	entry := s.validations[key]
	if entry == nil {
		entry = &definitionValidation{}
		s.validations[key] = entry
	}
	return entry
}

func (s *Service) markDefinitionCurrent(tenant string, ref databasev1.ConnectionRef) {
	entry := s.validation(tenant, ref)
	entry.mu.Lock()
	entry.validUntil = time.Now().Add(definitionLeaseTTL)
	entry.mu.Unlock()
}

func (s *Service) clearDefinition(tenant string, ref databasev1.ConnectionRef) {
	s.validationMu.Lock()
	delete(s.validations, validationKey(tenant, ref))
	s.validationMu.Unlock()
}

func (s *Service) hydrate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, ref databasev1.ConnectionRef) error {
	payload, err := json.Marshal(struct {
		Connection databasev1.ConnectionRef `json:"connection"`
	}{Connection: ref})
	if err != nil {
		return err
	}
	operation, logicalService, routingDomain := "resolveRuntime", managerLogicalService, runtimeRoutingDomain
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: "platform.database", Operation: &operation,
		LogicalService: &logicalService, RoutingDomain: &routingDomain,
	}, call, payload)
	if err != nil {
		return NewRuntimeError(databasev1.ErrorConnectionUnavailable, true, err)
	}
	if result == nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		if result.GetError().GetCode() == "platform.database.not_found" {
			_ = s.manager.RetireAll(ctx, call.GetTenantId(), ref)
			s.clearDefinition(call.GetTenantId(), ref)
			return NewRuntimeError(databasev1.ErrorConnectionNotFound, false, errors.New("连接定义不存在"))
		}
		return NewRuntimeError(databasev1.ErrorConnectionUnavailable, true, errors.New("连接管理服务未能解析连接定义"))
	}
	parsed, err := databasev1.ParseRequest(databasev1.OperationActivate, raw)
	if err != nil {
		return NewRuntimeError(databasev1.ErrorConnectionUnavailable, false, errors.New("连接管理服务返回无效定义"))
	}
	request := parsed.(*databasev1.ActivateRequest)
	if request.Connection.Ref != ref {
		return NewRuntimeError(databasev1.ErrorConnectionUnavailable, false, errors.New("连接管理服务返回的 revision 不匹配"))
	}
	material, err := s.materialFor(host, call.GetTenantId(), request.Connection.Credentials)
	if err != nil {
		return NewRuntimeError(databasev1.ErrorConnectionUnavailable, false, err)
	}
	scope, err := requestScope(call, false)
	if err != nil {
		return NewRuntimeError(databasev1.ErrorInvalidRequest, false, err)
	}
	_, err = s.manager.Activate(ctx, scope, request.Connection, material)
	return err
}

func (s *Service) ensureDefinitionCurrent(ctx context.Context, host sdk.Host, call *contractv1.CallContext, ref databasev1.ConnectionRef) error {
	entry := s.validation(call.GetTenantId(), ref)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if time.Now().Before(entry.validUntil) {
		return nil
	}
	if err := s.hydrate(ctx, host, call, ref); err != nil {
		entry.validUntil = time.Time{}
		s.clearDefinition(call.GetTenantId(), ref)
		return err
	}
	entry.validUntil = time.Now().Add(definitionLeaseTTL)
	return nil
}

func (s *Service) acquire(ctx context.Context, host sdk.Host, call *contractv1.CallContext, ref databasev1.ConnectionRef) (*PoolLease, error) {
	scope, err := requestScope(call, true)
	if err != nil {
		return nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err)
	}
	if err := s.ensureDefinitionCurrent(ctx, host, call, ref); err != nil {
		return nil, err
	}
	return s.manager.Acquire(ctx, scope, ref)
}

func runtimeResult(value any, err error) (*contractv1.CallResult, []byte, error) {
	if err != nil {
		code, retryable := ErrorDetails(err)
		message := "数据库运行时操作失败"
		if code == databasev1.ErrorInvalidRequest || code == databasev1.ErrorUnsupported || code == databasev1.ErrorConnectionNotFound {
			message = err.Error()
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{
			Code: code, Message: message, Retryable: retryable,
		}}, nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func (s *Service) handler(operation string) sdk.Handler {
	return func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		parsed, err := databasev1.ParseRequest(operation, payload)
		if err != nil {
			return runtimeResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err))
		}
		switch request := parsed.(type) {
		case *databasev1.ProviderListRequest:
			return runtimeResult(databasev1.ProviderListResult{Providers: s.registry.Descriptors()}, nil)
		case *databasev1.ProbeRequest:
			value, callErr := s.probe(ctx, host, call, request)
			return runtimeResult(value, callErr)
		case *databasev1.ActivateRequest:
			value, callErr := s.activate(ctx, host, call, request)
			return runtimeResult(value, callErr)
		case *databasev1.RetireRequest:
			if callErr := requireManager(call); callErr != nil {
				return runtimeResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, callErr))
			}
			scope, callErr := requestScope(call, false)
			if callErr == nil {
				callErr = s.manager.RetireAll(ctx, scope.TenantID, request.Connection)
				s.clearDefinition(scope.TenantID, request.Connection)
			}
			return runtimeResult(struct{}{}, callErr)
		case *databasev1.QueryRequest:
			if callErr := requireExecutor(call, request.Connection); callErr != nil {
				return runtimeResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, callErr))
			}
			if request.TransactionHandle != "" {
				return runtimeResult(nil, NewRuntimeError(databasev1.ErrorUnsupported, false, errors.New("事务句柄将在 A4 开放")))
			}
			lease, callErr := s.acquire(ctx, host, call, request.Connection)
			if callErr != nil {
				return runtimeResult(nil, callErr)
			}
			defer lease.Release()
			value, callErr := lease.Query(ctx, request.Statement, request.MaxRows)
			return runtimeResult(value, callErr)
		case *databasev1.ExecuteRequest:
			if callErr := requireExecutor(call, request.Connection); callErr != nil {
				return runtimeResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, callErr))
			}
			if request.TransactionHandle != "" {
				return runtimeResult(nil, NewRuntimeError(databasev1.ErrorUnsupported, false, errors.New("事务句柄将在 A4 开放")))
			}
			lease, callErr := s.acquire(ctx, host, call, request.Connection)
			if callErr != nil {
				return runtimeResult(nil, callErr)
			}
			defer lease.Release()
			value, callErr := lease.Execute(ctx, request.Statement)
			return runtimeResult(value, callErr)
		default:
			return runtimeResult(nil, NewRuntimeError(databasev1.ErrorUnsupported, false, errors.New("Database Runtime 操作尚未开放")))
		}
	}
}

func (s *Service) Contribution() sdk.Contribution {
	return sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage, ID: databasev1.Capability, Descriptor: descriptor(),
		Handlers: map[string]sdk.Handler{
			databasev1.OperationProviders: s.handler(databasev1.OperationProviders),
			databasev1.OperationProbe:     s.handler(databasev1.OperationProbe),
			databasev1.OperationActivate:  s.handler(databasev1.OperationActivate),
			databasev1.OperationRetire:    s.handler(databasev1.OperationRetire),
			databasev1.OperationQuery:     s.handler(databasev1.OperationQuery),
			databasev1.OperationExecute:   s.handler(databasev1.OperationExecute),
		},
	}
}

func descriptor() []byte {
	return []byte(`{"title":"Database Runtime","subcommands":[{"name":"providers","description":"列出当前制品内已注册的关系数据库 Provider","paramsSchema":{"type":"object","additionalProperties":false,"maxProperties":0}},{"name":"probe","description":"由连接管理面执行一次性连通性检查"},{"name":"activate","description":"发布或轮换连接 revision"},{"name":"retire","description":"排空并删除连接 revision"},{"name":"query","description":"通过活动连接池执行参数化查询"},{"name":"execute","description":"通过活动连接池执行参数化写操作"}]}`)
}
