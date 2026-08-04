package platformcontrolbootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	platformcontrol "cdsoft.com.cn/VastPlan/extensions/libraries/go/platformcontrol"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

// Service owns Database Runtime's bootstrap-only lifecycle. It exposes no
// database credential or driver object and accepts only the trusted host
// system caller.
type Service struct {
	bootstrapper         *Bootstrapper
	binding              *sharedstate.BindingStore
	credentialsDirectory string

	mu      sync.Mutex
	managed platformcontrol.ManagedStore
	status  platformcontrolv1.Status
}

func NewService(bootstrapper *Bootstrapper, binding *sharedstate.BindingStore, credentialsDirectory string) (*Service, error) {
	if bootstrapper == nil || binding == nil {
		return nil, errors.New("Platform Control Runtime Service 依赖不能为空")
	}
	return &Service{bootstrapper: bootstrapper, binding: binding, credentialsDirectory: credentialsDirectory,
		status: platformcontrolv1.Status{Phase: platformcontrolv1.PhaseUnconfigured}}, nil
}

func (s *Service) Contribution() sdk.Contribution {
	return sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage,
		ID:             platformcontrolv1.BootstrapCapability,
		Descriptor:     []byte(`{"title":"Platform Control SQL Bootstrap","subcommands":[{"name":"test"},{"name":"initialize"},{"name":"open"}]}`),
		Handlers: map[string]sdk.Handler{
			platformcontrolv1.OperationTest:       s.handler(platformcontrolv1.OperationTest),
			platformcontrolv1.OperationInitialize: s.handler(platformcontrolv1.OperationInitialize),
			platformcontrolv1.OperationOpen:       s.handler(platformcontrolv1.OperationOpen),
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
	return err
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
		return s.execute(ctx, operation, profile, source)
	}
}

func (s *Service) execute(ctx context.Context, operation string, profile platformcontrolv1.Profile, source platformcontrol.SecretSource) (*contractv1.CallResult, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch operation {
	case platformcontrolv1.OperationTest:
		s.status = platformcontrolv1.Status{Phase: platformcontrolv1.PhaseTesting, Generation: profile.Generation}
		if err := s.bootstrapper.Test(ctx, profile, source); err != nil {
			s.status = platformcontrolv1.Status{Phase: platformcontrolv1.PhaseRecovery, Generation: profile.Generation, Code: platformcontrolv1.ErrorUnavailable}
			return bootstrapResult(s.status, platformcontrolv1.ErrorUnavailable, true)
		}
		// Test must never mutate the current binding.
		generation, _, ready := s.binding.Snapshot()
		phase := platformcontrolv1.PhaseUnconfigured
		if ready {
			phase = platformcontrolv1.PhaseReady
		}
		s.status = platformcontrolv1.Status{Phase: phase, Generation: generation}
		return bootstrapResult(s.status, "", false)
	case platformcontrolv1.OperationInitialize, platformcontrolv1.OperationOpen:
		return s.activate(ctx, operation, profile, source)
	default:
		return bootstrapResult(platformcontrolv1.Status{}, platformcontrolv1.ErrorInvalid, false)
	}
}

func (s *Service) activate(ctx context.Context, operation string, profile platformcontrolv1.Profile, source platformcontrol.SecretSource) (*contractv1.CallResult, []byte, error) {
	identity := platformcontrol.ProfileIdentity(profile)
	currentGeneration, currentIdentity, ready := s.binding.Snapshot()
	if ready && currentGeneration == profile.Generation && currentIdentity == identity {
		s.status = platformcontrolv1.Status{Phase: platformcontrolv1.PhaseReady, Generation: currentGeneration}
		return bootstrapResult(s.status, "", false)
	}
	if currentGeneration >= profile.Generation {
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
		s.status = platformcontrolv1.Status{Phase: platformcontrolv1.PhaseRecovery, Generation: profile.Generation, Code: platformcontrolv1.ErrorInitializationFailed}
		return bootstrapResult(s.status, platformcontrolv1.ErrorInitializationFailed, true)
	}
	if err := s.binding.Bind(profile.Generation, identity, candidate); err != nil {
		_ = candidate.Close()
		return bootstrapResult(s.status, platformcontrolv1.ErrorConflict, false)
	}
	previous := s.managed
	s.managed = candidate
	s.status = platformcontrolv1.Status{Phase: platformcontrolv1.PhaseReady, Generation: profile.Generation}
	if previous != nil {
		_ = previous.Close()
	}
	return bootstrapResult(s.status, "", false)
}

func trustedBootstrapCaller(call *contractv1.CallContext) bool {
	return call != nil && call.GetCaller() != nil &&
		call.GetCaller().GetKind() == contractv1.CallerKind_CALLER_KIND_SYSTEM &&
		call.GetCaller().GetId() == platformcontrolv1.TrustedBootstrapSystemID
}

func bootstrapResult(status platformcontrolv1.Status, code string, retryable bool) (*contractv1.CallResult, []byte, error) {
	if code != "" {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{
			Code: code, Message: "Platform Control SQL Bootstrap 请求失败", Retryable: retryable,
		}}, nil, nil
	}
	raw, err := json.Marshal(status)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

var _ interface{ Close() error } = (*Service)(nil)
