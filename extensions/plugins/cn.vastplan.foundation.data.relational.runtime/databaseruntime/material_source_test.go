package databaseruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
)

func testRuntimeAudience(symbol string) string { return "runtime:v1:" + strings.Repeat(symbol, 43) }

type leaseHost struct {
	tenant, audience string
	secret           []byte
	now              time.Time
	calls            int
}

func (h *leaseHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	h.calls++
	if target.GetCapability() != credentiallease.RuntimeKernelService || target.GetOperation() != "issue" {
		return nil, nil, context.Canceled
	}
	var request credentiallease.Request
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, nil, err
	}
	envelope, err := credentiallease.Seal(request, credentiallease.Claims{
		TenantID: h.tenant, Audience: h.audience, Ref: request.Ref,
	}, h.secret, h.now, 10*time.Second)
	if err != nil {
		return nil, nil, err
	}
	raw, _ := json.Marshal(envelope)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func TestRuntimeMaterialSourceOpensOnlyInsideDatabaseRuntimeAndWipes(t *testing.T) {
	ref := testConnectionSpec("postgresql").Credentials
	audience := testRuntimeAudience("A")
	now := time.Now().UTC()
	host := &leaseHost{tenant: "tenant-a", audience: audience, secret: []byte("db-password"), now: now}
	source, err := NewRuntimeMaterialSource(host, "tenant-a", ref, audience)
	if err != nil {
		t.Fatal(err)
	}
	source.now = func() time.Time { return now.Add(time.Second) }
	var observed []byte
	if err := source.WithMaterial(context.Background(), func(material CredentialMaterial) error {
		observed = material.Bytes()
		if !bytes.Equal(observed, host.secret) {
			t.Fatalf("material 不匹配: %q", observed)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if host.calls != 1 {
		t.Fatalf("每次使用必须取得一次性 lease: %d", host.calls)
	}
	for _, value := range observed {
		if value != 0 {
			t.Fatal("回调结束后 material 必须清零")
		}
	}
}

func TestRuntimeMaterialSourceRejectsWrongAudienceAndTenant(t *testing.T) {
	ref := testConnectionSpec("postgresql").Credentials
	now := time.Now().UTC()
	for name, host := range map[string]*leaseHost{
		"audience": {tenant: "tenant-a", audience: testRuntimeAudience("Q"), secret: []byte("secret"), now: now},
		"tenant":   {tenant: "tenant-b", audience: testRuntimeAudience("A"), secret: []byte("secret"), now: now},
	} {
		t.Run(name, func(t *testing.T) {
			source, err := NewRuntimeMaterialSource(host, "tenant-a", ref, testRuntimeAudience("A"))
			if err != nil {
				t.Fatal(err)
			}
			source.now = func() time.Time { return now }
			if err := source.WithMaterial(context.Background(), func(CredentialMaterial) error { return nil }); err == nil {
				t.Fatal("错误 audience/tenant 的 lease 必须拒绝")
			}
		})
	}
}

func TestRuntimeMaterialSourceRejectsExpiredLease(t *testing.T) {
	ref := testConnectionSpec("postgresql").Credentials
	audience := testRuntimeAudience("A")
	issued := time.Now().UTC().Add(-time.Minute)
	host := &leaseHost{tenant: "tenant-a", audience: audience, secret: []byte("secret"), now: issued}
	source, err := NewRuntimeMaterialSource(host, "tenant-a", ref, audience)
	if err != nil {
		t.Fatal(err)
	}
	source.now = time.Now
	if err := source.WithMaterial(context.Background(), func(CredentialMaterial) error { return nil }); err == nil {
		t.Fatal("过期 material lease 必须拒绝")
	}
}
