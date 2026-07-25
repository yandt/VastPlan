package credentials

import (
	"errors"
	"sync"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
)

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
	Tenants            map[string]map[string]Record        `json:"tenants"`
	Managed            map[string]map[string]ManagedRecord `json:"managed"`
	ManagedAudit       map[string]managedAuditState        `json:"managedAudit,omitempty"`
	ManagedMaintenance map[string]ManagedMaintenanceStatus `json:"managedMaintenance,omitempty"`
}

const (
	managedPreparing = "Preparing"
	managedCandidate = "Candidate"
	managedActive    = "Active"
	managedAborted   = "Aborted"
	managedRetired   = "Retired"
)

// ManagedRecord is the custodian-owned representation. Ciphertext is persisted
// only here; callers receive Ref and StageID, never ciphertext or plaintext.
type ManagedRecord struct {
	StageID         string                            `json:"stageId"`
	Ref             pluginconfig.ManagedCredentialRef `json:"ref"`
	Resource        string                            `json:"resource"`
	State           string                            `json:"state"`
	CreatedAt       time.Time                         `json:"createdAt"`
	UpdatedAt       time.Time                         `json:"updatedAt"`
	Ciphertext      string                            `json:"ciphertext,omitempty"`
	AuthorityID     string                            `json:"authorityId,omitempty"`
	Coordinator     string                            `json:"coordinator,omitempty"`
	CandidateID     string                            `json:"candidateId,omitempty"`
	ConfigurationID string                            `json:"configurationId,omitempty"`
	FieldID         string                            `json:"fieldId,omitempty"`
}
type Service struct {
	mu          sync.Mutex
	workflowMu  sync.Mutex
	transit     Transit
	data        persisted
	session     *credentialStateSession
	testSave    func(persisted) error
	leaseSlots  chan struct{}
	maintenance MaintenancePolicy
	now         func() time.Time
}

func New(transit Transit) (*Service, error) {
	policy, _ := (Configuration{}).Policy()
	return NewWithOptions(transit, ServiceOptions{Maintenance: policy})
}

type ServiceOptions struct {
	Maintenance MaintenancePolicy
	Now         func() time.Time
}

func NewWithOptions(transit Transit, options ServiceOptions) (*Service, error) {
	if transit == nil {
		return nil, errors.New("凭证 Transit 适配器不能为空")
	}
	if options.Maintenance.Interval == 0 {
		options.Maintenance, _ = (Configuration{}).Policy()
	}
	if options.Maintenance.OrphanChunkGrace == 0 {
		options.Maintenance.OrphanChunkGrace = defaultOrphanChunkGrace
	}
	if options.Maintenance.ChunkGCBatchSize == 0 {
		options.Maintenance.ChunkGCBatchSize = defaultChunkGCBatch
	}
	if err := validateMaintenancePolicy(options.Maintenance); err != nil {
		return nil, err
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	s := &Service{
		transit: transit, leaseSlots: make(chan struct{}, 32), maintenance: options.Maintenance, now: options.Now,
		data: persisted{Tenants: map[string]map[string]Record{}, Managed: map[string]map[string]ManagedRecord{}, ManagedAudit: map[string]managedAuditState{}, ManagedMaintenance: map[string]ManagedMaintenanceStatus{}},
	}
	return s, nil
}
