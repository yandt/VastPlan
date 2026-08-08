package platformcontrolbootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	platformcontrol "cdsoft.com.cn/VastPlan/extensions/libraries/go/platformcontrol"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

// Service owns Database Runtime's bootstrap-only lifecycle. It exposes no
// database credential or driver object and accepts only the trusted host
// system caller.
type Service struct {
	bootstrapper         *Bootstrapper
	binding              *sharedstate.BindingStore
	recordBinding        *databaseruntime.PlatformRecordBinding
	prepareModels        func(context.Context, databaseruntime.PlatformRecordStore) error
	credentialsDirectory string

	mu      sync.Mutex
	managed platformcontrol.ManagedStore
	// managedGeneration identifies the pool in managed, so a release request can
	// prove it targets that pool and not one a later activation installed.
	managedGeneration uint64
	status            platformcontrolv1.Status
}

func NewService(bootstrapper *Bootstrapper, binding *sharedstate.BindingStore, recordBinding *databaseruntime.PlatformRecordBinding,
	prepareModels func(context.Context, databaseruntime.PlatformRecordStore) error, credentialsDirectory string) (*Service, error) {
	if bootstrapper == nil || binding == nil || recordBinding == nil || prepareModels == nil {
		return nil, errors.New("Platform Control Runtime Service 依赖不能为空")
	}
	return &Service{bootstrapper: bootstrapper, binding: binding, recordBinding: recordBinding, prepareModels: prepareModels, credentialsDirectory: credentialsDirectory,
		status: platformcontrolv1.Status{Phase: platformcontrolv1.PhaseUnconfigured}}, nil
}

func (s *Service) Contribution() sdk.Contribution {
	return sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage,
		ID:             platformcontrolv1.BootstrapCapability,
		Descriptor:     []byte(`{"title":"Platform Control SQL Bootstrap","subcommands":[{"name":"test","description":"测试 Platform Control SQL 候选"},{"name":"provision","description":"首次配置时创建 Platform Control 目标数据库"},{"name":"initialize","description":"初始化并绑定 Platform Control SQL 候选"},{"name":"open","description":"打开已提交的 Platform Control SQL Profile"},{"name":"close","description":"释放宿主不再提交的 Platform Control SQL 候选连接池"}]}`),
		Handlers: map[string]sdk.Handler{
			platformcontrolv1.OperationTest:       s.handler(platformcontrolv1.OperationTest),
			platformcontrolv1.OperationProvision:  s.handler(platformcontrolv1.OperationProvision),
			platformcontrolv1.OperationInitialize: s.handler(platformcontrolv1.OperationInitialize),
			platformcontrolv1.OperationOpen:       s.handler(platformcontrolv1.OperationOpen),
			platformcontrolv1.OperationClose:      s.closeHandler(),
		},
	}
}

func (s *Service) Status() platformcontrolv1.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.managed == nil {
		return nil
	}
	err := s.managed.Close()
	s.managed = nil
	s.managedGeneration = 0
	return err
}

// closeHandler is separate from handler because releasing a pool needs neither
// a profile nor a secret: the replica already owns the pool, and the request
// only has to name the generation it is releasing.
func (s *Service) closeHandler() sdk.Handler {
	return func(_ context.Context, _ sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if !trustedBootstrapCaller(call) {
			return bootstrapResult(platformcontrolv1.Status{}, platformcontrolv1.ErrorInvalid, false)
		}
		var request platformcontrolv1.CloseRequest
		if err := json.Unmarshal(payload, &request); err != nil || request.Generation == 0 {
			return bootstrapResult(platformcontrolv1.Status{}, platformcontrolv1.ErrorInvalid, false)
		}

		s.mu.Lock()
		defer s.mu.Unlock()
		// Idempotent, and deliberately a no-op once a later activation replaced
		// the pool: the generation guard is what keeps a stale release from
		// closing the pool that is currently serving Shared State.
		if s.managed != nil && s.managedGeneration == request.Generation {
			_ = s.managed.Close()
			s.managed = nil
			s.managedGeneration = 0
		}
		return bootstrapResult(s.status, "", false)
	}
}

func (s *Service) handler(operation string) sdk.Handler {
	return func(ctx context.Context, _ sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if !trustedBootstrapCaller(call) {
			return bootstrapResult(platformcontrolv1.Status{}, platformcontrolv1.ErrorInvalid, false)
		}
		profile, err := platformcontrolv1.ParseProfile(payload)
		if err != nil {
			return bootstrapResult(platformcontrolv1.Status{}, platformcontrolv1.ErrorInvalid, false)
		}
		source, err := platformcontrol.ResolveSecretSource(profile.SecretRef, s.credentialsDirectory)
		if err != nil {
			return bootstrapResult(platformcontrolv1.Status{}, platformcontrolv1.ErrorUnavailable, false)
		}
		return s.execute(ctx, call, operation, profile, source)
	}
}

func (s *Service) execute(ctx context.Context, call *contractv1.CallContext, operation string, profile platformcontrolv1.Profile, source platformcontrol.SecretSource) (*contractv1.CallResult, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch operation {
	case platformcontrolv1.OperationTest:
		s.status = platformcontrolv1.Status{Phase: platformcontrolv1.PhaseTesting, Generation: profile.Generation}
		if err := s.bootstrapper.Test(ctx, profile, source); err != nil {
			return s.databaseFailure(call, operation, profile, "probe", err, databasev1.ErrorConnectionUnavailable)
		}
		// Test must never mutate the current binding.
		generation, _, ready := s.binding.Snapshot()
		phase := platformcontrolv1.PhaseUnconfigured
		if ready {
			phase = platformcontrolv1.PhaseReady
		}
		s.status = platformcontrolv1.Status{Phase: phase, Generation: generation}
		return bootstrapResult(s.status, "", false)
	case platformcontrolv1.OperationProvision:
		s.status = platformcontrolv1.Status{Phase: platformcontrolv1.PhaseProvisioning, Generation: profile.Generation}
		if err := s.bootstrapper.Provision(ctx, profile, source); err != nil {
			return s.databaseFailure(call, operation, profile, "provision", err, platformcontrolv1.ErrorProvisioningFailed)
		}
		generation, _, ready := s.binding.Snapshot()
		phase := platformcontrolv1.PhaseUnconfigured
		if ready {
			phase = platformcontrolv1.PhaseReady
		}
		s.status = platformcontrolv1.Status{Phase: phase, Generation: generation}
		return bootstrapResult(s.status, "", false)
	case platformcontrolv1.OperationInitialize, platformcontrolv1.OperationOpen:
		return s.activate(ctx, call, operation, profile, source)
	default:
		return bootstrapResult(platformcontrolv1.Status{}, platformcontrolv1.ErrorInvalid, false)
	}
}

func (s *Service) activate(ctx context.Context, call *contractv1.CallContext, operation string, profile platformcontrolv1.Profile, source platformcontrol.SecretSource) (*contractv1.CallResult, []byte, error) {
	identity := platformcontrol.ProfileIdentity(profile)
	currentGeneration, currentIdentity, ready := s.binding.Snapshot()
	recordGeneration, recordIdentity, recordReady := s.recordBinding.Snapshot()
	if ready && recordReady && currentGeneration == profile.Generation && recordGeneration == profile.Generation && currentIdentity == identity && recordIdentity == identity {
		s.status = platformcontrolv1.Status{Phase: platformcontrolv1.PhaseReady, Generation: currentGeneration}
		return bootstrapResult(s.status, "", false)
	}
	if currentGeneration >= profile.Generation || recordGeneration >= profile.Generation {
		return bootstrapResult(s.status, platformcontrolv1.ErrorConflict, false)
	}

	s.status = platformcontrolv1.Status{Phase: platformcontrolv1.PhaseInitializing, Generation: profile.Generation}
	var candidate platformcontrol.ManagedStore
	var err error
	if operation == platformcontrolv1.OperationInitialize {
		candidate, err = s.bootstrapper.Initialize(ctx, profile, source)
	} else {
		candidate, err = s.bootstrapper.Open(ctx, profile, source)
	}
	if err != nil {
		return s.databaseFailure(call, operation, profile, "initialize", err, platformcontrolv1.ErrorInitializationFailed)
	}
	recordStore, ok := candidate.(databaseruntime.PlatformRecordStore)
	if !ok {
		_ = candidate.Close()
		return bootstrapResult(s.status, platformcontrolv1.ErrorInitializationFailed, false)
	}
	if err := s.prepareModels(ctx, recordStore); err != nil {
		_ = candidate.Close()
		s.status = platformcontrolv1.Status{Phase: platformcontrolv1.PhaseRecovery, Generation: profile.Generation, Code: platformcontrolv1.ErrorInitializationFailed}
		return bootstrapResult(s.status, platformcontrolv1.ErrorInitializationFailed, false)
	}
	if err := s.binding.Bind(profile.Generation, identity, candidate); err != nil {
		_ = candidate.Close()
		return bootstrapResult(s.status, platformcontrolv1.ErrorConflict, false)
	}
	if err := s.recordBinding.Bind(profile.Generation, identity, recordStore); err != nil {
		_ = candidate.Close()
		return bootstrapResult(s.status, platformcontrolv1.ErrorConflict, false)
	}
	previous := s.managed
	s.managed = candidate
	s.managedGeneration = profile.Generation
	s.status = platformcontrolv1.Status{Phase: platformcontrolv1.PhaseReady, Generation: profile.Generation}
	if previous != nil {
		_ = previous.Close()
	}
	return bootstrapResult(s.status, "", false)
}

func (s *Service) databaseFailure(call *contractv1.CallContext, operation string, profile platformcontrolv1.Profile, stage string, err error, fallback string) (*contractv1.CallResult, []byte, error) {
	code, retryable := databaseruntime.ErrorDetails(err)
	if code == databasev1.ErrorQueryFailed || !databasev1.KnownErrorCode(code) {
		code = fallback
	}
	databaseruntime.LogRuntimeDiagnostic(call, operation, profile.Connection.ProviderID, stage, err)
	s.status = platformcontrolv1.Status{Phase: platformcontrolv1.PhaseRecovery, Generation: profile.Generation, Code: code}
	return bootstrapResult(s.status, code, retryable)
}

func trustedBootstrapCaller(call *contractv1.CallContext) bool {
	return call != nil && call.GetCaller() != nil &&
		call.GetCaller().GetKind() == contractv1.CallerKind_CALLER_KIND_SYSTEM &&
		call.GetCaller().GetId() == platformcontrolv1.TrustedBootstrapSystemID
}

func bootstrapResult(status platformcontrolv1.Status, code string, retryable bool) (*contractv1.CallResult, []byte, error) {
	if code != "" {
		message := "Platform Control SQL Bootstrap 请求失败"
		if databasev1.KnownErrorCode(code) {
			message = databaseruntime.RuntimeSafeMessage(code)
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{
			Code: code, Message: message, Retryable: retryable,
		}}, nil, nil
	}
	raw, err := json.Marshal(status)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

var _ interface{ Close() error } = (*Service)(nil)
