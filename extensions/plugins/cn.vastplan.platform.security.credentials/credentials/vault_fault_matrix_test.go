package credentials

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
)

func TestVaultTransitFaultMatrixFailsClosedAndRecovers(t *testing.T) {
	vault := newVaultFaultServer(t)
	transit := vault.transit(t, 50*time.Millisecond)
	secret := []byte("fault-matrix-plaintext")
	ciphertext, err := transit.Encrypt(context.Background(), secret)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		mode string
	}{
		{name: "permission_denied", mode: "denied"},
		{name: "malformed_response", mode: "malformed"},
		{name: "request_timeout", mode: "delayed"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			vault.mode.Store(item.mode)
			if value, err := transit.Decrypt(context.Background(), ciphertext); err == nil || len(value) != 0 {
				t.Fatalf("Vault %s 时 decrypt 必须 fail-closed: value=%q err=%v", item.mode, value, err)
			} else {
				assertNoVaultSecret(t, err.Error(), secret, ciphertext, vault.token)
			}
		})
	}

	vault.mode.Store("ready")
	recovered, err := transit.Decrypt(context.Background(), ciphertext)
	if err != nil || string(recovered) != string(secret) {
		t.Fatalf("Vault 恢复后 decrypt 未恢复: value=%q err=%v", recovered, err)
	}
}

func TestMaterialLeaseVaultOutageDeniesThenRecovers(t *testing.T) {
	vault := newVaultFaultServer(t)
	transit := vault.transit(t, 50*time.Millisecond)
	service, err := New(transit)
	if err != nil {
		t.Fatal(err)
	}
	host := newCredentialStateHost(t)
	owner := managedContext("tenant-a", "plugin.database")
	secret := "database-fault-matrix-secret"
	result, raw, err := service.Handler(context.Background(), host, owner, []byte(`{"purpose":"database.connection","resource":"primary","value":"`+secret+`"}`), "stageManaged")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("暂存凭证失败: result=%+v raw=%s err=%v", result, raw, err)
	}
	var staged pluginconfig.StagedCredential
	if err := json.Unmarshal(raw, &staged); err != nil {
		t.Fatal(err)
	}
	activate, _ := json.Marshal(map[string]string{"stageId": staged.ID})
	if result, _, err = service.Handler(context.Background(), host, owner, activate, "activateManaged"); err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("激活凭证失败: result=%+v err=%v", result, err)
	}

	request, recipient, err := credentiallease.NewRequest(staged.Ref)
	if err != nil {
		t.Fatal(err)
	}
	defer recipient.Discard()
	kernel := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "runtime-a"}}
	leasePayload, _ := json.Marshal(request)
	vault.mode.Store("delayed")
	result, raw, err = service.MaterialLeaseHandler(context.Background(), host, kernel, leasePayload, "issue")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_ERROR || result.GetError().GetCode() != "platform.credentials.material_lease.denied" || len(raw) != 0 {
		t.Fatalf("Vault 故障时 material lease 必须拒绝且不得返回包: result=%+v raw=%s err=%v", result, raw, err)
	}
	assertNoVaultSecret(t, result.GetError().GetMessage(), []byte(secret), "", vault.token)

	vault.mode.Store("ready")
	result, raw, err = service.MaterialLeaseHandler(context.Background(), host, kernel, leasePayload, "issue")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("Vault 恢复后 material lease 未恢复: result=%+v raw=%s err=%v", result, raw, err)
	}
	var envelope credentiallease.Envelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(envelope.Ciphertext), secret) {
		t.Fatal("material lease envelope 泄漏了明文")
	}
	material, err := recipient.Open(envelope, credentiallease.Claims{TenantID: "tenant-a", Audience: "runtime-a", Ref: staged.Ref}, time.Now().UTC())
	if err != nil || string(material) != secret {
		t.Fatalf("恢复后的 material lease 无法由目标宿主解封: value=%q err=%v", material, err)
	}
	for index := range material {
		material[index] = 0
	}
}

type vaultFaultServer struct {
	server *httptest.Server
	mode   atomic.Value
	token  string
}

func newVaultFaultServer(t *testing.T) *vaultFaultServer {
	t.Helper()
	vault := &vaultFaultServer{token: "vault-test-token-must-not-leak"}
	vault.mode.Store("ready")
	vault.server = httptest.NewServer(http.HandlerFunc(vault.handle))
	t.Cleanup(vault.server.Close)
	return vault
}

func (v *vaultFaultServer) transit(t *testing.T, timeout time.Duration) VaultTransit {
	t.Helper()
	tokenFile := t.TempDir() + "/vault-token"
	if err := os.WriteFile(tokenFile, []byte(v.token), 0o600); err != nil {
		t.Fatal(err)
	}
	return VaultTransit{Address: v.server.URL, Key: "vastplan-a3", TokenFile: tokenFile, Client: &http.Client{Timeout: timeout}}
}

func (v *vaultFaultServer) handle(response http.ResponseWriter, request *http.Request) {
	switch v.mode.Load().(string) {
	case "delayed":
		time.Sleep(200 * time.Millisecond)
	case "denied":
		response.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(response).Encode(map[string]any{"errors": []string{"permission denied"}})
		return
	case "malformed":
		_, _ = response.Write([]byte("not-json"))
		return
	}
	if request.Header.Get("X-Vault-Token") != v.token {
		response.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(response).Encode(map[string]any{"errors": []string{"bad token"}})
		return
	}
	var body map[string]string
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		response.WriteHeader(http.StatusBadRequest)
		return
	}
	operation := strings.TrimPrefix(request.URL.Path, "/v1/transit/")
	operation = strings.SplitN(operation, "/", 2)[0]
	switch operation {
	case "encrypt":
		_ = json.NewEncoder(response).Encode(map[string]any{"data": map[string]string{"ciphertext": "vault:v1:" + body["plaintext"]}})
	case "rewrap":
		_ = json.NewEncoder(response).Encode(map[string]any{"data": map[string]string{"ciphertext": strings.Replace(body["ciphertext"], "vault:v1:", "vault:v2:", 1)}})
	case "decrypt":
		encoded := strings.TrimPrefix(strings.TrimPrefix(body["ciphertext"], "vault:v1:"), "vault:v2:")
		if _, err := base64.StdEncoding.DecodeString(encoded); err != nil {
			response.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(response).Encode(map[string]any{"errors": []string{"invalid ciphertext"}})
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"data": map[string]string{"plaintext": encoded}})
	default:
		response.WriteHeader(http.StatusNotFound)
	}
}

func assertNoVaultSecret(t *testing.T, text string, plaintext []byte, ciphertext, token string) {
	t.Helper()
	for _, forbidden := range []string{string(plaintext), ciphertext, token} {
		if forbidden != "" && strings.Contains(text, forbidden) {
			t.Fatalf("Vault 错误泄漏敏感值 %q: %s", forbidden, text)
		}
	}
}
