package authorizationnative

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const Capability = "foundation.security.authorization-engine.native"

type preparedPolicy struct {
	policy    authorizationv1.AuthorizationIR
	digest    string
	expiresAt time.Time
}

type Engine struct {
	now      func() time.Time
	mu       sync.Mutex
	policies map[string]preparedPolicy
	proofs   map[string]authorizationv1.EngineExplainResult
}

func NewEngine() *Engine {
	return &Engine{now: func() time.Time { return time.Now().UTC() }, policies: map[string]preparedPolicy{}, proofs: map[string]authorizationv1.EngineExplainResult{}}
}

func (e *Engine) Contribution() sdk.Contribution {
	operations := []string{"prepare", "evaluate", "explain", "health"}
	handlers := make(map[string]sdk.Handler, len(operations))
	for _, operation := range operations {
		op := operation
		handlers[op] = func(_ context.Context, _ sdk.Host, callCtx *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			if callCtx == nil || callCtx.Caller == nil || callCtx.Caller.Kind != contractv1.CallerKind_CALLER_KIND_SYSTEM {
				return failure("authorization.engine.forbidden", errors.New("Native Engine 只接受可信宿主调用")), nil, nil
			}
			value, err := e.handle(op, raw)
			if err != nil {
				return failure("authorization.engine.rejected", err), nil, nil
			}
			encoded, err := json.Marshal(value)
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, encoded, err
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: handlers}
}

func Descriptor() []byte {
	descriptions := map[string]string{"prepare": "准备签名策略快照", "evaluate": "求值并返回有界 Decision Proof", "explain": "解释本地 Decision Proof", "health": "读取 Engine Provider 健康状态"}
	subcommands := make([]map[string]string, 0, 4)
	for _, operation := range []string{"prepare", "evaluate", "explain", "health"} {
		subcommands = append(subcommands, map[string]string{"name": operation, "description": descriptions[operation]})
	}
	descriptor, _ := json.Marshal(map[string]any{"title": "VastPlan Native Authorization Engine", "subcommands": subcommands})
	return descriptor
}

func (e *Engine) handle(operation string, raw []byte) (any, error) {
	switch operation {
	case "prepare":
		var request authorizationv1.EnginePrepareRequest
		if err := strictJSON(raw, &request); err != nil {
			return nil, err
		}
		return e.Prepare(request)
	case "evaluate":
		var request authorizationv1.EngineEvaluateRequest
		if err := strictJSON(raw, &request); err != nil {
			return nil, err
		}
		return e.Evaluate(request)
	case "explain":
		var request authorizationv1.EngineExplainRequest
		if err := strictJSON(raw, &request); err != nil {
			return nil, err
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		result, ok := e.proofs[request.ProofID]
		if !ok {
			return nil, errors.New("Decision Proof 不存在")
		}
		return result, nil
	case "health":
		e.mu.Lock()
		defer e.mu.Unlock()
		e.pruneLocked(e.now())
		return authorizationv1.EngineHealthResult{Ready: true, PreparedPolicies: len(e.policies), ProviderID: "native-rbac"}, nil
	default:
		return nil, fmt.Errorf("未知 Native Engine 操作 %s", operation)
	}
}

func (e *Engine) Prepare(request authorizationv1.EnginePrepareRequest) (authorizationv1.EnginePrepareResult, error) {
	if err := authorizationv1.ValidateAuthorizationIR(request.Snapshot.Payload.Policy); err != nil {
		return authorizationv1.EnginePrepareResult{}, err
	}
	digest, err := authorizationv1.AuthorizationIRDigest(request.Snapshot.Payload.Policy)
	if err != nil {
		return authorizationv1.EnginePrepareResult{}, err
	}
	handle := digestValue([]byte(request.Snapshot.Payload.SnapshotID + "\x00" + digest))
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pruneLocked(e.now())
	e.policies[handle] = preparedPolicy{policy: request.Snapshot.Payload.Policy, digest: digest, expiresAt: request.Snapshot.Payload.ExpiresAt}
	return authorizationv1.EnginePrepareResult{Handle: handle, PolicyDigest: digest, ExpiresAt: request.Snapshot.Payload.ExpiresAt}, nil
}

func (e *Engine) Evaluate(request authorizationv1.EngineEvaluateRequest) (authorizationv1.EngineEvaluateResult, error) {
	now := e.now()
	e.mu.Lock()
	e.pruneLocked(now)
	prepared, ok := e.policies[request.Handle]
	e.mu.Unlock()
	if !ok || !now.Before(prepared.expiresAt) || request.Input.PolicyDigest != prepared.digest {
		return authorizationv1.EngineEvaluateResult{}, errors.New("Prepared Policy 不存在、已过期或摘要不匹配")
	}
	evaluation := Evaluate(prepared.policy, request.Input, now)
	inputRaw, _ := json.Marshal(request.Input)
	inputDigest := digestValue(inputRaw)
	validUntil := now.Add(5 * time.Minute)
	if prepared.expiresAt.Before(validUntil) {
		validUntil = prepared.expiresAt
	}
	proof := authorizationv1.DecisionProof{
		ProofID: digestValue([]byte(request.Handle + "\x00" + inputDigest + "\x00" + now.Format(time.RFC3339Nano))), ProviderID: "native-rbac",
		PolicyDigest: prepared.digest, InputDigest: inputDigest, Decision: evaluation.Decision, ReasonCode: evaluation.ReasonCode,
		MatchedRoleIDs: evaluation.MatchedRoleIDs, MatchedBindingIDs: evaluation.MatchedBindingIDs,
		RevocationRevision: prepared.policy.RevocationRevision, EvaluatedAt: now, ValidUntil: validUntil,
	}
	explanation := authorizationv1.EngineExplainResult{ProofID: proof.ProofID, Steps: []authorizationv1.ExplanationStep{{Code: evaluation.ReasonCode, Outcome: string(evaluation.Decision), References: append(append([]string{}, evaluation.MatchedRoleIDs...), evaluation.MatchedBindingIDs...)}}}
	e.mu.Lock()
	e.proofs[proof.ProofID] = explanation
	if len(e.proofs) > 4096 {
		e.proofs = map[string]authorizationv1.EngineExplainResult{proof.ProofID: explanation}
	}
	e.mu.Unlock()
	return authorizationv1.EngineEvaluateResult{Decision: evaluation.Decision, Proof: proof}, nil
}

func (e *Engine) pruneLocked(now time.Time) {
	for handle, policy := range e.policies {
		if !now.Before(policy.expiresAt) {
			delete(e.policies, handle)
		}
	}
}

func strictJSON(raw []byte, target any) error {
	if len(raw) > 4<<20 {
		return errors.New("Native Engine 请求超过 4 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("Native Engine 请求只能包含一个 JSON document")
	}
	return nil
}

func digestValue(raw []byte) string { value := sha256.Sum256(raw); return hex.EncodeToString(value[:]) }
func failure(code string, err error) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error()}}
}
