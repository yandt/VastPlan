// Command connectionmanager 启动数据库连接定义与受控连通性检查插件进程。
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const id, version, capability = "cn.vastplan.platform.data.relational.connection-manager", "0.7.0", "platform.database"

const credentialCapability = "platform.credentials"

var errConnectionNotFound = errors.New("数据库连接不存在")

type definition struct {
	Name          string                            `json:"name"`
	ResourceID    string                            `json:"resourceId"`
	Revision      uint64                            `json:"revision"`
	ProviderID    string                            `json:"providerId"`
	Endpoint      string                            `json:"endpoint"`
	Database      string                            `json:"database,omitempty"`
	Options       json.RawMessage                   `json:"options"`
	Pool          databasev1.PoolPolicy             `json:"pool"`
	CredentialRef pluginconfig.ManagedCredentialRef `json:"credentialRef"`
}
type credentialStatus struct {
	Managed bool  `json:"managed"`
	Version int64 `json:"version"`
}
type definitionView struct {
	Name       string                `json:"name"`
	ResourceID string                `json:"resourceId"`
	Revision   uint64                `json:"revision"`
	ProviderID string                `json:"providerId"`
	Endpoint   string                `json:"endpoint"`
	Database   string                `json:"database,omitempty"`
	Options    json.RawMessage       `json:"options"`
	Pool       databasev1.PoolPolicy `json:"pool"`
	Runtime    string                `json:"runtime"`
	Credential credentialStatus      `json:"credential"`
}

func view(value definition, runtime string) definitionView {
	return definitionView{Name: value.Name, ResourceID: value.ResourceID, Revision: value.Revision, ProviderID: value.ProviderID,
		Endpoint: value.Endpoint, Database: value.Database, Options: append(json.RawMessage(nil), value.Options...), Pool: value.Pool,
		Runtime: runtime, Credential: credentialStatus{Managed: value.CredentialRef.Handle != "", Version: value.CredentialRef.Version}}
}

type defineInput struct {
	Name            string                 `json:"name"`
	ProviderID      string                 `json:"providerId"`
	Endpoint        string                 `json:"endpoint"`
	Database        string                 `json:"database,omitempty"`
	Options         json.RawMessage        `json:"options"`
	Pool            *databasev1.PoolPolicy `json:"pool,omitempty"`
	CredentialValue string                 `json:"credentialValue,omitempty"`
}
type pendingDefinition struct {
	Desired  definition                    `json:"desired"`
	Previous *definition                   `json:"previous,omitempty"`
	Staged   pluginconfig.StagedCredential `json:"staged"`
}
type persisted struct {
	FormatVersion int                                            `json:"formatVersion"`
	Tenants       map[string]map[string]definition               `json:"tenants"`
	Revisions     map[string]map[string]connectionIdentity       `json:"revisions"`
	Pending       map[string]map[string]pendingDefinition        `json:"pending"`
	Publications  map[string]map[string]runtimePublication       `json:"publications"`
	Retire        map[string][]pluginconfig.ManagedCredentialRef `json:"retire,omitempty"`
}

type connectionIdentity struct {
	ResourceID   string `json:"resourceId"`
	LastRevision uint64 `json:"lastRevision"`
}

type runtimePublication struct {
	Action           string                             `json:"action"`
	Connection       definition                         `json:"connection"`
	RetireCredential *pluginconfig.ManagedCredentialRef `json:"retireCredential,omitempty"`
}
type service struct {
	opMu sync.Mutex
	mu   sync.RWMutex
	file string
	data persisted
}

func newService(file string) (*service, error) {
	if file == "" {
		return nil, errors.New("VASTPLAN_DATABASE_CONNECTIONS_STATE_FILE 不能为空")
	}
	s := &service{file: file, data: persisted{FormatVersion: 3, Tenants: map[string]map[string]definition{}, Revisions: map[string]map[string]connectionIdentity{}, Pending: map[string]map[string]pendingDefinition{}, Publications: map[string]map[string]runtimePublication{}, Retire: map[string][]pluginconfig.ManagedCredentialRef{}}}
	raw, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return nil, err
	}
	if s.data.FormatVersion != 3 {
		return nil, fmt.Errorf("数据库连接状态格式版本 %d 不受支持；开发环境请删除旧状态后重建", s.data.FormatVersion)
	}
	if s.data.Tenants == nil {
		s.data.Tenants = map[string]map[string]definition{}
	}
	if s.data.Pending == nil {
		s.data.Pending = map[string]map[string]pendingDefinition{}
	}
	if s.data.Revisions == nil {
		s.data.Revisions = map[string]map[string]connectionIdentity{}
	}
	if s.data.Publications == nil {
		s.data.Publications = map[string]map[string]runtimePublication{}
	}
	if s.data.Retire == nil {
		s.data.Retire = map[string][]pluginconfig.ManagedCredentialRef{}
	}
	return s, nil
}

func (s *service) save() error {
	raw, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.file), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.file), ".connections-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.file)
}

func (s *service) definitions(t string) map[string]definition {
	if s.data.Tenants[t] == nil {
		s.data.Tenants[t] = map[string]definition{}
	}
	return s.data.Tenants[t]
}
func (s *service) revisions(t string) map[string]connectionIdentity {
	if s.data.Revisions[t] == nil {
		s.data.Revisions[t] = map[string]connectionIdentity{}
	}
	return s.data.Revisions[t]
}
func (s *service) pending(t string) map[string]pendingDefinition {
	if s.data.Pending[t] == nil {
		s.data.Pending[t] = map[string]pendingDefinition{}
	}
	return s.data.Pending[t]
}
func (s *service) publications(t string) map[string]runtimePublication {
	if s.data.Publications[t] == nil {
		s.data.Publications[t] = map[string]runtimePublication{}
	}
	return s.data.Publications[t]
}
func tenant(c *contractv1.CallContext) (string, error) {
	if c == nil || c.TenantId == "" {
		return "", errors.New("数据库调用必须携带 tenant")
	}
	return c.TenantId, nil
}
func callCredential(ctx context.Context, host sdk.Host, call *contractv1.CallContext, operation string, input any, output any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	logicalService, routingDomain := "platform.credentials", "platform"
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: credentialCapability, Operation: &operation, LogicalService: &logicalService, RoutingDomain: &routingDomain}, call, payload)
	if err != nil {
		return err
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		if result != nil && result.Error != nil {
			return errors.New(result.Error.Message)
		}
		return errors.New("凭证插件拒绝托管凭证操作")
	}
	if output != nil {
		return json.Unmarshal(raw, output)
	}
	return nil
}

func defaultPoolPolicy() databasev1.PoolPolicy {
	return databasev1.PoolPolicy{MinIdle: 0, MaxIdle: 8, MaxOpen: 32, MaxLifetimeMS: 30 * 60_000,
		MaxIdleTimeMS: 5 * 60_000, AcquireTimeoutMS: 5_000, IdlePoolTTLMS: 15 * 60_000}
}

func connectionResourceID(tenantID, name string) string {
	digest := sha256.Sum256([]byte(tenantID + "\x00" + name))
	return fmt.Sprintf("connection-%x", digest[:12])
}

func connectionSpec(value definition) databasev1.ConnectionSpec {
	return databasev1.ConnectionSpec{
		Ref:        databasev1.ConnectionRef{ResourceID: value.ResourceID, Revision: value.Revision},
		ProviderID: value.ProviderID, Endpoint: value.Endpoint, Database: value.Database,
		Options: append(json.RawMessage(nil), value.Options...), Credentials: value.CredentialRef, Pool: value.Pool,
	}
}

func callRuntime(ctx context.Context, host sdk.Host, call *contractv1.CallContext, operation string, input any, output any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	logicalService, routingDomain := "foundation.data.relational.runtime", "platform"
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: databasev1.Capability, Operation: &operation,
		LogicalService: &logicalService, RoutingDomain: &routingDomain,
	}, call, payload)
	if err != nil {
		return err
	}
	if result == nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		if result.GetError().GetCode() != "" {
			return fmt.Errorf("Database Runtime %s: %s", result.GetError().GetCode(), result.GetError().GetMessage())
		}
		return errors.New("Database Runtime 暂不可用")
	}
	if output != nil {
		return json.Unmarshal(raw, output)
	}
	return nil
}

func (s *service) reconcilePublications(ctx context.Context, host sdk.Host, call *contractv1.CallContext, t string) error {
	s.mu.RLock()
	items := make(map[string]runtimePublication, len(s.data.Publications[t]))
	for name, item := range s.data.Publications[t] {
		items[name] = item
	}
	s.mu.RUnlock()
	for name, item := range items {
		var err error
		switch item.Action {
		case databasev1.OperationActivate:
			err = callRuntime(ctx, host, call, databasev1.OperationActivate,
				databasev1.ActivateRequest{Connection: connectionSpec(item.Connection)}, nil)
		case databasev1.OperationRetire:
			err = callRuntime(ctx, host, call, databasev1.OperationRetire,
				databasev1.RetireRequest{Connection: connectionSpec(item.Connection).Ref}, nil)
		default:
			err = errors.New("数据库 Runtime publication action 无效")
		}
		if err != nil {
			return fmt.Errorf("发布数据库连接 %q: %w", name, err)
		}
		s.mu.Lock()
		current, ok := s.publications(t)[name]
		if ok && current.Action == item.Action && current.Connection.Revision == item.Connection.Revision {
			delete(s.publications(t), name)
			retireLength := len(s.data.Retire[t])
			if item.RetireCredential != nil && item.RetireCredential.Handle != "" {
				s.data.Retire[t] = append(s.data.Retire[t], *item.RetireCredential)
			}
			if saveErr := s.save(); saveErr != nil {
				s.publications(t)[name] = current
				s.data.Retire[t] = s.data.Retire[t][:retireLength]
				s.mu.Unlock()
				return saveErr
			}
		}
		s.mu.Unlock()
	}
	return nil
}

func (s *service) reconcilePending(ctx context.Context, host sdk.Host, call *contractv1.CallContext, t string) error {
	s.mu.RLock()
	items := make(map[string]pendingDefinition, len(s.data.Pending[t]))
	for name, item := range s.data.Pending[t] {
		items[name] = item
	}
	s.mu.RUnlock()
	for name, item := range items {
		if err := callCredential(ctx, host, call, "activateManaged", map[string]string{"stageId": item.Staged.ID}, nil); err != nil {
			return fmt.Errorf("恢复数据库连接 %q 的凭证候选: %w", name, err)
		}
		s.mu.Lock()
		current, ok := s.pending(t)[name]
		if ok && current.Staged.ID == item.Staged.ID {
			old, oldExists := s.definitions(t)[name]
			oldPublication, publicationExists := s.publications(t)[name]
			s.definitions(t)[name] = item.Desired
			delete(s.pending(t), name)
			publication := runtimePublication{Action: databasev1.OperationActivate, Connection: item.Desired}
			if item.Previous != nil && item.Previous.CredentialRef.Handle != "" {
				ref := item.Previous.CredentialRef
				publication.RetireCredential = &ref
			}
			s.publications(t)[name] = publication
			if err := s.save(); err != nil {
				s.pending(t)[name] = current
				if publicationExists {
					s.publications(t)[name] = oldPublication
				} else {
					delete(s.publications(t), name)
				}
				if oldExists {
					s.definitions(t)[name] = old
				} else {
					delete(s.definitions(t), name)
				}
				s.mu.Unlock()
				return err
			}
		}
		s.mu.Unlock()
	}
	return nil
}

func (s *service) reconcileRetire(ctx context.Context, host sdk.Host, call *contractv1.CallContext, t string) error {
	s.mu.RLock()
	refs := append([]pluginconfig.ManagedCredentialRef(nil), s.data.Retire[t]...)
	s.mu.RUnlock()
	for _, ref := range refs {
		if err := callCredential(ctx, host, call, "retireManaged", map[string]string{"handle": ref.Handle}, nil); err != nil {
			return err
		}
		s.mu.Lock()
		queued := s.data.Retire[t]
		for index := range queued {
			if queued[index].Handle == ref.Handle {
				s.data.Retire[t] = append(queued[:index], queued[index+1:]...)
				break
			}
		}
		if err := s.save(); err != nil {
			s.mu.Unlock()
			return err
		}
		s.mu.Unlock()
	}
	return nil
}

func (s *service) define(ctx context.Context, host sdk.Host, call *contractv1.CallContext, t string, in defineInput) (definition, error) {
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 160 || strings.TrimSpace(in.ProviderID) == "" || len(in.ProviderID) > 80 || strings.TrimSpace(in.Endpoint) == "" || len(in.Endpoint) > 2048 || len(in.Database) > 256 || len(in.CredentialValue) > 4<<20 || len(in.Options) == 0 {
		return definition{}, errors.New("数据库连接字段为空或超过长度上限")
	}
	pool := defaultPoolPolicy()
	if in.Pool != nil {
		pool = *in.Pool
	}
	s.mu.RLock()
	old, exists := s.data.Tenants[t][in.Name]
	identity, identityExists := s.data.Revisions[t][in.Name]
	s.mu.RUnlock()
	revision, resourceID := uint64(1), connectionResourceID(t, in.Name)
	if identityExists {
		revision, resourceID = identity.LastRevision+1, identity.ResourceID
	} else if exists {
		revision, resourceID = old.Revision+1, old.ResourceID
	}
	makeDesired := func(ref pluginconfig.ManagedCredentialRef) (definition, error) {
		desired := definition{Name: in.Name, ResourceID: resourceID, Revision: revision, ProviderID: in.ProviderID,
			Endpoint: in.Endpoint, Database: in.Database, Options: append(json.RawMessage(nil), in.Options...), Pool: pool, CredentialRef: ref}
		if err := databasev1.ValidateConnectionSpec(connectionSpec(desired)); err != nil {
			return definition{}, err
		}
		return desired, nil
	}
	if in.CredentialValue == "" {
		if !exists || old.CredentialRef.Handle == "" {
			return definition{}, errors.New("新连接必须在当前页面输入凭证")
		}
		updated, err := makeDesired(old.CredentialRef)
		if err != nil {
			return definition{}, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.definitions(t)[in.Name] = updated
		previousIdentity, previousIdentityExists := s.revisions(t)[in.Name]
		s.revisions(t)[in.Name] = connectionIdentity{ResourceID: updated.ResourceID, LastRevision: updated.Revision}
		previousPublication, publicationExists := s.publications(t)[in.Name]
		s.publications(t)[in.Name] = runtimePublication{Action: databasev1.OperationActivate, Connection: updated}
		if err := s.save(); err != nil {
			s.definitions(t)[in.Name] = old
			if previousIdentityExists {
				s.revisions(t)[in.Name] = previousIdentity
			} else {
				delete(s.revisions(t), in.Name)
			}
			if publicationExists {
				s.publications(t)[in.Name] = previousPublication
			} else {
				delete(s.publications(t), in.Name)
			}
			return definition{}, err
		}
		return updated, nil
	}
	var staged pluginconfig.StagedCredential
	if err := callCredential(ctx, host, call, "stageManaged", map[string]string{"purpose": "database.connection", "resource": in.Name, "value": in.CredentialValue}, &staged); err != nil {
		return definition{}, err
	}
	if staged.ID == "" || staged.Ref.Handle == "" || staged.Ref.Owner != id || staged.Ref.Purpose != "database.connection" || staged.Ref.Scope != "tenant" || staged.Ref.Version < 1 {
		_ = callCredential(ctx, host, call, "abortManaged", map[string]string{"stageId": staged.ID}, nil)
		return definition{}, errors.New("凭证插件返回了不符合当前业务插件边界的引用")
	}
	desired, err := makeDesired(staged.Ref)
	if err != nil {
		_ = callCredential(ctx, host, call, "abortManaged", map[string]string{"stageId": staged.ID}, nil)
		return definition{}, err
	}
	pending := pendingDefinition{Desired: desired, Staged: staged}
	if exists {
		previous := old
		pending.Previous = &previous
	}
	s.mu.Lock()
	s.pending(t)[in.Name] = pending
	previousIdentity, previousIdentityExists := s.revisions(t)[in.Name]
	s.revisions(t)[in.Name] = connectionIdentity{ResourceID: desired.ResourceID, LastRevision: desired.Revision}
	if err := s.save(); err != nil {
		delete(s.pending(t), in.Name)
		if previousIdentityExists {
			s.revisions(t)[in.Name] = previousIdentity
		} else {
			delete(s.revisions(t), in.Name)
		}
		s.mu.Unlock()
		_ = callCredential(ctx, host, call, "abortManaged", map[string]string{"stageId": staged.ID}, nil)
		return definition{}, err
	}
	s.mu.Unlock()
	if err := callCredential(ctx, host, call, "activateManaged", map[string]string{"stageId": staged.ID}, nil); err != nil {
		return definition{}, err
	}
	s.mu.Lock()
	s.definitions(t)[in.Name] = desired
	delete(s.pending(t), in.Name)
	previousPublication, publicationExists := s.publications(t)[in.Name]
	publication := runtimePublication{Action: databasev1.OperationActivate, Connection: desired}
	if exists && old.CredentialRef.Handle != "" {
		ref := old.CredentialRef
		publication.RetireCredential = &ref
	}
	s.publications(t)[in.Name] = publication
	if err := s.save(); err != nil {
		s.pending(t)[in.Name] = pending
		if publicationExists {
			s.publications(t)[in.Name] = previousPublication
		} else {
			delete(s.publications(t), in.Name)
		}
		if exists {
			s.definitions(t)[in.Name] = old
		} else {
			delete(s.definitions(t), in.Name)
		}
		s.mu.Unlock()
		return definition{}, err
	}
	s.mu.Unlock()
	return desired, nil
}

func (s *service) handle(ctx context.Context, host sdk.Host, c *contractv1.CallContext, p []byte, op string) (*contractv1.CallResult, []byte, error) {
	t, err := tenant(c)
	if err != nil {
		return nil, nil, err
	}
	// Configuration writes are infrequent. Serializing the complete local saga
	// prevents two concurrent updates of the same connection from activating an
	// older credential after a newer candidate has already won.
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if err := s.reconcilePending(ctx, host, c, t); err != nil {
		return nil, nil, err
	}
	if op == "resolveRuntime" {
		if c.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || c.GetCaller().GetId() != databasev1.RuntimePluginID {
			return domainError("platform.database.forbidden", errors.New("只有 Database Runtime 可以解析内部连接定义"))
		}
		var in struct {
			Connection databasev1.ConnectionRef `json:"connection"`
		}
		if err := json.Unmarshal(p, &in); err != nil || databasev1.ValidateConnectionRef(in.Connection) != nil {
			return domainError("platform.database.invalid_request", errors.New("连接引用无效"))
		}
		s.mu.RLock()
		var found *definition
		for _, candidate := range s.data.Tenants[t] {
			if candidate.ResourceID == in.Connection.ResourceID && candidate.Revision == in.Connection.Revision {
				copy := candidate
				found = &copy
				break
			}
		}
		s.mu.RUnlock()
		if found == nil {
			return domainError("platform.database.not_found", errConnectionNotFound)
		}
		raw, err := json.Marshal(databasev1.ActivateRequest{Connection: connectionSpec(*found)})
		if err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
	// 发布与凭证清理由持久化 outbox 驱动。普通读取会尽力推进，Runtime
	// 暂不可用不会让管理面定义消失或阻塞列表读取。
	_ = s.reconcilePublications(ctx, host, c, t)
	_ = s.reconcileRetire(ctx, host, c, t)
	var out any
	if op == "define" {
		var in defineInput
		if err := json.Unmarshal(p, &in); err != nil {
			return nil, nil, err
		}
		var saved definition
		saved, err = s.define(ctx, host, c, t, in)
		_ = s.reconcilePublications(ctx, host, c, t)
		_ = s.reconcileRetire(ctx, host, c, t)
		s.mu.RLock()
		status := "ready"
		if publication, pending := s.data.Publications[t][saved.Name]; pending && publication.Connection.Revision == saved.Revision {
			status = "pending"
		}
		s.mu.RUnlock()
		out = view(saved, status)
	} else {
		var in struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(p, &in); err != nil {
			return nil, nil, err
		}
		s.mu.RLock()
		defs := s.data.Tenants[t]
		switch op {
		case "describe":
			var ok bool
			var value definition
			value, ok = defs[in.Name]
			if !ok {
				err = errConnectionNotFound
			} else {
				status := "ready"
				if publication, pending := s.data.Publications[t][value.Name]; pending && publication.Connection.Revision == value.Revision {
					status = "pending"
				}
				out = view(value, status)
			}
		case "list":
			items := make([]definitionView, 0, len(defs))
			for _, d := range defs {
				status := "ready"
				if publication, pending := s.data.Publications[t][d.Name]; pending && publication.Connection.Revision == d.Revision {
					status = "pending"
				}
				items = append(items, view(d, status))
			}
			sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
			out = items
		case "remove":
			d, ok := defs[in.Name]
			s.mu.RUnlock()
			if !ok {
				return domainError("platform.database.not_found", errConnectionNotFound)
			}
			s.mu.Lock()
			delete(s.definitions(t), in.Name)
			previousPublication, publicationExists := s.publications(t)[in.Name]
			ref := d.CredentialRef
			s.publications(t)[in.Name] = runtimePublication{Action: databasev1.OperationRetire, Connection: d, RetireCredential: &ref}
			err = s.save()
			if err != nil {
				s.definitions(t)[in.Name] = d
				if publicationExists {
					s.publications(t)[in.Name] = previousPublication
				} else {
					delete(s.publications(t), in.Name)
				}
			}
			s.mu.Unlock()
			if err == nil {
				_ = s.reconcilePublications(ctx, host, c, t)
				_ = s.reconcileRetire(ctx, host, c, t)
			}
			out = map[string]any{"name": in.Name, "removed": err == nil}
			goto marshal
		case "probe":
			d, ok := defs[in.Name]
			s.mu.RUnlock()
			if !ok {
				return domainError("platform.database.not_found", errConnectionNotFound)
			}
			var probe databasev1.ProbeResult
			if callErr := callRuntime(ctx, host, c, databasev1.OperationProbe,
				databasev1.ProbeRequest{Connection: connectionSpec(d)}, &probe); callErr != nil {
				return nil, nil, callErr
			}
			out = probe
			goto marshal
		default:
			s.mu.RUnlock()
			return nil, nil, errors.New("不支持的数据库操作")
		}
		s.mu.RUnlock()
	}
	if errors.Is(err, errConnectionNotFound) {
		return domainError("platform.database.not_found", err)
	}
	if err != nil {
		return nil, nil, err
	}
marshal:
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func domainError(code string, err error) (*contractv1.CallResult, []byte, error) {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error()}}, nil, nil
}
func main() {
	s, err := newService(os.Getenv("VASTPLAN_DATABASE_CONNECTIONS_STATE_FILE"))
	if err != nil {
		log.Fatal(err)
	}
	p := sdk.New(id, version, map[string]string{"backend": "^0.1"})
	handler := func(op string) sdk.Handler {
		return func(ctx context.Context, h sdk.Host, c *contractv1.CallContext, b []byte) (*contractv1.CallResult, []byte, error) {
			return s.handle(ctx, h, c, b, op)
		}
	}
	p.Contribute(sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: capability, Descriptor: descriptor(), Handlers: map[string]sdk.Handler{"define": handler("define"), "describe": handler("describe"), "list": handler("list"), "remove": handler("remove"), "probe": handler("probe"), "resolveRuntime": handler("resolveRuntime")}})
	if err := p.Serve(); err != nil {
		log.Fatal(err)
	}
}

func descriptor() []byte {
	return []byte(`{"title":"数据库连接","subcommands":[
		{"name":"define","description":"定义连接、Provider 参数和连接池策略，并发布到 Database Runtime","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"name":{"type":"string"},"providerId":{"type":"string"},"endpoint":{"type":"string"},"database":{"type":"string"},"options":{"type":"object"},"pool":{"type":"object"},"credentialValue":{"type":"string"}},"required":["name","providerId","endpoint","options"]}},
		{"name":"describe","description":"读取连接定义","paramsSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}},
		{"name":"list","description":"列出连接定义","paramsSchema":{"type":"object","properties":{}}},
		{"name":"remove","description":"删除连接定义","paramsSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}},
		{"name":"probe","description":"由 Database Runtime 使用托管凭证探测连接","paramsSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}}
	]}`)
}
