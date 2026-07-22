// Package session projects signed authorization policy into bounded Portal sessions.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	PluginID      = "cn.vastplan.foundation.security.authorization-session"
	PluginVersion = "0.1.0"
	Capability    = "foundation.security.authorization-session"
)

type Resolver struct {
	store     SnapshotStore
	directory GroupDirectory
	now       func() time.Time
}

type GroupDirectory interface {
	Groups(subjectID string) ([]authorizationv1.ExternalGroup, uint64, error)
}

type EmptyGroupDirectory struct{}

func (EmptyGroupDirectory) Groups(string) ([]authorizationv1.ExternalGroup, uint64, error) {
	return []authorizationv1.ExternalGroup{}, 0, nil
}

func NewResolver(store SnapshotStore) (*Resolver, error) {
	return NewResolverWithDirectory(store, EmptyGroupDirectory{})
}

func NewResolverWithDirectory(store SnapshotStore, directory GroupDirectory) (*Resolver, error) {
	if store == nil {
		return nil, errors.New("Authorization Session 需要 Policy Snapshot Store")
	}
	if directory == nil {
		directory = EmptyGroupDirectory{}
	}
	return &Resolver{store: store, directory: directory, now: func() time.Time { return time.Now().UTC() }}, nil
}

func Descriptor() []byte {
	return []byte(`{"title":"企业身份会话授权","subcommands":[{"name":"resolve","description":"将稳定认证主体解析为内部会话权限"}]}`)
}

func (r *Resolver) Contribution() sdk.Contribution {
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: map[string]sdk.Handler{"resolve": r.resolve}}
}

func (r *Resolver) resolve(_ context.Context, _ sdk.Host, callCtx *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
	if callCtx == nil || callCtx.Caller == nil || callCtx.Caller.Kind != contractv1.CallerKind_CALLER_KIND_SYSTEM || callCtx.Scene != "portal.bff" {
		return failure("foundation.authorization-session.forbidden", errors.New("只有可信 Portal BFF 可以解析会话授权")), nil, nil
	}
	request, err := parseResolveRequest(raw)
	if err != nil {
		return failure("foundation.authorization-session.invalid-request", err), nil, nil
	}
	if callCtx.TenantId != "" && callCtx.TenantId != request.TenantID {
		return failure("foundation.authorization-session.tenant-mismatch", errors.New("CallContext tenant 与请求不一致")), nil, nil
	}
	snapshot, err := r.store.Load()
	if err != nil {
		return failure("foundation.authorization-session.snapshot-unavailable", err), nil, nil
	}
	now := r.now()
	if now.Before(snapshot.Payload.NotBefore) || !now.Before(snapshot.Payload.ExpiresAt) || !contains(snapshot.Payload.Audience, "portal:"+request.TenantID+":"+request.PortalID) {
		return failure("foundation.authorization-session.snapshot-inactive", errors.New("Policy Snapshot 未生效、已过期或 audience 不匹配")), nil, nil
	}
	subjectID := StableSubjectID(request.ProviderProfileID, request.Issuer, request.Subject)
	groups, _, err := r.directory.Groups(subjectID)
	if err != nil {
		return failure("foundation.authorization-session.directory-unavailable", err), nil, nil
	}
	permissions := resolvePermissions(snapshot.Payload.Policy, subjectID, groups, now)
	if len(permissions) == 0 {
		return failure("foundation.authorization-session.subject-unbound", errors.New("稳定主体没有有效授权绑定")), nil, nil
	}
	digest, err := authorizationv1.AuthorizationIRDigest(snapshot.Payload.Policy)
	if err != nil {
		return nil, nil, err
	}
	result := ResolveResult{SubjectID: subjectID, TenantID: request.TenantID, Roles: permissions, Policy: PolicyRef{ID: snapshot.Payload.SnapshotID, Revision: snapshot.Payload.Revision, Digest: digest}, ExpiresAt: snapshot.Payload.ExpiresAt.UTC().Format(time.RFC3339Nano)}
	response, _ := json.Marshal(result)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, response, nil
}

func resolvePermissions(policy authorizationv1.AuthorizationIR, subjectID string, groups []authorizationv1.ExternalGroup, now time.Time) []string {
	revoked := map[string]struct{}{}
	for _, item := range policy.Revocations {
		if !now.Before(item.EffectiveAt) {
			revoked[item.Kind+"\x00"+item.TargetID] = struct{}{}
		}
	}
	if _, denied := revoked["subject\x00"+subjectID]; denied {
		return []string{}
	}
	roles := map[string]authorizationv1.CompiledRole{}
	for _, role := range policy.Roles {
		roles[fmt.Sprintf("%s@%d", role.ID, role.Revision)] = role
	}
	allowed, denied := map[string]struct{}{}, map[string]struct{}{}
	externalGroups := map[string]struct{}{}
	for _, group := range groups {
		externalGroups[group.Issuer+"\x00"+group.ID] = struct{}{}
	}
	for _, binding := range policy.Bindings {
		directUser := binding.Subject.Kind == authorizationv1.SubjectUser && binding.Subject.ID == subjectID && binding.Subject.Issuer == StableSubjectIssuer
		_, externalGroup := externalGroups[binding.Subject.Issuer+"\x00"+binding.Subject.ID]
		if (!directUser && (binding.Subject.Kind != authorizationv1.SubjectGroup || !externalGroup)) || now.Before(binding.NotBefore) || !now.Before(binding.ExpiresAt) {
			continue
		}
		if _, off := revoked["binding\x00"+binding.ID]; off {
			continue
		}
		if _, off := revoked["role\x00"+binding.RoleID]; off {
			continue
		}
		role, exists := roles[fmt.Sprintf("%s@%d", binding.RoleID, binding.RoleRevision)]
		if !exists {
			continue
		}
		for _, statement := range role.Statements {
			// Session projection contains only global unconditional permissions.
			// Resource/attribute constrained decisions remain in the policy engine.
			if statement.Resource != nil || len(statement.Constraints) != 0 {
				continue
			}
			target := allowed
			if statement.Effect == authorizationv1.EffectDeny {
				target = denied
			}
			for _, permission := range statement.Permissions {
				target[permission] = struct{}{}
			}
		}
	}
	for permission := range denied {
		delete(allowed, permission)
	}
	result := make([]string, 0, len(allowed))
	for permission := range allowed {
		result = append(result, permission)
	}
	sort.Strings(result)
	return result
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
func failure(code string, err error) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error()}}
}
