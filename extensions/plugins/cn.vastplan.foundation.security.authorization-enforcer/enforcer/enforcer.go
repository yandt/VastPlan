package enforcer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
)

const (
	PluginID      = "cn.vastplan.foundation.security.authorization-enforcer"
	PluginVersion = "0.1.0"
	Capability    = "foundation.security.authorization-enforcer"
)

type cacheEntry struct {
	response   extpoint.PermissionResponse
	validUntil time.Time
}

type Enforcer struct {
	source    PolicySource
	directory GroupDirectory
	audience  map[string]struct{}
	now       func() time.Time
	mu        sync.Mutex
	bundle    *PolicyBundle
	cache     map[string]cacheEntry
}

func New(source PolicySource, directory GroupDirectory, audience []string) (*Enforcer, error) {
	if source == nil {
		return nil, errors.New("Authorization Enforcer 需要 Policy Source")
	}
	if directory == nil {
		directory = EmptyGroupDirectory{}
	}
	allowed := map[string]struct{}{}
	for _, value := range audience {
		if strings.TrimSpace(value) != "" {
			allowed[strings.TrimSpace(value)] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return nil, errors.New("Authorization Enforcer 需要 audience")
	}
	return &Enforcer{source: source, directory: directory, audience: allowed, now: func() time.Time { return time.Now().UTC() }, cache: map[string]cacheEntry{}}, nil
}

func (e *Enforcer) Check(_ context.Context, callCtx *contractv1.CallContext, raw []byte) (extpoint.PermissionResponse, error) {
	var request extpoint.PermissionRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return extpoint.PermissionResponse{}, err
	}
	if callCtx == nil || callCtx.Caller == nil {
		return extpoint.PermissionResponse{Decision: extpoint.DecisionDeny, Reason: "缺少调用身份"}, nil
	}
	if callCtx.Caller.Kind != contractv1.CallerKind_CALLER_KIND_USER {
		return extpoint.PermissionResponse{Decision: extpoint.DecisionAbstain, Reason: "非用户调用交给 workload 策略"}, nil
	}
	bundle, err := e.loadBundle()
	if err != nil {
		return extpoint.PermissionResponse{Decision: extpoint.DecisionDeny, Reason: "授权策略不可用"}, nil
	}
	guard, permissionEntries, governed := operationGuard(bundle.Catalog, request)
	if !governed {
		return extpoint.PermissionResponse{Decision: extpoint.DecisionAbstain, Reason: "操作不在权限目录"}, nil
	}
	now := e.now()
	if err := e.validateBundle(bundle, now); err != nil {
		return extpoint.PermissionResponse{Decision: extpoint.DecisionDeny, Reason: err.Error()}, nil
	}
	if callCtx.Principal == nil || callCtx.Principal.UserId == "" || callCtx.Caller.Id != callCtx.Principal.UserId {
		return extpoint.PermissionResponse{Decision: extpoint.DecisionDeny, Reason: "用户主体不可信"}, nil
	}
	groups, directoryRevision, err := e.directory.Groups(callCtx.Principal.UserId)
	if err != nil {
		return extpoint.PermissionResponse{Decision: extpoint.DecisionDeny, Reason: "主体目录不可用"}, nil
	}
	input := authorizationv1.EvaluationInput{
		RequestID: requestID(callCtx), PolicyDigest: bundle.PolicyDigest, DomainID: selectDomain(bundle.Snapshot.Payload.Policy, callCtx),
		Subject: authorizationv1.Subject{Kind: authorizationv1.SubjectUser, ID: callCtx.Principal.UserId, Issuer: authenticationv1.StableSubjectIssuer}, ExternalGroups: groups,
		Target: authorizationv1.EvaluationTarget{ExtensionPoint: request.ExtensionPoint, Capability: request.Capability, Operation: request.Operation},
		Scope:  authorizationv1.EvaluationScope{TenantID: callCtx.TenantId, ProjectID: callCtx.GetProjectId()}, RequiredPermissions: append([]string(nil), guard.Permissions...), EvaluatedAt: now,
	}
	input.ContextDigest = contextDigest(input, directoryRevision, bundle.Snapshot.Payload.Policy.RevocationRevision)
	if cached, ok := e.cached(input.ContextDigest, now); ok {
		return cached, nil
	}
	evaluation := Evaluate(bundle.Snapshot.Payload.Policy, input, now)
	response := extpoint.PermissionResponse{Decision: extpoint.DecisionDeny, Reason: evaluation.ReasonCode}
	if evaluation.Decision == authorizationv1.DecisionAllow {
		response.Decision = extpoint.DecisionAllow
	}
	e.storeCache(input.ContextDigest, response, decisionTTL(permissionEntries), bundle.Snapshot.Payload.ExpiresAt, now)
	return response, nil
}

func (e *Enforcer) loadBundle() (PolicyBundle, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	loaded, err := e.source.Load()
	if err == nil {
		if loaded.PolicyDigest == "" {
			loaded.PolicyDigest, err = authorizationv1.AuthorizationIRDigest(loaded.Snapshot.Payload.Policy)
			if err != nil {
				return PolicyBundle{}, err
			}
		}
		if e.bundle == nil || e.bundle.Snapshot.Payload.Revision != loaded.Snapshot.Payload.Revision || e.bundle.Snapshot.Payload.Policy.RevocationRevision != loaded.Snapshot.Payload.Policy.RevocationRevision {
			e.cache = map[string]cacheEntry{}
		}
		e.bundle = &loaded
		return loaded, nil
	}
	if e.bundle != nil && e.now().Before(e.bundle.Snapshot.Payload.ExpiresAt) {
		return *e.bundle, nil
	}
	return PolicyBundle{}, err
}

func (e *Enforcer) validateBundle(bundle PolicyBundle, now time.Time) error {
	if bundle.Catalog.Digest != bundle.Snapshot.Payload.Policy.CatalogDigest {
		return errors.New("Policy Snapshot 与 Permission Catalog digest 不一致")
	}
	if now.Before(bundle.Snapshot.Payload.NotBefore) || !now.Before(bundle.Snapshot.Payload.ExpiresAt) {
		return errors.New("Policy Snapshot 未生效或已过期")
	}
	for _, audience := range bundle.Snapshot.Payload.Audience {
		if _, ok := e.audience[audience]; ok {
			return nil
		}
	}
	return errors.New("Policy Snapshot audience 不匹配")
}

func (e *Enforcer) cached(key string, now time.Time) (extpoint.PermissionResponse, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	value, ok := e.cache[key]
	return value.response, ok && now.Before(value.validUntil)
}
func (e *Enforcer) storeCache(key string, response extpoint.PermissionResponse, ttl time.Duration, expiresAt, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	until := now.Add(ttl)
	if expiresAt.Before(until) {
		until = expiresAt
	}
	e.cache[key] = cacheEntry{response: response, validUntil: until}
	if len(e.cache) > 4096 {
		e.cache = map[string]cacheEntry{key: e.cache[key]}
	}
}

func operationGuard(catalog pluginv1.PermissionCatalog, request extpoint.PermissionRequest) (pluginv1.PermissionOperationEntry, []pluginv1.PermissionCatalogEntry, bool) {
	permissions := map[string]pluginv1.PermissionCatalogEntry{}
	for _, entry := range catalog.Permissions {
		permissions[entry.Code] = entry
	}
	for _, operation := range catalog.Operations {
		if operation.ExtensionPoint != request.ExtensionPoint || operation.Capability != request.Capability || operation.Operation != request.Operation {
			continue
		}
		entries := make([]pluginv1.PermissionCatalogEntry, 0, len(operation.Permissions))
		for _, code := range operation.Permissions {
			entries = append(entries, permissions[code])
		}
		return operation, entries, true
	}
	return pluginv1.PermissionOperationEntry{}, nil, false
}

func selectDomain(policy authorizationv1.AuthorizationIR, callCtx *contractv1.CallContext) string {
	if project := callCtx.GetProjectId(); project != "" {
		for _, domain := range policy.Domains {
			if domain.Kind == authorizationv1.DomainProject && domain.Scope.TenantID == callCtx.TenantId && domain.Scope.ProjectID == project {
				return domain.ID
			}
		}
	}
	if tenant := callCtx.TenantId; tenant != "" {
		for _, domain := range policy.Domains {
			if domain.Kind == authorizationv1.DomainTenant && domain.Scope.TenantID == tenant {
				return domain.ID
			}
		}
	}
	return policy.RootDomainID
}

func decisionTTL(entries []pluginv1.PermissionCatalogEntry) time.Duration {
	rank := 0
	for _, entry := range entries {
		if value := map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}[entry.Risk]; value > rank {
			rank = value
		}
	}
	if rank >= 3 {
		return 5 * time.Second
	}
	return 5 * time.Minute
}
func requestID(callCtx *contractv1.CallContext) string {
	if callCtx.Trace != nil && callCtx.Trace.TraceId != "" {
		return callCtx.Trace.TraceId
	}
	return "authorization.check"
}
func contextDigest(input authorizationv1.EvaluationInput, directoryRevision, revocationRevision uint64) string {
	groups := append([]authorizationv1.ExternalGroup(nil), input.ExternalGroups...)
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Issuer != groups[j].Issuer {
			return groups[i].Issuer < groups[j].Issuer
		}
		return groups[i].ID < groups[j].ID
	})
	input.ExternalGroups = groups
	raw, _ := json.Marshal(struct {
		Input              authorizationv1.EvaluationInput `json:"input"`
		DirectoryRevision  uint64                          `json:"directoryRevision"`
		RevocationRevision uint64                          `json:"revocationRevision"`
	}{input, directoryRevision, revocationRevision})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
func EncodeResponse(response extpoint.PermissionResponse) ([]byte, error) {
	return json.Marshal(response)
}
func ErrorResponse(err error) extpoint.PermissionResponse {
	return extpoint.PermissionResponse{Decision: extpoint.DecisionDeny, Reason: fmt.Sprintf("Authorization Enforcer 错误: %v", err)}
}
