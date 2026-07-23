package credentialbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/callcontext"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
	"cdsoft.com.cn/VastPlan/core/shared/go/runtimeidentity"
)

func databaseRuntimeIdentity() runtimeidentity.Identity {
	return runtimeidentity.Identity{
		PluginID: DatabaseRuntimePluginID, Publisher: "vastplan", Version: "0.2.0",
		ArtifactSHA256: strings.Repeat("a", 64), NodeID: "node-a", RuntimeScope: "database-service", InstanceID: "runtime-a",
	}
}

func databaseCredentialRef() commonv1.ManagedCredentialRef {
	return commonv1.ManagedCredentialRef{
		Handle: "credential://managed/database-primary", Scope: "tenant", Owner: DatabaseCredentialOwner,
		Purpose: DatabaseCredentialPurpose, Version: 1,
	}
}

func TestRuntimeLeaseRelaysCiphertextToExactRuntimeAudience(t *testing.T) {
	identity, ref := databaseRuntimeIdentity(), databaseCredentialRef()
	audience, _ := identity.Audience()
	secret := []byte("database-password")
	now := time.Now().UTC()
	broker, err := NewRuntimeLease(func(ctx context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if _, ok := callcontext.FromContext(ctx); !ok {
			t.Fatal("runtime relay 必须建立可信 SYSTEM provenance")
		}
		if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_SYSTEM || call.GetCaller().GetId() != audience || call.GetTenantId() != "tenant-a" {
			t.Fatalf("runtime audience 未绑定到系统调用: %+v", call)
		}
		if target.GetCapability() != materialLeaseCapability || bytes.Contains(payload, secret) {
			t.Fatal("relay 目标错误或请求泄露明文")
		}
		var request credentiallease.Request
		if err := json.Unmarshal(payload, &request); err != nil {
			t.Fatal(err)
		}
		envelope, err := credentiallease.Seal(request, credentiallease.Claims{TenantID: "tenant-a", Audience: audience, Ref: ref}, secret, now, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(envelope)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	broker.now = func() time.Time { return now }
	request, recipient, err := credentiallease.NewRequest(ref)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := broker.IssueRuntimeLease(context.Background(), "tenant-a", identity, request)
	if err != nil {
		t.Fatal(err)
	}
	material, err := recipient.Open(envelope, credentiallease.Claims{TenantID: "tenant-a", Audience: audience, Ref: ref}, now)
	if err != nil || !bytes.Equal(material, secret) {
		t.Fatalf("Database Runtime 无法解封 relay: %q err=%v", material, err)
	}
}

func TestRuntimeLeaseRejectsUntrustedIdentityAndCredentialScopeBeforeInvocation(t *testing.T) {
	called := false
	broker, _ := NewRuntimeLease(func(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
		called = true
		return nil, nil, nil
	})
	identity := databaseRuntimeIdentity()
	identity.Publisher = "attacker"
	request, _, _ := credentiallease.NewRequest(databaseCredentialRef())
	if _, err := broker.IssueRuntimeLease(context.Background(), "tenant-a", identity, request); err == nil || called {
		t.Fatalf("第三方 Runtime 必须在调用凭证服务前拒绝: err=%v called=%v", err, called)
	}
	identity = databaseRuntimeIdentity()
	ref := databaseCredentialRef()
	ref.Owner = "cn.vastplan.platform.other"
	request, _, _ = credentiallease.NewRequest(ref)
	if _, err := broker.IssueRuntimeLease(context.Background(), "tenant-a", identity, request); err == nil || called {
		t.Fatalf("跨 owner 凭证必须在调用前拒绝: err=%v called=%v", err, called)
	}
}

func TestRuntimeCredentialGrantTableRequiresExactPluginOwnerPurposeTuple(t *testing.T) {
	tests := []struct{ pluginID, owner, purpose string }{
		{DatabaseRuntimePluginID, DatabaseCredentialOwner, DatabaseCredentialPurpose},
		{OIDCProviderPluginID, OIDCProviderPluginID, OIDCCredentialPurpose},
		{WebhookProviderPluginID, WebhookProviderPluginID, WebhookCredentialPurpose},
		{AssessmentProviderPluginID, AssessmentProviderPluginID, AssessmentCredentialPurpose},
	}
	for _, item := range tests {
		identity := databaseRuntimeIdentity()
		identity.PluginID = item.pluginID
		request, _, err := credentiallease.NewRequest(commonv1.ManagedCredentialRef{
			Handle: "credential://managed/exact", Scope: "tenant", Owner: item.owner, Purpose: item.purpose, Version: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := authorizeRuntimeCredential(identity, request); err != nil {
			t.Fatalf("精确授权 %s 被拒绝: %v", item.pluginID, err)
		}
		request.Ref.Purpose = DatabaseCredentialPurpose
		if item.purpose == DatabaseCredentialPurpose {
			request.Ref.Purpose = OIDCCredentialPurpose
		}
		if err := authorizeRuntimeCredential(identity, request); err == nil {
			t.Fatalf("跨 purpose 借权必须拒绝: %s", item.pluginID)
		}
	}
}
