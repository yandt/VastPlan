// Package credentials 保存凭证密文和元数据；它不提供任何读取明文的操作。
package credentials

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	PluginID          = "com.vastplan.platform.security.credentials"
	PluginVersion     = "0.2.0"
	Capability        = "platform.credentials"
	stateFileKey      = "platform.credentials.stateFile"
	vaultAddressKey   = "platform.credentials.vault.address"
	vaultKeyKey       = "platform.credentials.vault.transitKey"
	vaultTokenFileKey = "platform.credentials.vault.tokenFile"
)

type Transit interface {
	Encrypt(context.Context, []byte) (string, error)
	Rewrap(context.Context, string) (string, error)
}

// VaultTransit 使用 Vault Transit HTTP API。Token 只从权限受控的本地文件读取，
// 不写入 unit config、状态文件、日志或协议返回值。
type VaultTransit struct {
	Address, Key, TokenFile string
	Client                  *http.Client
}

func (v VaultTransit) call(ctx context.Context, operation string, body any) (string, error) {
	if strings.TrimSpace(v.Address) == "" || strings.TrimSpace(v.Key) == "" || strings.TrimSpace(v.TokenFile) == "" {
		return "", errors.New("Vault Transit 配置不完整")
	}
	token, err := os.ReadFile(v.TokenFile)
	if err != nil {
		return "", fmt.Errorf("读取 Vault token 文件: %w", err)
	}
	defer func() {
		for i := range token {
			token[i] = 0
		}
	}()
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(v.Address, "/")+"/v1/transit/"+operation+"/"+v.Key, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	request.Header.Set("X-Vault-Token", strings.TrimSpace(string(token)))
	request.Header.Set("Content-Type", "application/json")
	client := v.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("调用 Vault Transit: %w", err)
	}
	defer response.Body.Close()
	var decoded struct {
		Data struct {
			Ciphertext string `json:"ciphertext"`
		} `json:"data"`
		Errors []string `json:"errors"`
	}
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return "", err
	}
	if response.StatusCode/100 != 2 || decoded.Data.Ciphertext == "" {
		return "", fmt.Errorf("Vault Transit %s 失败: %s", operation, strings.Join(decoded.Errors, "; "))
	}
	return decoded.Data.Ciphertext, nil
}
func (v VaultTransit) Encrypt(ctx context.Context, value []byte) (string, error) {
	return v.call(ctx, "encrypt", map[string]string{"plaintext": base64.StdEncoding.EncodeToString(value)})
}
func (v VaultTransit) Rewrap(ctx context.Context, ciphertext string) (string, error) {
	return v.call(ctx, "rewrap", map[string]string{"ciphertext": ciphertext})
}

type Record struct {
	Name       string    `json:"name"`
	Version    int64     `json:"version"`
	KeyVersion string    `json:"keyVersion"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Revoked    bool      `json:"revoked"`
	Ciphertext string    `json:"ciphertext"`
}

// Metadata 是唯一允许经插件协议返回的凭证视图；密文和明文均不可返回。
type Metadata struct {
	Name       string    `json:"name"`
	Version    int64     `json:"version"`
	KeyVersion string    `json:"keyVersion"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Revoked    bool      `json:"revoked"`
}

func metadata(record Record) Metadata {
	return Metadata{Name: record.Name, Version: record.Version, KeyVersion: record.KeyVersion, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, Revoked: record.Revoked}
}

type persisted struct {
	Tenants map[string]map[string]Record `json:"tenants"`
}
type Service struct {
	mu      sync.Mutex
	file    string
	transit Transit
	data    persisted
}

func New(file string, transit Transit) (*Service, error) {
	s := &Service{file: file, transit: transit, data: persisted{Tenants: map[string]map[string]Record{}}}
	if file != "" {
		if err := s.load(); err != nil {
			return nil, err
		}
	}
	return s, nil
}
func (s *Service) load() error {
	raw, err := os.ReadFile(s.file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return err
	}
	if s.data.Tenants == nil {
		s.data.Tenants = map[string]map[string]Record{}
	}
	return nil
}
func (s *Service) save() error {
	if s.file == "" {
		return errors.New("凭证状态文件未配置")
	}
	raw, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.file), 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(s.file), ".credentials-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err = temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.file)
}
func tenant(ctx *contractv1.CallContext) (string, error) {
	if ctx == nil || strings.TrimSpace(ctx.TenantId) == "" {
		return "", errors.New("凭证调用必须携带 tenant")
	}
	return ctx.TenantId, nil
}
func validName(name string) error {
	if strings.TrimSpace(name) == "" || len(name) > 160 {
		return errors.New("凭证 name 必须为 1-160 个非空字符")
	}
	return nil
}
func (s *Service) records(tenant string) map[string]Record {
	if s.data.Tenants[tenant] == nil {
		s.data.Tenants[tenant] = map[string]Record{}
	}
	return s.data.Tenants[tenant]
}
func (s *Service) Put(ctx context.Context, call *contractv1.CallContext, name, value string) (Record, error) {
	if err := validName(name); err != nil {
		return Record{}, err
	}
	t, err := tenant(call)
	if err != nil {
		return Record{}, err
	}
	if value == "" {
		return Record{}, errors.New("凭证 value 不能为空")
	}
	cipher, err := s.transit.Encrypt(ctx, []byte(value))
	if err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records := s.records(t)
	now := time.Now().UTC()
	old, ok := records[name]
	version := old.Version + 1
	if !ok {
		version = 1
	}
	r := Record{Name: name, Version: version, KeyVersion: transitVersion(cipher), CreatedAt: now, UpdatedAt: now, Ciphertext: cipher}
	if ok {
		r.CreatedAt = old.CreatedAt
	}
	records[name] = r
	if err := s.save(); err != nil {
		return Record{}, err
	}
	return r, nil
}
func (s *Service) Describe(call *contractv1.CallContext, name string) (Record, error) {
	if err := validName(name); err != nil {
		return Record{}, err
	}
	t, err := tenant(call)
	if err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records(t)[name]
	if !ok {
		return Record{}, os.ErrNotExist
	}
	return r, nil
}
func (s *Service) List(call *contractv1.CallContext, prefix string) ([]Record, error) {
	t, err := tenant(call)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Record{}
	for _, r := range s.records(t) {
		if strings.HasPrefix(r.Name, prefix) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (s *Service) Rotate(ctx context.Context, call *contractv1.CallContext, name string) (Record, error) {
	r, err := s.Describe(call, name)
	if err != nil {
		return Record{}, err
	}
	if r.Revoked {
		return Record{}, errors.New("已撤销的凭证不能轮换")
	}
	cipher, err := s.transit.Rewrap(ctx, r.Ciphertext)
	if err != nil {
		return Record{}, err
	}
	t, _ := tenant(call)
	s.mu.Lock()
	defer s.mu.Unlock()
	r = s.records(t)[name]
	r.Version++
	r.KeyVersion = transitVersion(cipher)
	r.Ciphertext = cipher
	r.UpdatedAt = time.Now().UTC()
	s.records(t)[name] = r
	if err := s.save(); err != nil {
		return Record{}, err
	}
	return r, nil
}
func (s *Service) Revoke(call *contractv1.CallContext, name string) (Record, error) {
	r, err := s.Describe(call, name)
	if err != nil {
		return Record{}, err
	}
	t, _ := tenant(call)
	s.mu.Lock()
	defer s.mu.Unlock()
	r = s.records(t)[name]
	r.Revoked = true
	r.Version++
	r.UpdatedAt = time.Now().UTC()
	s.records(t)[name] = r
	if err := s.save(); err != nil {
		return Record{}, err
	}
	return r, nil
}
func transitVersion(cipher string) string {
	parts := strings.Split(cipher, ":")
	if len(parts) >= 2 && parts[0] == "vault" {
		return parts[1]
	}
	return "unknown"
}

func (s *Service) Handler(ctx context.Context, _ sdk.Host, call *contractv1.CallContext, payload []byte, op string) (*contractv1.CallResult, []byte, error) {
	var in struct {
		Name   string `json:"name"`
		Value  string `json:"value"`
		Prefix string `json:"prefix"`
	}
	if err := json.Unmarshal(payload, &in); err != nil {
		return nil, nil, err
	}
	var out any
	var err error
	switch op {
	case "put":
		var record Record
		record, err = s.Put(ctx, call, in.Name, in.Value)
		out = metadata(record)
	case "describe":
		var record Record
		record, err = s.Describe(call, in.Name)
		out = metadata(record)
	case "list":
		var records []Record
		records, err = s.List(call, in.Prefix)
		out = make([]Metadata, 0, len(records))
		for _, record := range records {
			out = append(out.([]Metadata), metadata(record))
		}
	case "rotate":
		var record Record
		record, err = s.Rotate(ctx, call, in.Name)
		out = metadata(record)
	case "revoke":
		var record Record
		record, err = s.Revoke(call, in.Name)
		out = metadata(record)
	default:
		err = fmt.Errorf("不支持的凭证操作 %q", op)
	}
	if err != nil {
		code := "platform.credentials.invalid"
		if errors.Is(err, os.ErrNotExist) {
			code = "platform.credentials.not_found"
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error()}}, nil, nil
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}
func Descriptor() []byte {
	raw, _ := json.Marshal(map[string]any{"title": "凭证管理", "subcommands": []map[string]string{{"name": "put", "description": "保存凭证"}, {"name": "describe", "description": "读取元数据"}, {"name": "list", "description": "列出元数据"}, {"name": "rotate", "description": "轮换包裹密钥"}, {"name": "revoke", "description": "撤销凭证"}}})
	return raw
}
func Contribution(s *Service) sdk.Contribution {
	h := func(op string) sdk.Handler {
		return func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return s.Handler(ctx, host, call, payload, op)
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: map[string]sdk.Handler{"put": h("put"), "describe": h("describe"), "list": h("list"), "rotate": h("rotate"), "revoke": h("revoke")}}
}
