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

	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.credentials/credentialsstate"
)

const (
	PluginID                = credentialsstate.PluginID
	PluginVersion           = "0.13.2"
	Capability              = "platform.credentials"
	MaterialLeaseCapability = "platform.credentials.material-lease"
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

type vaultTransitData struct {
	Ciphertext string `json:"ciphertext"`
	Plaintext  string `json:"plaintext"`
}

func (v VaultTransit) call(ctx context.Context, operation string, body any) (vaultTransitData, error) {
	if strings.TrimSpace(v.Address) == "" || strings.TrimSpace(v.Key) == "" || strings.TrimSpace(v.TokenFile) == "" {
		return vaultTransitData{}, errors.New("Vault Transit 配置不完整")
	}
	token, err := os.ReadFile(v.TokenFile)
	if err != nil {
		return vaultTransitData{}, fmt.Errorf("读取 Vault token 文件: %w", err)
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
		return vaultTransitData{}, fmt.Errorf("调用 Vault Transit: %w", err)
	}
	defer response.Body.Close()
	var decoded struct {
		Data   vaultTransitData `json:"data"`
		Errors []string         `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&decoded); err != nil {
		return vaultTransitData{}, err
	}
	if response.StatusCode/100 != 2 {
		return vaultTransitData{}, fmt.Errorf("Vault Transit %s 失败: %s", operation, strings.Join(decoded.Errors, "; "))
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
