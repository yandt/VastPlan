package seedaccess

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const HandoffCapability = "foundation.security.seed.handoff"

type PolicySnapshotStore interface {
	Load() (authorizationv1.SignedPolicySnapshot, error)
}

type HandoffService struct {
	authority  *Authority
	assertions AssertionProofVerifier
	policies   PolicySnapshotStore
	now        func() time.Time
}

func NewHandoffService(authority *Authority, assertions AssertionProofVerifier, policies PolicySnapshotStore) (*HandoffService, error) {
	if authority == nil || assertions == nil || policies == nil {
		return nil, errors.New("Seed Handoff 需要 Authority、Assertion Trust 与 Policy Snapshot Store")
	}
	return &HandoffService{authority: authority, assertions: assertions, policies: policies, now: func() time.Time { return time.Now().UTC() }}, nil
}

func HandoffDescriptor() []byte {
	return []byte(`{"title":"Seed 企业身份交接","subcommands":[{"name":"get","description":"读取 Seed 交接状态"},{"name":"configureProvider","description":"绑定已批准企业 Provider"},{"name":"verifyProvider","description":"验证 Broker 签名企业身份"},{"name":"prepareHandoff","description":"校验授权快照并准备交接"},{"name":"completeHandoff","description":"原子完成企业身份交接"}]}`)
}

func (s *HandoffService) Contribution() sdk.Contribution {
	handlers := map[string]sdk.Handler{}
	for _, operation := range []string{"get", "configureProvider", "verifyProvider", "prepareHandoff", "completeHandoff"} {
		op := operation
		handlers[op] = func(_ context.Context, _ sdk.Host, callCtx *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			return s.Handle(callCtx, op, raw)
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: HandoffCapability, Descriptor: HandoffDescriptor(), Handlers: handlers}
}

func (s *HandoffService) Handle(callCtx *contractv1.CallContext, operation string, raw []byte) (*contractv1.CallResult, []byte, error) {
	if callCtx == nil || callCtx.Caller == nil || (callCtx.Caller.Kind != contractv1.CallerKind_CALLER_KIND_USER && callCtx.Caller.Kind != contractv1.CallerKind_CALLER_KIND_SYSTEM) || callCtx.Scene != "portal.bff" {
		return handoffFailure("foundation.seed-handoff.forbidden", errors.New("Seed 交接只接受可信 Portal 管理调用")), nil, nil
	}
	if operation == "get" {
		state, err := s.authority.Status()
		return handoffResult(state, err)
	}
	var request handoffRequest
	if err := strictHandoffJSON(raw, &request); err != nil {
		return handoffFailure("foundation.seed-handoff.invalid-request", err), nil, nil
	}
	var state State
	var err error
	switch operation {
	case "configureProvider":
		state, err = s.authority.ConfigureProvider(request.ExpectedGeneration, request.ProviderProfile)
	case "verifyProvider":
		state, err = s.verifyProvider(request)
	case "prepareHandoff":
		state, err = s.prepareHandoff(request)
	case "completeHandoff":
		state, err = s.authority.CompleteHandoff(request.ExpectedGeneration, request.SealDigest)
	default:
		return handoffFailure("foundation.seed-handoff.operation-unknown", errors.New("Seed Handoff operation 未登记")), nil, nil
	}
	return handoffResult(state, err)
}

type handoffRequest struct {
	ExpectedGeneration uint64                                         `json:"expectedGeneration"`
	ProviderProfile    compositioncommonv1.Ref                        `json:"providerProfile"`
	Assertion          authenticationv1.SignedAuthenticationAssertion `json:"assertion"`
	RecoveryReady      bool                                           `json:"recoveryReady"`
	SealDigest         string                                         `json:"sealDigest"`
}

func (s *HandoffService) verifyProvider(request handoffRequest) (State, error) {
	assertion, err := s.verifyAssertion(request.Assertion, request.ProviderProfile)
	if err != nil {
		return State{}, err
	}
	return s.authority.VerifyProvider(request.ExpectedGeneration, request.ProviderProfile, assertion.Payload.Subject)
}

func (s *HandoffService) prepareHandoff(request handoffRequest) (State, error) {
	assertion, err := s.verifyAssertion(request.Assertion, request.ProviderProfile)
	if err != nil {
		return State{}, err
	}
	snapshot, err := s.policies.Load()
	if err != nil {
		return State{}, err
	}
	now := s.now()
	if now.Before(snapshot.Payload.NotBefore) || !now.Before(snapshot.Payload.ExpiresAt) || !containsAudience(snapshot.Payload.Audience, "portal:"+assertion.Payload.TenantID+":"+assertion.Payload.PortalID) {
		return State{}, errors.New("交接 Policy Snapshot 未生效、已过期或 audience 不匹配")
	}
	subjectID := authenticationv1.StableSubjectID(assertion.Payload.ProviderProfileID, assertion.Payload.Subject.Issuer, assertion.Payload.Subject.ID)
	if !hasActiveSubjectBinding(snapshot.Payload.Policy, subjectID, now) {
		return State{}, errors.New("企业主体尚未绑定有效内部授权")
	}
	digest, err := authorizationv1.AuthorizationIRDigest(snapshot.Payload.Policy)
	if err != nil {
		return State{}, err
	}
	expiresAt := now.Add(5 * time.Minute)
	if snapshot.Payload.ExpiresAt.Before(expiresAt) {
		expiresAt = snapshot.Payload.ExpiresAt
	}
	seal := HandoffSeal{ProviderProfile: request.ProviderProfile, Subject: assertion.Payload.Subject, PolicySnapshot: compositioncommonv1.Ref{ID: snapshot.Payload.SnapshotID, Revision: snapshot.Payload.Revision, Digest: digest}, SessionID: assertion.Payload.AssertionID, AuthenticatedAt: assertion.Payload.IssuedAt, ExpiresAt: expiresAt, RecoveryReady: request.RecoveryReady}
	return s.authority.PrepareHandoff(request.ExpectedGeneration, seal)
}

func (s *HandoffService) verifyAssertion(signed authenticationv1.SignedAuthenticationAssertion, profile compositioncommonv1.Ref) (authenticationv1.SignedAuthenticationAssertion, error) {
	raw, _ := json.Marshal(signed)
	parsed, err := authenticationv1.ParseSignedAssertion(raw)
	if err != nil {
		return authenticationv1.SignedAuthenticationAssertion{}, err
	}
	if err := s.assertions.Verify(parsed); err != nil {
		return authenticationv1.SignedAuthenticationAssertion{}, err
	}
	now := s.now()
	if parsed.Payload.ProviderProfileID != profile.ID || !strings.HasPrefix(parsed.Payload.Audience, "portal:") || now.Before(parsed.Payload.IssuedAt.Add(-5*time.Second)) || !now.Before(parsed.Payload.ExpiresAt) {
		return authenticationv1.SignedAuthenticationAssertion{}, errors.New("企业身份 Assertion 与 Provider、audience 或时间窗不匹配")
	}
	return parsed, nil
}

func hasActiveSubjectBinding(policy authorizationv1.AuthorizationIR, subjectID string, now time.Time) bool {
	revoked := map[string]struct{}{}
	for _, item := range policy.Revocations {
		if !now.Before(item.EffectiveAt) {
			revoked[item.Kind+"\x00"+item.TargetID] = struct{}{}
		}
	}
	if _, denied := revoked["subject\x00"+subjectID]; denied {
		return false
	}
	for _, binding := range policy.Bindings {
		if binding.Subject.Kind == authorizationv1.SubjectUser && binding.Subject.ID == subjectID && binding.Subject.Issuer == authenticationv1.StableSubjectIssuer && !now.Before(binding.NotBefore) && now.Before(binding.ExpiresAt) {
			if _, denied := revoked["binding\x00"+binding.ID]; !denied {
				if _, denied = revoked["role\x00"+binding.RoleID]; !denied {
					return true
				}
			}
		}
	}
	return false
}

func (a *Authority) Status() (State, error) { return a.store.Load() }
func strictHandoffJSON(raw []byte, output any) error {
	if len(raw) > 1<<20 {
		return errors.New("Seed Handoff 请求超过上限")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("Seed Handoff 请求只能包含一个 JSON 文档")
	}
	return nil
}
func containsAudience(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func handoffResult(state State, err error) (*contractv1.CallResult, []byte, error) {
	if err != nil {
		return handoffFailure("foundation.seed-handoff.rejected", err), nil, nil
	}
	raw, _ := json.Marshal(state)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}
func handoffFailure(code string, err error) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error()}}
}
