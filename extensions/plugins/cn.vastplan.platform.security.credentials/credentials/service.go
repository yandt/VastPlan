// Package credentials 保存凭证密文和元数据；它不提供任何读取明文的操作。
package credentials

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
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
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	PluginID          = "cn.vastplan.platform.security.credentials"
	PluginVersion     = "0.3.0"
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
	Tenants map[string]map[string]Record        `json:"tenants"`
	Managed map[string]map[string]ManagedRecord `json:"managed"`
}

const (
	managedPreparing = "Preparing"
	managedActive    = "Active"
	managedAborted   = "Aborted"
	managedRetired   = "Retired"
)

// ManagedRecord is the custodian-owned representation. Ciphertext is persisted
// only here; callers receive Ref and StageID, never ciphertext or plaintext.
type ManagedRecord struct {
	StageID    string                            `json:"stageId"`
	Ref        pluginconfig.ManagedCredentialRef `json:"ref"`
	Resource   string                            `json:"resource"`
	State      string                            `json:"state"`
	CreatedAt  time.Time                         `json:"createdAt"`
	UpdatedAt  time.Time                         `json:"updatedAt"`
	Ciphertext string                            `json:"ciphertext,omitempty"`
}
type Service struct {
	mu      sync.Mutex
	file    string
	transit Transit
	data    persisted
}

func New(file string, transit Transit) (*Service, error) {
	if transit == nil {
		return nil, errors.New("凭证 Transit 适配器不能为空")
	}
	s := &Service{file: file, transit: transit, data: persisted{Tenants: map[string]map[string]Record{}, Managed: map[string]map[string]ManagedRecord{}}}
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
	if s.data.Managed == nil {
		s.data.Managed = map[string]map[string]ManagedRecord{}
	}
	return validateManagedState(s.data.Managed)
}

func validateManagedState(tenants map[string]map[string]ManagedRecord) error {
	for tenantID, records := range tenants {
		if strings.TrimSpace(tenantID) == "" {
			return errors.New("托管凭证状态包含空 tenant")
		}
		for stageID, record := range records {
			if stageID != record.StageID || !strings.HasPrefix(stageID, "stage-") || !strings.HasPrefix(record.Ref.Handle, "credential://managed/") || record.Ref.Scope != "tenant" || record.Ref.Owner == "" || record.Ref.Purpose == "" || record.Resource == "" || record.Ref.Version < 1 {
				return fmt.Errorf("托管凭证状态 %q 元数据无效", stageID)
			}
			switch record.State {
			case managedPreparing, managedActive:
				if record.Ciphertext == "" {
					return fmt.Errorf("托管凭证状态 %q 缺少密文", stageID)
				}
			case managedAborted, managedRetired:
				if record.Ciphertext != "" {
					return fmt.Errorf("已终止托管凭证 %q 不得保留密文", stageID)
				}
			default:
				return fmt.Errorf("托管凭证状态 %q 的 state 无效", stageID)
			}
		}
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

func (s *Service) managedRecords(tenant string) map[string]ManagedRecord {
	if s.data.Managed[tenant] == nil {
		s.data.Managed[tenant] = map[string]ManagedRecord{}
	}
	return s.data.Managed[tenant]
}

func managedOwner(call *contractv1.CallContext) (string, error) {
	if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || strings.TrimSpace(call.GetCaller().GetId()) == "" {
		return "", errors.New("托管凭证只接受已认证业务插件")
	}
	return call.GetCaller().GetId(), nil
}

func opaqueID(prefix string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(raw), nil
}

// StageManaged creates a non-runnable credential candidate owned by the
// authenticated calling plugin. owner is never accepted from the payload.
func (s *Service) StageManaged(ctx context.Context, call *contractv1.CallContext, purpose, resource string, value []byte) (pluginconfig.StagedCredential, error) {
	t, err := tenant(call)
	if err != nil {
		return pluginconfig.StagedCredential{}, err
	}
	owner, err := managedOwner(call)
	if err != nil {
		return pluginconfig.StagedCredential{}, err
	}
	if strings.TrimSpace(purpose) == "" || strings.TrimSpace(resource) == "" || len(purpose) > 160 || len(resource) > 320 || len(value) == 0 || len(value) > 4<<20 {
		return pluginconfig.StagedCredential{}, errors.New("托管凭证 purpose、resource 和 value 均不能为空")
	}
	ciphertext, err := s.transit.Encrypt(ctx, value)
	if err != nil {
		return pluginconfig.StagedCredential{}, err
	}
	stageID, err := opaqueID("stage-")
	if err != nil {
		return pluginconfig.StagedCredential{}, err
	}
	handle, err := opaqueID("credential://managed/")
	if err != nil {
		return pluginconfig.StagedCredential{}, err
	}
	now := time.Now().UTC()
	ref := pluginconfig.ManagedCredentialRef{Handle: handle, Scope: "tenant", Owner: owner, Purpose: purpose, Version: 1}
	record := ManagedRecord{StageID: stageID, Ref: ref, Resource: resource, State: managedPreparing, CreatedAt: now, UpdatedAt: now, Ciphertext: ciphertext}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.managedRecords(t)[stageID] = record
	if err := s.save(); err != nil {
		delete(s.managedRecords(t), stageID)
		return pluginconfig.StagedCredential{}, err
	}
	return pluginconfig.StagedCredential{ID: stageID, Ref: ref}, nil
}

func (s *Service) managedTransition(call *contractv1.CallContext, stageID, target string) (pluginconfig.ManagedCredentialRef, error) {
	t, err := tenant(call)
	if err != nil {
		return pluginconfig.ManagedCredentialRef{}, err
	}
	owner, err := managedOwner(call)
	if err != nil {
		return pluginconfig.ManagedCredentialRef{}, err
	}
	if !strings.HasPrefix(stageID, "stage-") {
		return pluginconfig.ManagedCredentialRef{}, errors.New("托管凭证 stageId 无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.managedRecords(t)[stageID]
	if !ok {
		return pluginconfig.ManagedCredentialRef{}, os.ErrNotExist
	}
	if record.Ref.Owner != owner {
		return pluginconfig.ManagedCredentialRef{}, errors.New("托管凭证不属于当前插件")
	}
	switch target {
	case managedActive:
		if record.State != managedPreparing && record.State != managedActive {
			return pluginconfig.ManagedCredentialRef{}, errors.New("只有 Preparing 凭证可以激活")
		}
	case managedAborted:
		if record.State == managedActive || record.State == managedRetired {
			return pluginconfig.ManagedCredentialRef{}, errors.New("已激活凭证不能终止候选")
		}
		record.Ciphertext = ""
	default:
		return pluginconfig.ManagedCredentialRef{}, errors.New("未知托管凭证状态")
	}
	record.State = target
	record.UpdatedAt = time.Now().UTC()
	s.managedRecords(t)[stageID] = record
	if err := s.save(); err != nil {
		return pluginconfig.ManagedCredentialRef{}, err
	}
	return record.Ref, nil
}

func (s *Service) ActivateManaged(call *contractv1.CallContext, stageID string) (pluginconfig.ManagedCredentialRef, error) {
	return s.managedTransition(call, stageID, managedActive)
}

func (s *Service) AbortManaged(call *contractv1.CallContext, stageID string) (pluginconfig.ManagedCredentialRef, error) {
	return s.managedTransition(call, stageID, managedAborted)
}

func (s *Service) RetireManaged(call *contractv1.CallContext, handle string) (pluginconfig.ManagedCredentialRef, error) {
	t, err := tenant(call)
	if err != nil {
		return pluginconfig.ManagedCredentialRef{}, err
	}
	owner, err := managedOwner(call)
	if err != nil {
		return pluginconfig.ManagedCredentialRef{}, err
	}
	if !strings.HasPrefix(handle, "credential://managed/") || len(handle) > 256 {
		return pluginconfig.ManagedCredentialRef{}, errors.New("托管凭证 handle 无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, record := range s.managedRecords(t) {
		if record.Ref.Handle != handle {
			continue
		}
		if record.Ref.Owner != owner {
			return pluginconfig.ManagedCredentialRef{}, errors.New("托管凭证不可由当前插件退役")
		}
		if record.State == managedRetired {
			return record.Ref, nil
		}
		if record.State != managedActive {
			return pluginconfig.ManagedCredentialRef{}, errors.New("只有 Active 托管凭证可以退役")
		}
		record.State, record.Ciphertext, record.UpdatedAt = managedRetired, "", time.Now().UTC()
		s.managedRecords(t)[id] = record
		if err := s.save(); err != nil {
			return pluginconfig.ManagedCredentialRef{}, err
		}
		return record.Ref, nil
	}
	return pluginconfig.ManagedCredentialRef{}, os.ErrNotExist
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
		Name     string `json:"name"`
		Value    string `json:"value"`
		Prefix   string `json:"prefix"`
		StageID  string `json:"stageId"`
		Handle   string `json:"handle"`
		Purpose  string `json:"purpose"`
		Resource string `json:"resource"`
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
	case "stageManaged":
		secret := []byte(in.Value)
		defer func() {
			for index := range secret {
				secret[index] = 0
			}
		}()
		out, err = s.StageManaged(ctx, call, in.Purpose, in.Resource, secret)
	case "activateManaged":
		out, err = s.ActivateManaged(call, in.StageID)
	case "abortManaged":
		out, err = s.AbortManaged(call, in.StageID)
	case "retireManaged":
		out, err = s.RetireManaged(call, in.Handle)
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
	return []byte(`{"title":"凭证管理","subcommands":[
		{"name":"put","description":"以 Vault Transit 加密后保存凭证","paramsSchema":{"type":"object","properties":{"name":{"type":"string"},"value":{"type":"string"}},"required":["name","value"]}},
		{"name":"describe","description":"读取凭证元数据，不返回明文或密文","paramsSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}},
		{"name":"list","description":"列出当前租户的凭证元数据","paramsSchema":{"type":"object","properties":{"prefix":{"type":"string"}}}},
		{"name":"rotate","description":"通过 Vault Transit rewrap 轮换凭证包裹密钥","paramsSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}},
		{"name":"revoke","description":"撤销凭证引用","paramsSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}},
		{"name":"stageManaged","description":"由业务插件创建不可读取的凭证候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"purpose":{"type":"string"},"resource":{"type":"string"},"value":{"type":"string"}},"required":["purpose","resource","value"]}},
		{"name":"activateManaged","description":"由创建者激活托管凭证候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"stageId":{"type":"string"}},"required":["stageId"]}},
		{"name":"abortManaged","description":"由创建者终止托管凭证候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"stageId":{"type":"string"}},"required":["stageId"]}},
		{"name":"retireManaged","description":"由创建者退役不再使用的托管凭证","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"handle":{"type":"string"}},"required":["handle"]}}
	]}`)
}
func Contribution(s *Service) sdk.Contribution {
	h := func(op string) sdk.Handler {
		return func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return s.Handler(ctx, host, call, payload, op)
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: map[string]sdk.Handler{"put": h("put"), "describe": h("describe"), "list": h("list"), "rotate": h("rotate"), "revoke": h("revoke"), "stageManaged": h("stageManaged"), "activateManaged": h("activateManaged"), "abortManaged": h("abortManaged"), "retireManaged": h("retireManaged")}}
}
