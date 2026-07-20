package credentiallease_test

import (
	"bytes"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
)

func TestLeaseRoundTripIsBoundAndOneUse(t *testing.T) {
	ref := pluginconfig.ManagedCredentialRef{Handle: "credential://managed/abc", Scope: "tenant", Owner: "plugin.database", Purpose: "database.connection", Version: 2}
	request, recipient, err := credentiallease.NewRequest(ref)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claims := credentiallease.Claims{TenantID: "tenant-a", Audience: "node-a", Ref: ref}
	secret := []byte("database-password")
	envelope, err := credentiallease.Seal(request, claims, secret, now, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains([]byte(envelope.Ciphertext), secret) {
		t.Fatal("lease ciphertext 不得包含明文")
	}
	material, err := recipient.Open(envelope, claims, now.Add(time.Second))
	if err != nil || !bytes.Equal(material, secret) {
		t.Fatalf("解封失败 material=%q err=%v", material, err)
	}
	for index := range material {
		material[index] = 0
	}
	if _, err := recipient.Open(envelope, claims, now.Add(time.Second)); err == nil {
		t.Fatal("同一 recipient 不得重复消费")
	}
}

func TestLeaseRejectsTamperingAudienceAndExpiry(t *testing.T) {
	ref := pluginconfig.ManagedCredentialRef{Handle: "credential://managed/abc", Scope: "tenant", Owner: "plugin.database", Purpose: "database.connection", Version: 1}
	now := time.Now().UTC()
	claims := credentiallease.Claims{TenantID: "tenant-a", Audience: "node-a", Ref: ref}

	request, recipient, _ := credentiallease.NewRequest(ref)
	envelope, _ := credentiallease.Seal(request, claims, []byte("secret"), now, time.Second)
	envelope.Audience = "node-b"
	if _, err := recipient.Open(envelope, credentiallease.Claims{TenantID: "tenant-a", Audience: "node-b", Ref: ref}, now); err == nil {
		t.Fatal("篡改 AAD 必须失败")
	}

	request, recipient, _ = credentiallease.NewRequest(ref)
	envelope, _ = credentiallease.Seal(request, claims, []byte("secret"), now, time.Second)
	if _, err := recipient.Open(envelope, claims, now.Add(2*time.Second)); err == nil {
		t.Fatal("过期 lease 必须失败")
	}
}

func TestLeaseRejectsMalformedManagedReference(t *testing.T) {
	_, _, err := credentiallease.NewRequest(pluginconfig.ManagedCredentialRef{Handle: "not-managed", Scope: "tenant", Owner: "plugin", Purpose: "database", Version: 1})
	if err == nil {
		t.Fatal("非 managed handle 必须拒绝")
	}
}
