package credentialbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/callcontext"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
)

func TestManagedLeaseOpensOnlyInsideCallbackAndWipes(t *testing.T) {
	ref := kernelspi.CredentialRef{Handle: "credential://managed/stage-a", Scope: "tenant", Owner: "plugin.database", Purpose: "database.connection", Version: 1}
	secret := []byte("database-password")
	now := time.Now().UTC()
	invoker := func(ctx context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if _, ok := callcontext.FromContext(ctx); !ok {
			t.Fatal("内核调用必须携带不可上网的 trusted provenance")
		}
		if target.GetCapability() != materialLeaseCapability || target.GetOperation() != "issue" || target.GetLogicalService() != materialLeaseLogicalService || target.GetRoutingDomain() != materialLeaseRoutingDomain {
			t.Fatalf("调用目标错误: %+v", target)
		}
		if call.GetTenantId() != "tenant-a" || call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_SYSTEM || call.GetCaller().GetId() != "node-a" {
			t.Fatalf("宿主调用身份错误: %+v", call)
		}
		if bytes.Contains(payload, secret) {
			t.Fatal("material lease 请求不得包含明文")
		}
		var request credentiallease.Request
		if err := json.Unmarshal(payload, &request); err != nil {
			t.Fatal(err)
		}
		envelope, err := credentiallease.Seal(request, credentiallease.Claims{TenantID: "tenant-a", Audience: "node-a", Ref: ref}, secret, now, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		response, _ := json.Marshal(envelope)
		if bytes.Contains(response, secret) {
			t.Fatal("material lease 响应不得包含明文")
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, response, nil
	}
	broker, err := NewManagedLease("node-a", invoker)
	if err != nil {
		t.Fatal(err)
	}
	broker.now = func() time.Time { return now.Add(time.Second) }
	var observed []byte
	err = broker.WithCredential(context.Background(), kernelspi.Scope{TenantID: "tenant-a", PluginID: "plugin.database", Namespace: "database.probe"}, ref, func(value kernelspi.CredentialMaterial) error {
		observed = value.Bytes()
		if !bytes.Equal(observed, secret) {
			t.Fatalf("material 错误: %q", observed)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range observed {
		if value != 0 {
			t.Fatal("回调结束后 material 必须擦除")
		}
	}
}

func TestManagedLeaseRejectsOwnerMismatchBeforeInvocation(t *testing.T) {
	called := false
	broker, err := NewManagedLease("node-a", func(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
		called = true
		return nil, nil, errors.New("unexpected")
	})
	if err != nil {
		t.Fatal(err)
	}
	ref := kernelspi.CredentialRef{Handle: "credential://managed/stage-a", Scope: "tenant", Owner: "plugin.database", Purpose: "database.connection", Version: 1}
	err = broker.WithCredential(context.Background(), kernelspi.Scope{TenantID: "tenant-a", PluginID: "plugin.other", Namespace: "database.probe"}, ref, func(kernelspi.CredentialMaterial) error { return nil })
	if err == nil || called {
		t.Fatalf("owner 不匹配必须在寻址前拒绝: err=%v called=%v", err, called)
	}
}

func TestManagedLeaseRejectsEnvelopeForAnotherAudience(t *testing.T) {
	ref := kernelspi.CredentialRef{Handle: "credential://managed/stage-a", Scope: "tenant", Owner: "plugin.database", Purpose: "database.connection", Version: 1}
	now := time.Now().UTC()
	broker, _ := NewManagedLease("node-a", func(_ context.Context, _ *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		var request credentiallease.Request
		_ = json.Unmarshal(payload, &request)
		envelope, _ := credentiallease.Seal(request, credentiallease.Claims{TenantID: "tenant-a", Audience: "node-b", Ref: ref}, []byte("secret"), now, time.Second)
		response, _ := json.Marshal(envelope)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, response, nil
	})
	broker.now = func() time.Time { return now }
	err := broker.WithCredential(context.Background(), kernelspi.Scope{TenantID: "tenant-a", PluginID: "plugin.database", Namespace: "database.probe"}, ref, func(kernelspi.CredentialMaterial) error { return nil })
	if err == nil {
		t.Fatal("错误 audience 的 lease 必须拒绝")
	}
}

type recordingBroker struct{ calls int }

func (b *recordingBroker) WithCredential(_ context.Context, _ kernelspi.Scope, _ kernelspi.CredentialRef, use func(kernelspi.CredentialMaterial) error) error {
	b.calls++
	return use(material([]byte("value")))
}

func TestCompositeRoutesUnambiguousReferences(t *testing.T) {
	managed, named := &recordingBroker{}, &recordingBroker{}
	broker, _ := NewComposite(managed, named)
	scope := kernelspi.Scope{TenantID: "tenant-a", PluginID: "plugin", Namespace: "test"}
	use := func(kernelspi.CredentialMaterial) error { return nil }
	if err := broker.WithCredential(context.Background(), scope, kernelspi.CredentialRef{Handle: "credential://managed/a"}, use); err != nil {
		t.Fatal(err)
	}
	if err := broker.WithCredential(context.Background(), scope, kernelspi.CredentialRef{Name: "ssh.identity"}, use); err != nil {
		t.Fatal(err)
	}
	if err := broker.WithCredential(context.Background(), scope, kernelspi.CredentialRef{Handle: "credential://managed/a", Name: "ambiguous"}, use); err == nil {
		t.Fatal("含 handle 和 name 的歧义引用必须拒绝")
	}
	if managed.calls != 1 || named.calls != 1 {
		t.Fatalf("路由计数错误 managed=%d named=%d", managed.calls, named.calls)
	}
}
