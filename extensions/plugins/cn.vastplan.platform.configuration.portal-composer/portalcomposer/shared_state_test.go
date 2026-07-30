package portalcomposer

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func TestComposerStateIsSharedAcrossInstancesAndTenantIsolated(t *testing.T) {
	host := &configuredHost{state: newStateOnlyHost(t)}
	callA := &contractv1.CallContext{TenantId: "tenant-a", Principal: &contractv1.Principal{UserId: "author", SystemRoles: []string{"portal.compose"}}}
	first := New(nil)
	request := portalapi.PortalVersionRequest{PortalID: "admin", Configuration: portalapi.PortalConfiguration{Application: spec("/")}}
	created, raw, err := Contribution(first).Handlers["createPortal"](context.Background(), host, callA, mustJSON(t, request))
	if err != nil || created.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("首次实例创建失败: result=%+v err=%v", created, err)
	}
	var portal portalapi.Portal
	if err := json.Unmarshal(raw, &portal); err != nil || portal.ID != "admin" || len(portal.Versions) != 1 {
		t.Fatalf("首次实例响应错误: %s %v", raw, err)
	}
	second := New(nil)
	listed, raw, err := Contribution(second).Handlers["portalGovernance"](context.Background(), host, callA, []byte(`{}`))
	if err != nil || listed.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("第二实例读取失败: result=%+v err=%v", listed, err)
	}
	var governance portalapi.PortalGovernanceSnapshot
	if err := json.Unmarshal(raw, &governance); err != nil || len(governance.Portals) != 1 || governance.Portals[0].ID != portal.ID {
		t.Fatalf("第二实例未读取共享状态: %s %v", raw, err)
	}
	callB := &contractv1.CallContext{TenantId: "tenant-b", Principal: &contractv1.Principal{UserId: "author", SystemRoles: []string{"portal.compose"}}}
	result, raw, err := Contribution(New(nil)).Handlers["portalGovernance"](context.Background(), host, callB, []byte(`{}`))
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || string(raw) != `{"portals":[]}` {
		t.Fatalf("跨 tenant 状态泄漏: result=%+v raw=%s err=%v", result, raw, err)
	}
}

func TestPreferenceDocumentsArePerSubjectAndCASFenced(t *testing.T) {
	host := newStateOnlyHost(t)
	alice := portalapi.Principal{ID: "alice", TenantID: "tenant-a"}
	bob := portalapi.Principal{ID: "bob", TenantID: "tenant-a"}
	callAlice := preferenceCall(alice)
	scope := testPreferenceScope()
	first, err := newPreferenceStore(context.Background(), host, callAlice, alice)
	if err != nil {
		t.Fatal(err)
	}
	created, err := first.Put(alice, portalapi.PutPortalPreferenceRequest{Scope: scope, Values: portalapi.PortalPreferenceValues{RendererID: "mui"}})
	if err != nil || created.Revision != 1 {
		t.Fatalf("首次偏好写入失败: %+v %v", created, err)
	}
	reopened, err := newPreferenceStore(context.Background(), host, callAlice, alice)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.Get(alice, scope)
	if err != nil || loaded.Values.RendererID != "mui" {
		t.Fatalf("第二实例未读取用户偏好: %+v %v", loaded, err)
	}
	bobStore, err := newPreferenceStore(context.Background(), host, preferenceCall(bob), bob)
	if err != nil {
		t.Fatal(err)
	}
	missing, err := bobStore.Get(bob, scope)
	if err != nil || missing.Revision != 0 {
		t.Fatalf("subject 状态泄漏: %+v %v", missing, err)
	}

	competitor, err := newPreferenceStore(context.Background(), host, callAlice, alice)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Put(alice, portalapi.PutPortalPreferenceRequest{Scope: scope, ExpectedRevision: 1, Values: portalapi.PortalPreferenceValues{RendererID: "arco"}}); err != nil {
		t.Fatal(err)
	}
	_, err = competitor.Put(alice, portalapi.PutPortalPreferenceRequest{Scope: scope, ExpectedRevision: 1, Values: portalapi.PortalPreferenceValues{RendererID: "mui", ShellTemplateID: "standard"}})
	if !errors.Is(err, portalapi.ErrPreferenceConflict) {
		t.Fatalf("并发偏好 CAS 必须单赢家: %v", err)
	}
	if preferenceDocumentKey(alice.ID) == alice.ID {
		t.Fatal("Shared State key 不得暴露 subject")
	}
}

func TestPreferenceDocumentsRebaseConcurrentWritesToDifferentScopes(t *testing.T) {
	host := newStateOnlyHost(t)
	principal := portalapi.Principal{ID: "alice", TenantID: "tenant-a"}
	call := preferenceCall(principal)
	first, err := newPreferenceStore(context.Background(), host, call, principal)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newPreferenceStore(context.Background(), host, call, principal)
	if err != nil {
		t.Fatal(err)
	}
	firstScope := testPreferenceScope()
	secondScope := testPreferenceScope()
	secondScope.PortalID = "operations-secondary"
	if _, err := first.Put(principal, portalapi.PutPortalPreferenceRequest{
		Scope:  firstScope,
		Values: portalapi.PortalPreferenceValues{RendererID: "arco"},
	}); err != nil {
		t.Fatalf("首个 scope 写入失败: %v", err)
	}
	if _, err := second.Put(principal, portalapi.PutPortalPreferenceRequest{
		Scope:  secondScope,
		Values: portalapi.PortalPreferenceValues{RendererID: "antd"},
	}); err != nil {
		t.Fatalf("不同 scope 应在 Shared State CAS 冲突后自动重基: %v", err)
	}

	reopened, err := newPreferenceStore(context.Background(), host, call, principal)
	if err != nil {
		t.Fatal(err)
	}
	for scope, renderer := range map[portalapi.PortalPreferenceScope]string{
		firstScope:  "arco",
		secondScope: "antd",
	} {
		preference, err := reopened.Get(principal, scope)
		if err != nil || preference.Values.RendererID != renderer {
			t.Fatalf("重基后偏好丢失: scope=%s value=%+v err=%v", scope.PortalID, preference, err)
		}
	}
}

func preferenceCall(principal portalapi.Principal) *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: principal.TenantID, Scene: "portal.bff", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: principal.ID}, Principal: &contractv1.Principal{UserId: principal.ID}}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
