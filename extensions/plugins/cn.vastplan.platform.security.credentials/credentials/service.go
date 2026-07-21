// Package credentials 保存凭证密文和元数据；它不提供任何返回明文的协议操作。
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
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	PluginID                = "cn.vastplan.platform.security.credentials"
	PluginVersion           = "0.5.0"
	Capability              = "platform.credentials"
	MaterialLeaseCapability = "platform.credentials.material-lease"
	stateFileKey            = "platform.credentials.stateFile"
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
	mu         sync.Mutex
	file       string
	transit    Transit
	data       persisted
	leaseSlots chan struct{}
}

func New(file string, transit Transit) (*Service, error) {
	if transit == nil {
		return nil, errors.New("凭证 Transit 适配器不能为空")
	}
	s := &Service{file: file, transit: transit, data: persisted{Tenants: map[string]map[string]Record{}, Managed: map[string]map[string]ManagedRecord{}}, leaseSlots: make(chan struct{}, 32)}
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

// IssueMaterialLease decrypts only for an authenticated kernel identity and
// immediately reseals the material to the requester's one-use X25519 key.
// Plaintext is never returned in a protocol payload.
func (s *Service) IssueMaterialLease(ctx context.Context, call *contractv1.CallContext, request credentiallease.Request) (credentiallease.Envelope, error) {
	if err := credentiallease.ValidateRequest(request); err != nil {
		return credentiallease.Envelope{}, err
	}
	t, err := tenant(call)
	if err != nil {
		return credentiallease.Envelope{}, err
	}
	audience := strings.TrimSpace(call.GetCaller().GetId())
	if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_SYSTEM || audience == "" {
		return credentiallease.Envelope{}, errors.New("material lease 只接受已认证可信宿主")
	}
	select {
	case s.leaseSlots <- struct{}{}:
		defer func() { <-s.leaseSlots }()
	case <-ctx.Done():
		return credentiallease.Envelope{}, ctx.Err()
	}
	s.mu.Lock()
	var matched ManagedRecord
	for _, record := range s.managedRecords(t) {
		if record.Ref.Handle == request.Ref.Handle {
			matched = record
			break
		}
	}
	if matched.StageID == "" || matched.State != managedActive || matched.Ref != request.Ref || matched.Ciphertext == "" {
		s.mu.Unlock()
		return credentiallease.Envelope{}, errors.New("托管凭证不存在、未激活或引用不匹配")
	}
	ciphertext := matched.Ciphertext
	s.mu.Unlock()
	material, err := s.transit.Decrypt(ctx, ciphertext)
	if err != nil {
		return credentiallease.Envelope{}, err
	}
	defer func() {
		for index := range material {
			material[index] = 0
		}
	}()
	// A revoke/retire racing the KMS request wins. Do not issue a lease from a
	// stale record merely because decryption started while it was Active.
	s.mu.Lock()
	current, ok := s.managedRecords(t)[matched.StageID]
	stillActive := ok && current.State == managedActive && current.Ref == matched.Ref && current.Ciphertext == ciphertext
	s.mu.Unlock()
	if !stillActive {
		return credentiallease.Envelope{}, errors.New("托管凭证在 lease 签发期间已变化")
	}
	return credentiallease.Seal(request, credentiallease.Claims{TenantID: t, Audience: audience, Ref: matched.Ref}, material, time.Now().UTC(), credentiallease.DefaultTTL)
}

func decodeMaterialLeaseRequest(payload []byte) (credentiallease.Request, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var request credentiallease.Request
	if err := decoder.Decode(&request); err != nil {
		return credentiallease.Request{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return credentiallease.Request{}, errors.New("material lease 请求只能包含一个 JSON 文档")
	}
	return request, nil
}

func (s *Service) MaterialLeaseHandler(ctx context.Context, _ sdk.Host, call *contractv1.CallContext, payload []byte, operation string) (*contractv1.CallResult, []byte, error) {
	if operation != "issue" {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "platform.credentials.material_lease.invalid", Message: "不支持的 material lease 操作"}}, nil, nil
	}
	request, err := decodeMaterialLeaseRequest(payload)
	if err != nil {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "platform.credentials.material_lease.invalid", Message: err.Error()}}, nil, nil
	}
	envelope, err := s.IssueMaterialLease(ctx, call, request)
	if err != nil {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "platform.credentials.material_lease.denied", Message: err.Error()}}, nil, nil
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
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

func MaterialLeaseDescriptor() []byte {
	return []byte(`{"title":"可信宿主凭证 Material Lease","subcommands":[
		{"name":"issue","description":"向可信宿主一次性公钥签发短期加密 material lease","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"ref":{"type":"object","additionalProperties":false,"properties":{"handle":{"type":"string"},"scope":{"const":"tenant"},"owner":{"type":"string"},"purpose":{"type":"string"},"version":{"type":"integer","minimum":1}},"required":["handle","scope","owner","purpose","version"]},"recipientPublicKey":{"type":"string"}},"required":["ref","recipientPublicKey"]}}
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

func MaterialLeaseContribution(s *Service) sdk.Contribution {
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: MaterialLeaseCapability, Descriptor: MaterialLeaseDescriptor(), Handlers: map[string]sdk.Handler{"issue": func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		return s.MaterialLeaseHandler(ctx, host, call, payload, "issue")
	}}}
}
