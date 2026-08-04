package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDevelopmentTransitRoundTripsAndRewrapsMaterial(t *testing.T) {
	transit, err := newDevelopmentTransit(bytes.Repeat([]byte{0x42}, 32), "vastplan-local")
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("database-password")
	ciphertext := transitRequest(t, transit, "encrypt", map[string]string{"plaintext": base64.StdEncoding.EncodeToString(secret)})["ciphertext"]
	if ciphertext == "" || bytes.Contains([]byte(ciphertext), secret) {
		t.Fatal("开发 Transit 未返回密文或泄露了明文")
	}
	opened := transitRequest(t, transit, "decrypt", map[string]string{"ciphertext": ciphertext})["plaintext"]
	decoded, err := base64.StdEncoding.DecodeString(opened)
	if err != nil || !bytes.Equal(decoded, secret) {
		t.Fatalf("开发 Transit 解密失败: %v", err)
	}
	rewrapped := transitRequest(t, transit, "rewrap", map[string]string{"ciphertext": ciphertext})["ciphertext"]
	if rewrapped == "" || rewrapped == ciphertext {
		t.Fatal("rewrap 必须生成新的认证密文")
	}
	opened = transitRequest(t, transit, "decrypt", map[string]string{"ciphertext": rewrapped})["plaintext"]
	decoded, err = base64.StdEncoding.DecodeString(opened)
	if err != nil || !bytes.Equal(decoded, secret) {
		t.Fatalf("rewrap 后无法解密: %v", err)
	}
}

func transitRequest(t *testing.T, transit http.Handler, operation string, payload map[string]string) map[string]string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/transit/"+operation+"/vastplan-local", bytes.NewReader(raw))
	request.Header.Set("X-Vault-Token", "vastplan-local-vault-token")
	response := httptest.NewRecorder()
	transit.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("Transit %s 失败: status=%d body=%s", operation, response.Code, response.Body.String())
	}
	var envelope struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}
