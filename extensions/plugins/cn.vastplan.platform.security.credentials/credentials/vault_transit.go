// Package credentials 保存凭证密文和元数据；它不提供任何返回明文的协议操作。
package credentials

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/credentiallease"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.credentials/credentialsstate"
)

const (
	PluginID                = credentialsstate.PluginID
	PluginVersion           = "0.14.3"
	Capability              = "platform.credentials"
	MaterialLeaseCapability = credentiallease.Capability
	vaultAddressKey         = "platform.credentials.vault.address"
	vaultKeyKey             = "platform.credentials.vault.transitKey"
	vaultTokenFileKey       = "platform.credentials.vault.tokenFile"
)

type Transit interface {
	Encrypt(context.Context, []byte) (string, error)
	Rewrap(context.Context, string) (string, error)
	Decrypt(context.Context, string) ([]byte, error)
}

// VaultTransit 使用 Vault Transit HTTP API。Token 只从权限受控的本地文件读取，
// 不写入 unit config、状态文件、日志或协议返回值。
type VaultTransit struct {
	Address, Key, TokenFile string
	Client                  *http.Client
}

// VaultTransitError keeps HTTP/retry semantics without retaining a Vault
// response body, token, ciphertext or plaintext. InvalidMaterial is true only
// when Vault explicitly rejected a decrypt input as unusable.
type VaultTransitError struct {
	Operation       string
	StatusCode      int
	Retryable       bool
	InvalidMaterial bool
	Err             error
}

func (e *VaultTransitError) Error() string {
	if e == nil {
		return "Vault Transit 调用失败"
	}
	if e.StatusCode != 0 {
		return fmt.Sprintf("Vault Transit %s 失败: status=%d", e.Operation, e.StatusCode)
	}
	if e.Err != nil {
		return fmt.Sprintf("Vault Transit %s 失败: %v", e.Operation, e.Err)
	}
	return fmt.Sprintf("Vault Transit %s 失败", e.Operation)
}

func (e *VaultTransitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func vaultFailure(operation string, status int, retryable, invalidMaterial bool, err error) error {
	return &VaultTransitError{Operation: operation, StatusCode: status, Retryable: retryable, InvalidMaterial: invalidMaterial, Err: err}
}

type vaultTransitData struct {
	Ciphertext string `json:"ciphertext"`
	Plaintext  string `json:"plaintext"`
}

func (v VaultTransit) call(ctx context.Context, operation string, body any) (vaultTransitData, error) {
	if strings.TrimSpace(v.Address) == "" || strings.TrimSpace(v.Key) == "" || strings.TrimSpace(v.TokenFile) == "" {
		return vaultTransitData{}, vaultFailure(operation, 0, false, false, errors.New("配置不完整"))
	}
	token, err := os.ReadFile(v.TokenFile)
	if err != nil {
		return vaultTransitData{}, vaultFailure(operation, 0, false, false, fmt.Errorf("读取 token 文件: %w", err))
	}
	defer func() {
		for i := range token {
			token[i] = 0
		}
	}()
	raw, err := json.Marshal(body)
	if err != nil {
		return vaultTransitData{}, err
	}
	defer func() {
		for index := range raw {
			raw[index] = 0
		}
	}()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(v.Address, "/")+"/v1/transit/"+operation+"/"+v.Key, bytes.NewReader(raw))
	if err != nil {
		return vaultTransitData{}, err
	}
	request.Header.Set("X-Vault-Token", strings.TrimSpace(string(token)))
	request.Header.Set("Content-Type", "application/json")
	client := v.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return vaultTransitData{}, vaultFailure(operation, 0, true, false, err)
	}
	defer response.Body.Close()
	var decoded struct {
		Data   vaultTransitData `json:"data"`
		Errors []string         `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&decoded); err != nil {
		return vaultTransitData{}, vaultFailure(operation, response.StatusCode, response.StatusCode >= 500, false, errors.New("响应格式无效"))
	}
	if response.StatusCode/100 != 2 {
		retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		invalidMaterial := operation == "decrypt" && response.StatusCode == http.StatusBadRequest
		return vaultTransitData{}, vaultFailure(operation, response.StatusCode, retryable, invalidMaterial, nil)
	}
	return decoded.Data, nil
}
func (v VaultTransit) Encrypt(ctx context.Context, value []byte) (string, error) {
	data, err := v.call(ctx, "encrypt", map[string]string{"plaintext": base64.StdEncoding.EncodeToString(value)})
	if err != nil || data.Ciphertext == "" {
		return "", errors.Join(err, errors.New("Vault Transit encrypt 未返回 ciphertext"))
	}
	return data.Ciphertext, nil
}
func (v VaultTransit) Rewrap(ctx context.Context, ciphertext string) (string, error) {
	data, err := v.call(ctx, "rewrap", map[string]string{"ciphertext": ciphertext})
	if err != nil || data.Ciphertext == "" {
		return "", errors.Join(err, errors.New("Vault Transit rewrap 未返回 ciphertext"))
	}
	return data.Ciphertext, nil
}
func (v VaultTransit) Decrypt(ctx context.Context, ciphertext string) ([]byte, error) {
	data, err := v.call(ctx, "decrypt", map[string]string{"ciphertext": ciphertext})
	if err != nil || data.Plaintext == "" {
		return nil, errors.Join(err, errors.New("Vault Transit decrypt 未返回 plaintext"))
	}
	value, err := base64.StdEncoding.DecodeString(data.Plaintext)
	if err != nil || len(value) == 0 || len(value) > 4<<20 {
		for index := range value {
			value[index] = 0
		}
		return nil, errors.New("Vault Transit plaintext 编码或长度无效")
	}
	return value, nil
}
