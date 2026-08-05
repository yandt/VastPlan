package apiexposure

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
)

const stateFormatVersion = 1
const maximumStateBytes = 1 << 20

var routeKeyPattern = regexp.MustCompile(`^[a-z2-7]{20}$`)

type persistedState struct {
	FormatVersion      int                  `json:"formatVersion"`
	NextRevision       uint64               `json:"nextRevision"`
	NextAudit          uint64               `json:"nextAudit"`
	CatalogGeneration  uint64               `json:"catalogGeneration"`
	CatalogDirty       bool                 `json:"catalogDirty"`
	Revisions          []Revision           `json:"revisions"`
	DataPlaneRevisions []DataPlaneRevision  `json:"dataPlaneRevisions"`
	Tombstones         map[string]time.Time `json:"tombstones"`
	Audit              []AuditEvent         `json:"audit"`
}

type Service struct {
	mu                 sync.Mutex
	workflowMu         sync.Mutex
	state              persistedState
	gatewayCatalogFile string
	contractCatalog    apiv1.ContractCatalog
	configured         bool
	stateSession       *apiExposureStateSession
	testSave           func(persistedState) error
	catalogRestored    bool
	now                func() time.Time
	leases             map[string]apiv1.EndpointLease
	leaseOwners        map[string]RuntimeCaller
	tickets            map[string]ticketRecord
	leaseCursor        uint64
}

func EmptyContractCatalog() apiv1.ContractCatalog {
	return apiv1.ContractCatalog{SchemaVersion: apiv1.SchemaVersion, Generation: 1, Contracts: []apiv1.ResolvedContract{}, DataPlaneServices: []apiv1.ResolvedDataPlaneService{}}
}

func New(gatewayCatalogFile string, catalog apiv1.ContractCatalog) (*Service, error) {
	s := &Service{
		state: persistedState{FormatVersion: stateFormatVersion, Tombstones: map[string]time.Time{}},
		now:   time.Now, leases: map[string]apiv1.EndpointLease{}, leaseOwners: map[string]RuntimeCaller{}, tickets: map[string]ticketRecord{},
	}
	if err := s.configure(gatewayCatalogFile, catalog); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) configure(gatewayCatalogFile string, catalog apiv1.ContractCatalog) error {
	if strings.TrimSpace(gatewayCatalogFile) == "" {
		return errors.New("API Exposure gatewayCatalogFile 不能为空")
	}
	if err := apiv1.ValidateContractCatalog(catalog); err != nil {
		return fmt.Errorf("API Contract Catalog 无效: %w", err)
	}
	if s.configured {
		if s.gatewayCatalogFile != gatewayCatalogFile || s.contractCatalog.Generation != catalog.Generation {
			return errors.New("API Exposure 运行中不允许切换 Gateway 或 Contract Catalog")
		}
		return nil
	}
	s.gatewayCatalogFile, s.contractCatalog = gatewayCatalogFile, catalog
	s.configured = true
	return nil
}

func decodePersistedState(raw []byte) (persistedState, error) {
	if len(raw) == 0 || len(raw) > maximumStateBytes {
		return persistedState{}, errors.New("API Exposure Shared State 为空或超过 1 MiB")
	}
	var state persistedState
	if err := json.Unmarshal(raw, &state); err != nil {
		return persistedState{}, fmt.Errorf("解析 API Exposure Shared State: %w", err)
	}
	if state.FormatVersion != stateFormatVersion {
		return persistedState{}, fmt.Errorf("API Exposure 状态格式版本 %d 不受支持", state.FormatVersion)
	}
	if state.Tombstones == nil {
		state.Tombstones = map[string]time.Time{}
	}
	if err := validatePersistedState(state); err != nil {
		return persistedState{}, err
	}
	return state, nil
}

func validatePersistedState(state persistedState) error {
	revisions := map[uint64]struct{}{}
	resourceKeys := map[string]string{}
	for _, revision := range state.Revisions {
		if revision.ID == 0 || !validStatus(revision.Status) {
			return errors.New("API Exposure revision 状态无效")
		}
		if _, duplicate := revisions[revision.ID]; duplicate {
			return errors.New("API Exposure revision id 重复")
		}
		revisions[revision.ID] = struct{}{}
		catalog := apiv1.ExposureCatalog{SchemaVersion: apiv1.SchemaVersion, Generation: 1, Exposures: []apiv1.ResolvedExposure{{Exposure: revision.Exposure, Contract: revision.Contract}}, DataPlaneExposures: []apiv1.DataPlaneExposure{}}
		if err := apiv1.ValidateExposureCatalog(catalog); err != nil {
			return fmt.Errorf("持久化 API Exposure 无效: %w", err)
		}
		if existing, ok := resourceKeys[revision.Exposure.RouteKey]; ok && existing != revision.Exposure.ID {
			return errors.New("API Exposure Route Key 被不同资源复用")
		}
		resourceKeys[revision.Exposure.RouteKey] = revision.Exposure.ID
	}
	for _, revision := range state.DataPlaneRevisions {
		if revision.ID == 0 || !validStatus(revision.Status) {
			return errors.New("Data Plane revision 状态无效")
		}
		if _, duplicate := revisions[revision.ID]; duplicate {
			return errors.New("API Exposure revision id 重复")
		}
		revisions[revision.ID] = struct{}{}
		if err := apiv1.ValidateDataPlaneExposure(revision.Exposure); err != nil {
			return fmt.Errorf("持久化 Data Plane Exposure 无效: %w", err)
		}
		if existing, ok := resourceKeys[revision.Exposure.RouteKey]; ok && existing != revision.Exposure.ID {
			return errors.New("Route Key 被 HTTP 与 Data Plane 资源复用")
		}
		resourceKeys[revision.Exposure.RouteKey] = revision.Exposure.ID
	}
	for key := range state.Tombstones {
		if !routeKeyPattern.MatchString(key) {
			return errors.New("API Exposure tombstone Route Key 无效")
		}
	}
	return nil
}

func validStatus(status Status) bool {
	switch status {
	case StatusDraft, StatusPendingApproval, StatusApproved, StatusPublished, StatusSuperseded, StatusRetired:
		return true
	default:
		return false
	}
}

func (s *Service) saveLocked() error {
	if s.testSave != nil {
		return s.testSave(s.state)
	}
	if s.stateSession == nil {
		return ErrStoreUnavailable
	}
	revision, err := s.stateSession.repository.save(s.stateSession.ctx, s.stateSession.call, s.state, s.stateSession.revision)
	if err == nil {
		s.stateSession.revision = revision
	}
	return err
}

func (s *Service) saveAndPublishLocked() error {
	s.state.CatalogDirty = true
	if err := s.saveLocked(); err != nil {
		return err
	}
	if err := s.publishCatalogLocked(); err != nil {
		return err
	}
	s.state.CatalogDirty = false
	return s.saveLocked()
}

func (s *Service) publishCatalogLocked() error {
	catalog := apiv1.ExposureCatalog{SchemaVersion: apiv1.SchemaVersion, Generation: s.state.CatalogGeneration, Exposures: []apiv1.ResolvedExposure{}, DataPlaneExposures: []apiv1.DataPlaneExposure{}}
	for _, revision := range s.state.Revisions {
		if revision.Status != StatusPublished {
			continue
		}
		resolved, err := s.resolveContract(ContractSelector{PluginID: revision.Exposure.Contract.PluginID, ArtifactSHA256: revision.Exposure.Contract.ArtifactSHA256, ContributionID: revision.Exposure.Contract.ContributionID})
		if err != nil || resolved.Reference.ContractDigest != revision.Exposure.Contract.ContractDigest {
			continue
		}
		catalog.Exposures = append(catalog.Exposures, apiv1.ResolvedExposure{Exposure: revision.Exposure, Contract: revision.Contract})
	}
	sort.Slice(catalog.Exposures, func(i, j int) bool {
		return catalog.Exposures[i].Exposure.RouteKey < catalog.Exposures[j].Exposure.RouteKey
	})
	for _, revision := range s.state.DataPlaneRevisions {
		if revision.Status == StatusPublished {
			if _, err := s.resolveDataPlaneService(revision.Exposure.Service); err != nil {
				continue
			}
			catalog.DataPlaneExposures = append(catalog.DataPlaneExposures, revision.Exposure)
		}
	}
	sort.Slice(catalog.DataPlaneExposures, func(i, j int) bool {
		return catalog.DataPlaneExposures[i].RouteKey < catalog.DataPlaneExposures[j].RouteKey
	})
	if err := apiv1.ValidateExposureCatalog(catalog); err != nil {
		return fmt.Errorf("生成 API Exposure Gateway Catalog: %w", err)
	}
	raw, err := json.Marshal(catalog)
	if err != nil {
		return err
	}
	return atomicWrite(s.gatewayCatalogFile, raw, 0o600)
}

func atomicWrite(path string, raw []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".api-exposure-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (s *Service) resolveContract(selector ContractSelector) (apiv1.ResolvedContract, error) {
	for _, resolved := range s.contractCatalog.Contracts {
		reference := resolved.Reference
		if reference.PluginID == selector.PluginID && reference.ArtifactSHA256 == selector.ArtifactSHA256 && reference.ContributionID == selector.ContributionID {
			return resolved, nil
		}
	}
	return apiv1.ResolvedContract{}, errors.New("Contract Selector 未命中可信 Contract Catalog")
}

func (s *Service) resolveDataPlaneService(reference apiv1.DataPlaneServiceReference) (apiv1.ResolvedDataPlaneService, error) {
	for _, resolved := range s.contractCatalog.DataPlaneServices {
		if resolved.Reference == reference {
			return resolved, nil
		}
	}
	return apiv1.ResolvedDataPlaneService{}, errors.New("Data Plane Service 未命中可信 Contract Catalog")
}
