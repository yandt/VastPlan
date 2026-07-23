package credentials

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/configurationauthority"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type authorityHost struct {
	claims configurationauthority.Claims
	calls  int
}

func (h *authorityHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	h.calls++
	if target.GetExtensionPoint() != extpoint.KernelService || target.GetCapability() != configurationauthority.KernelConsumeService || target.GetOperation() != "consume" || !strings.Contains(string(payload), configurationauthority.TokenPrefix) {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR}, nil, nil
	}
	raw, _ := json.Marshal(h.claims)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

var _ sdk.Host = (*authorityHost)(nil)

func TestDelegatedStageDerivesOwnerPurposeAndLifecycleFromAuthority(t *testing.T) {
	now := time.Now().UTC()
	claims := configurationauthority.Claims{
		SchemaVersion: configurationauthority.SchemaVersion, AuthorityID: configurationauthority.TokenPrefix + strings.Repeat("a", 64), TenantID: "tenant-a",
		ConfigurationID: "cfg_" + strings.Repeat("b", 24), CatalogDigest: strings.Repeat("c", 64), Deployment: "platform", UnitID: "api",
		CandidateID: "pcfg_" + strings.Repeat("d", 32), FieldID: "token", Owner: "cn.example.target", Purpose: "remote.token",
		Resource: "plugin-configuration/resource", ArtifactSHA256: strings.Repeat("e", 64), SchemaDigest: strings.Repeat("f", 64),
		IssuedAt: now.Add(-time.Second), ExpiresAt: now.Add(30 * time.Second),
	}
	host := &authorityHost{claims: claims}
	service, err := openTestService(filepath.Join(t.TempDir(), "credentials.json"), &fakeTransit{})
	if err != nil {
		t.Fatal(err)
	}
	coordinator := managedContext("tenant-a", configurationauthority.CoordinatorPluginID)
	staged, err := service.StageDelegated(context.Background(), host, coordinator, configurationauthority.TokenPrefix+strings.Repeat("1", 64), []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if host.calls != 1 || staged.Ref.Owner != claims.Owner || staged.Ref.Purpose != claims.Purpose || staged.Ref.Scope != "tenant" {
		t.Fatalf("委托凭证未从授权派生身份: staged=%+v calls=%d", staged, host.calls)
	}
	if _, err := service.ActivateManaged(managedContext("tenant-a", claims.Owner), staged.ID); err == nil {
		t.Fatal("目标插件不得绕过配置候选直接激活委托凭证")
	}
	if _, err := service.PrepareDelegated(coordinator, staged.ID, "pcfg_"+strings.Repeat("0", 32)); err == nil {
		t.Fatal("错误 candidate 不得激活委托凭证")
	}
	request, recipient, err := credentiallease.NewRequest(staged.Ref)
	if err != nil {
		t.Fatal(err)
	}
	kernel := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "backend/runtime-candidate"}}
	if _, err := service.IssueMaterialLease(context.Background(), kernel, request); err == nil {
		t.Fatal("Preparing 委托凭证不得签发 material lease")
	}
	recipient.Discard()
	if _, err := service.PrepareDelegated(coordinator, staged.ID, claims.CandidateID); err != nil {
		t.Fatalf("配置协调器无法打开候选使用窗口: %v", err)
	}
	request, recipient, _ = credentiallease.NewRequest(staged.Ref)
	envelope, err := service.IssueMaterialLease(context.Background(), kernel, request)
	if err != nil {
		t.Fatalf("Candidate 委托凭证应允许候选宿主签发 material lease: %v", err)
	}
	material, err := recipient.Open(envelope, credentiallease.Claims{TenantID: "tenant-a", Audience: "backend/runtime-candidate", Ref: staged.Ref}, time.Now().UTC())
	if err != nil || string(material) != "secret" {
		t.Fatalf("Candidate material lease 无法解封: material=%q err=%v", material, err)
	}
	if _, err := service.ActivateDelegated(coordinator, staged.ID, claims.CandidateID); err != nil {
		t.Fatalf("配置协调器无法激活绑定候选: %v", err)
	}
	if _, err := service.RetireManaged(managedContext("tenant-a", claims.Owner), staged.Ref.Handle); err != nil {
		t.Fatalf("激活后目标 owner 应能退役凭证: %v", err)
	}
}

func TestDelegatedStageRejectsUntrustedCoordinatorBeforeConsumingAuthority(t *testing.T) {
	host := &authorityHost{}
	service, _ := openTestService(filepath.Join(t.TempDir(), "credentials.json"), &fakeTransit{})
	if _, err := service.StageDelegated(context.Background(), host, managedContext("tenant-a", "cn.example.attacker"), configurationauthority.TokenPrefix+strings.Repeat("1", 64), []byte("secret")); err == nil || host.calls != 0 {
		t.Fatalf("非协调器必须在消费授权前拒绝: calls=%d err=%v", host.calls, err)
	}
}
