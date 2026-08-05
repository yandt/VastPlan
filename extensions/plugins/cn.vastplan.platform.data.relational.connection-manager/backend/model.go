package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfig"
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
	TestCleanup   map[string][]testCredentialCleanup             `json:"testCleanup,omitempty"`
}

type testCredentialCleanup struct {
	StageID string                            `json:"stageId"`
	Ref     pluginconfig.ManagedCredentialRef `json:"ref"`
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
	opMu  sync.Mutex
	mu    sync.RWMutex
	data  persisted
	state stateProtocol
}

func newSharedStateService() *service {
	return &service{data: emptyPersisted(), state: &sharedStateProtocol{}}
}

func emptyPersisted() persisted {
	return persisted{
		FormatVersion: 3,
		Tenants:       map[string]map[string]definition{}, Revisions: map[string]map[string]connectionIdentity{},
		Pending: map[string]map[string]pendingDefinition{}, Publications: map[string]map[string]runtimePublication{},
		Retire: map[string][]pluginconfig.ManagedCredentialRef{}, TestCleanup: map[string][]testCredentialCleanup{},
	}
}

func decodePersisted(raw []byte, target *persisted) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("数据库连接状态只能包含一个 JSON 文档")
	}
	if target.FormatVersion != 3 {
		return fmt.Errorf("数据库连接状态格式版本 %d 不受支持；开发环境请删除旧状态后重建", target.FormatVersion)
	}
	if target.Tenants == nil {
		target.Tenants = map[string]map[string]definition{}
	}
	if target.Pending == nil {
		target.Pending = map[string]map[string]pendingDefinition{}
	}
	if target.Revisions == nil {
		target.Revisions = map[string]map[string]connectionIdentity{}
	}
	if target.Publications == nil {
		target.Publications = map[string]map[string]runtimePublication{}
	}
	if target.Retire == nil {
		target.Retire = map[string][]pluginconfig.ManagedCredentialRef{}
	}
	if target.TestCleanup == nil {
		target.TestCleanup = map[string][]testCredentialCleanup{}
	}
	return nil
}

func (s *service) save() error {
	raw, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	if s.state == nil {
		return errors.New("数据库 Shared State 保存缺少当前操作")
	}
	return s.state.save(raw)
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
