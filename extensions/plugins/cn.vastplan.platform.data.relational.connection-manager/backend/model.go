package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
)

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
	return definitionView{
		Name: value.Name, ResourceID: value.ResourceID, Revision: value.Revision, ProviderID: value.ProviderID,
		Endpoint: value.Endpoint, Database: value.Database, Options: append(json.RawMessage(nil), value.Options...), Pool: value.Pool,
		Runtime: runtime, Credential: credentialStatus{Managed: value.CredentialRef.Handle != "", Version: value.CredentialRef.Version},
	}
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
	service := &service{file: file, data: persisted{
		FormatVersion: 3,
		Tenants:       map[string]map[string]definition{}, Revisions: map[string]map[string]connectionIdentity{},
		Pending: map[string]map[string]pendingDefinition{}, Publications: map[string]map[string]runtimePublication{},
		Retire: map[string][]pluginconfig.ManagedCredentialRef{},
	}}
	raw, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return service, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &service.data); err != nil {
		return nil, err
	}
	if service.data.FormatVersion != 3 {
		return nil, fmt.Errorf("数据库连接状态格式版本 %d 不受支持；开发环境请删除旧状态后重建", service.data.FormatVersion)
	}
	if service.data.Tenants == nil {
		service.data.Tenants = map[string]map[string]definition{}
	}
	if service.data.Pending == nil {
		service.data.Pending = map[string]map[string]pendingDefinition{}
	}
	if service.data.Revisions == nil {
		service.data.Revisions = map[string]map[string]connectionIdentity{}
	}
	if service.data.Publications == nil {
		service.data.Publications = map[string]map[string]runtimePublication{}
	}
	if service.data.Retire == nil {
		service.data.Retire = map[string][]pluginconfig.ManagedCredentialRef{}
	}
	return service, nil
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

func (s *service) definitions(tenantID string) map[string]definition {
	if s.data.Tenants[tenantID] == nil {
		s.data.Tenants[tenantID] = map[string]definition{}
	}
	return s.data.Tenants[tenantID]
}

func (s *service) revisions(tenantID string) map[string]connectionIdentity {
	if s.data.Revisions[tenantID] == nil {
		s.data.Revisions[tenantID] = map[string]connectionIdentity{}
	}
	return s.data.Revisions[tenantID]
}

func (s *service) pending(tenantID string) map[string]pendingDefinition {
	if s.data.Pending[tenantID] == nil {
		s.data.Pending[tenantID] = map[string]pendingDefinition{}
	}
	return s.data.Pending[tenantID]
}

func (s *service) publications(tenantID string) map[string]runtimePublication {
	if s.data.Publications[tenantID] == nil {
		s.data.Publications[tenantID] = map[string]runtimePublication{}
	}
	return s.data.Publications[tenantID]
}

func tenant(call *contractv1.CallContext) (string, error) {
	if call == nil || call.TenantId == "" {
		return "", errors.New("数据库调用必须携带 tenant")
	}
	return call.TenantId, nil
}

func domainError(code string, err error) (*contractv1.CallResult, []byte, error) {
	return &contractv1.CallResult{
		Status: contractv1.CallResult_STATUS_ERROR,
		Error:  &contractv1.Error{Code: code, Message: err.Error()},
	}, nil, nil
}
