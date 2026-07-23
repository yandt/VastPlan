package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
)

const (
	PublicationPending   = "PendingApproval"
	PublicationApproved  = "Approved"
	PublicationPublished = "Published"
	PublicationRejected  = "Rejected"
	PublicationCancelled = "Cancelled"
	PublicationExpired   = "Expired"
)

type Publication = platformadminapi.ArtifactPublication
type PublicationRequest = platformadminapi.ArtifactPublicationRequest
type PublicationApprovalRequest = platformadminapi.ArtifactPublicationApprovalRequest
type PublicationTransitionRequest = platformadminapi.ArtifactPublicationTransitionRequest
type PublicationPage = platformadminapi.ArtifactPublicationPage

type publicationSnapshot struct {
	SchemaVersion string        `json:"schemaVersion"`
	Revision      uint64        `json:"revision"`
	Items         []Publication `json:"items"`
}

type SupplyChainEvidence = platformadminapi.ArtifactSupplyChainEvidence

func (s *Store) SubmitPublication(request PublicationRequest, actor string, now, expiresAt time.Time) (Publication, uint64, error) {
	actor, request.Reason = strings.TrimSpace(actor), strings.TrimSpace(request.Reason)
	if actor == "" || request.Reason == "" || len([]rune(request.Reason)) > 500 || request.Source.Channel != "testing" || request.TargetChannel != "stable" {
		return Publication{}, s.PublicationRevision(), errors.New("发布审批只接受 testing 到 stable 的完整申请")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if expiresAt.IsZero() || !expiresAt.After(now) {
		return Publication{}, s.publicationRevision, errors.New("发布审批有效期无效")
	}
	if err := s.expirePublicationsLocked(now); err != nil {
		return Publication{}, s.publicationRevision, err
	}
	if request.ExpectedRevision != s.publicationRevision {
		return Publication{}, s.publicationRevision, fmt.Errorf("发布审批 revision 冲突: expected=%d actual=%d", request.ExpectedRevision, s.publicationRevision)
	}
	entry, ok := s.entries[refKey(request.Source)]
	if !ok || entry.LifecycleStatus != LifecycleActive {
		return Publication{}, s.publicationRevision, errors.New("候选 testing 制品不存在或不在 active 状态")
	}
	_, proof, err := s.repository.ReadMetadataWithAttestation(request.Source)
	if err != nil {
		return Publication{}, s.publicationRevision, errors.New("候选制品供应链证明复验失败")
	}
	target := request.Source
	target.Channel = request.TargetChannel
	if existing, exists := s.entries[refKey(target)]; exists {
		if existing.SHA256 == entry.SHA256 {
			return Publication{}, s.publicationRevision, errors.New("目标 stable 制品已经发布")
		}
		return Publication{}, s.publicationRevision, errors.New("目标 stable 引用已被其他不可变制品占用")
	}
	id := publicationID(request.Source, target, entry.SHA256)
	if prior, exists := s.publications[id]; exists {
		return prior, s.publicationRevision, nil
	}
	record := Publication{ID: id, Revision: s.publicationRevision + 1, Status: PublicationPending, Source: request.Source, Target: target, SHA256: entry.SHA256, Publisher: entry.Publisher, KeyID: entry.KeyID, SourceAttestationSHA256: digestBytes(proof), Reason: request.Reason, SubmittedBy: actor, SubmittedAt: now.UTC().Format(time.RFC3339Nano), ExpiresAt: expiresAt.UTC().Format(time.RFC3339Nano)}
	previousRevision := s.publicationRevision
	s.publicationRevision, s.publications[id] = record.Revision, record
	if err := s.writePublicationsLocked(); err != nil {
		delete(s.publications, id)
		s.publicationRevision = previousRevision
		return Publication{}, previousRevision, err
	}
	return record, s.publicationRevision, nil
}

func (s *Store) ApprovePublication(request PublicationApprovalRequest, actor string, now time.Time) (Publication, uint64, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" || request.ID == "" {
		return Publication{}, s.PublicationRevision(), errors.New("发布审批请求无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := s.expirePublicationsLocked(now); err != nil {
		return Publication{}, s.publicationRevision, err
	}
	if request.ExpectedRevision != s.publicationRevision {
		return Publication{}, s.publicationRevision, fmt.Errorf("发布审批 revision 冲突: expected=%d actual=%d", request.ExpectedRevision, s.publicationRevision)
	}
	record, ok := s.publications[request.ID]
	if !ok {
		return Publication{}, s.publicationRevision, errors.New("发布审批不存在")
	}
	if record.Status != PublicationPending {
		return Publication{}, s.publicationRevision, errors.New("发布审批不在待批准状态")
	}
	if record.SubmittedBy == actor {
		return Publication{}, s.publicationRevision, errors.New("提交人与批准人必须分离")
	}
	prior := record
	previousRevision := s.publicationRevision
	record.Revision, record.Status, record.ApprovedBy, record.ApprovedAt = s.publicationRevision+1, PublicationApproved, actor, now.UTC().Format(time.RFC3339Nano)
	s.publicationRevision, s.publications[request.ID] = record.Revision, record
	if err := s.writePublicationsLocked(); err != nil {
		s.publications[request.ID], s.publicationRevision = prior, previousRevision
		return Publication{}, previousRevision, err
	}
	return record, s.publicationRevision, nil
}

func (s *Store) RejectPublication(request PublicationTransitionRequest, actor string, now time.Time) (Publication, uint64, error) {
	return s.terminatePublication(request, actor, now, PublicationRejected, false)
}

func (s *Store) CancelPublication(request PublicationTransitionRequest, actor string, now time.Time) (Publication, uint64, error) {
	return s.terminatePublication(request, actor, now, PublicationCancelled, true)
}

func (s *Store) terminatePublication(request PublicationTransitionRequest, actor string, now time.Time, status string, submitterOnly bool) (Publication, uint64, error) {
	actor, request.Reason = strings.TrimSpace(actor), strings.TrimSpace(request.Reason)
	if actor == "" || request.ID == "" || request.Reason == "" || len([]rune(request.Reason)) > 500 {
		return Publication{}, s.PublicationRevision(), errors.New("发布审批终止请求无效")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.expirePublicationsLocked(now); err != nil {
		return Publication{}, s.publicationRevision, err
	}
	if request.ExpectedRevision != s.publicationRevision {
		return Publication{}, s.publicationRevision, fmt.Errorf("发布审批 revision 冲突: expected=%d actual=%d", request.ExpectedRevision, s.publicationRevision)
	}
	record, ok := s.publications[request.ID]
	if !ok {
		return Publication{}, s.publicationRevision, errors.New("发布审批不存在")
	}
	if record.Status != PublicationPending && record.Status != PublicationApproved {
		return Publication{}, s.publicationRevision, errors.New("发布审批已处于不可变终态")
	}
	if submitterOnly && record.SubmittedBy != actor {
		return Publication{}, s.publicationRevision, errors.New("只有原提交人可以撤销发布审批")
	}
	if !submitterOnly && record.SubmittedBy == actor {
		return Publication{}, s.publicationRevision, errors.New("提交人与驳回人必须分离")
	}
	prior, previousRevision := record, s.publicationRevision
	record.Revision, record.Status = s.publicationRevision+1, status
	record.TerminalReason, record.TerminalBy, record.TerminalAt = request.Reason, actor, now.UTC().Format(time.RFC3339Nano)
	s.publicationRevision, s.publications[request.ID] = record.Revision, record
	if err := s.writePublicationsLocked(); err != nil {
		s.publications[request.ID], s.publicationRevision = prior, previousRevision
		return Publication{}, previousRevision, err
	}
	return record, s.publicationRevision, nil
}

func (s *Store) AuthorizePublication(attestation pluginservice.Attestation, now time.Time) (string, error) {
	if attestation.Artifact.Channel != "stable" {
		return "", nil
	}
	target := pluginv1.ArtifactRef{PluginID: attestation.Artifact.PluginID, Version: attestation.Artifact.Version, Channel: attestation.Artifact.Channel}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.expirePublicationsLocked(now); err != nil {
		return "", fmt.Errorf("收敛发布审批过期状态: %w", err)
	}
	for _, record := range s.publications {
		if record.Target != target || record.SHA256 != attestation.Artifact.SHA256 || record.Publisher != attestation.Publisher || record.KeyID != attestation.KeyID || (record.Status != PublicationApproved && record.Status != PublicationPublished) {
			continue
		}
		if record.Status == PublicationApproved {
			source, exists := s.entries[refKey(record.Source)]
			if !exists || source.LifecycleStatus != LifecycleActive || source.SHA256 != record.SHA256 || source.Publisher != record.Publisher || source.KeyID != record.KeyID {
				return "", errors.New("已批准发布的 testing 候选不再有效")
			}
		}
		return record.ID, nil
	}
	return "", errors.New("stable 制品缺少精确匹配且已批准的发布申请")
}

func (s *Store) MarkPublicationPublished(id string, attestationRaw []byte, now time.Time) error {
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := s.expirePublicationsLocked(now); err != nil {
		return err
	}
	record, ok := s.publications[id]
	if !ok || (record.Status != PublicationApproved && record.Status != PublicationPublished) {
		return errors.New("发布批准状态已失效")
	}
	proofDigest := digestBytes(attestationRaw)
	if record.Status == PublicationPublished {
		if record.PublishedAttestationSHA256 != proofDigest {
			return errors.New("已发布证明摘要不一致")
		}
		return nil
	}
	prior := record
	previousRevision := s.publicationRevision
	record.Revision, record.Status, record.PublishedAttestationSHA256, record.PublishedAt = s.publicationRevision+1, PublicationPublished, proofDigest, now.UTC().Format(time.RFC3339Nano)
	s.publicationRevision, s.publications[id] = record.Revision, record
	if err := s.writePublicationsLocked(); err != nil {
		s.publications[id], s.publicationRevision = prior, previousRevision
		return err
	}
	return nil
}

func (s *Store) ExpirePublications(now time.Time) (uint64, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.expirePublicationsLocked(now); err != nil {
		return s.publicationRevision, err
	}
	return s.publicationRevision, nil
}

func (s *Store) expirePublicationsLocked(now time.Time) error {
	ids := make([]string, 0)
	for id, record := range s.publications {
		if record.Status != PublicationPending && record.Status != PublicationApproved {
			continue
		}
		expiresAt, err := time.Parse(time.RFC3339Nano, record.ExpiresAt)
		if err != nil {
			return errors.New("发布审批过期时间无效")
		}
		if !expiresAt.After(now) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	previousRevision := s.publicationRevision
	prior := make(map[string]Publication, len(ids))
	for _, id := range ids {
		record := s.publications[id]
		prior[id] = record
		s.publicationRevision++
		record.Revision, record.Status = s.publicationRevision, PublicationExpired
		record.TerminalReason, record.TerminalBy, record.TerminalAt = "发布审批有效期已结束", "system", now.UTC().Format(time.RFC3339Nano)
		s.publications[id] = record
	}
	if err := s.writePublicationsLocked(); err != nil {
		for id, record := range prior {
			s.publications[id] = record
		}
		s.publicationRevision = previousRevision
		return err
	}
	return nil
}

func (s *Store) Publications() PublicationPage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Publication, 0, len(s.publications))
	for _, item := range s.publications {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Revision > items[j].Revision })
	return PublicationPage{Revision: s.publicationRevision, Items: items}
}

func (s *Store) PublicationRevision() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.publicationRevision
}

func (s *Store) Evidence(ref pluginv1.ArtifactRef) (SupplyChainEvidence, error) {
	s.mu.RLock()
	entry, ok := s.entries[refKey(ref)]
	s.mu.RUnlock()
	if !ok {
		return SupplyChainEvidence{}, errors.New("制品不存在")
	}
	_, proof, err := s.repository.ReadMetadataWithAttestation(ref)
	if err != nil {
		return SupplyChainEvidence{}, errors.New("制品供应链证明复验失败")
	}
	page := s.Publications()
	related := make([]Publication, 0)
	for _, item := range page.Items {
		if item.Source == ref || item.Target == ref {
			related = append(related, item)
		}
	}
	return SupplyChainEvidence{Ref: ref, SHA256: entry.SHA256, Size: entry.Size, Publisher: entry.Publisher, KeyID: entry.KeyID, SignedAt: entry.SignedAt.Format(time.RFC3339Nano), AttestationSHA256: digestBytes(proof), Verification: "verified", Name: entry.Name, Description: entry.Description, License: entry.License, Targets: append([]string(nil), entry.Targets...), Engines: cloneStringMap(entry.Engines), RepositoryRevision: entry.RepositoryRevision, LifecycleStatus: entry.LifecycleStatus, Publications: related}, nil
}

func (s *Store) loadPublications() error {
	raw, err := os.ReadFile(s.publicationsPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取发布审批状态: %w", err)
	}
	var snapshot publicationSnapshot
	if err := decodeStrict(raw, &snapshot); err != nil || snapshot.SchemaVersion != schemaVersion {
		return errors.New("发布审批状态无效")
	}
	for _, item := range snapshot.Items {
		if item.ID == "" || item.Revision == 0 || item.Revision > snapshot.Revision || !validPublicationStatus(item.Status) {
			return errors.New("发布审批记录无效")
		}
		submittedAt, submitErr := time.Parse(time.RFC3339Nano, item.SubmittedAt)
		expiresAt, expiryErr := time.Parse(time.RFC3339Nano, item.ExpiresAt)
		if submitErr != nil || expiryErr != nil || !expiresAt.After(submittedAt) {
			return errors.New("发布审批有效期缺失")
		}
		terminal := item.Status == PublicationRejected || item.Status == PublicationCancelled || item.Status == PublicationExpired
		if terminal != (item.TerminalReason != "" && item.TerminalBy != "" && item.TerminalAt != "") {
			return errors.New("发布审批终态审计字段无效")
		}
		if terminal {
			if _, err := time.Parse(time.RFC3339Nano, item.TerminalAt); err != nil {
				return errors.New("发布审批终态时间无效")
			}
		}
		if (item.Status == PublicationApproved || item.Status == PublicationPublished) && (item.ApprovedBy == "" || item.ApprovedAt == "") {
			return errors.New("发布审批批准审计字段无效")
		}
		if item.Status == PublicationPublished && (item.PublishedAt == "" || item.PublishedAttestationSHA256 == "") {
			return errors.New("发布审批发布审计字段无效")
		}
		if _, duplicate := s.publications[item.ID]; duplicate {
			return errors.New("发布审批 ID 重复")
		}
		s.publications[item.ID] = item
	}
	s.publicationRevision = snapshot.Revision
	changed := false
	for id, item := range s.publications {
		entry, published := s.entries[refKey(item.Target)]
		if item.Status == PublicationPublished {
			if !published || entry.SHA256 != item.SHA256 || entry.Publisher != item.Publisher || entry.KeyID != item.KeyID {
				return errors.New("已发布审批与 Catalog 不一致")
			}
			continue
		}
		if item.Status != PublicationApproved || !published {
			continue
		}
		if entry.SHA256 != item.SHA256 || entry.Publisher != item.Publisher || entry.KeyID != item.KeyID {
			return errors.New("已批准发布与 Catalog 不一致")
		}
		_, proof, proofErr := s.repository.ReadMetadataWithAttestation(item.Target)
		if proofErr != nil {
			return errors.New("恢复已发布审批时证明复验失败")
		}
		s.publicationRevision++
		item.Revision, item.Status, item.PublishedAttestationSHA256, item.PublishedAt = s.publicationRevision, PublicationPublished, digestBytes(proof), entry.PublishedAt.Format(time.RFC3339Nano)
		s.publications[id], changed = item, true
	}
	if changed {
		return s.writePublicationsLocked()
	}
	return nil
}

func validPublicationStatus(status string) bool {
	switch status {
	case PublicationPending, PublicationApproved, PublicationPublished, PublicationRejected, PublicationCancelled, PublicationExpired:
		return true
	default:
		return false
	}
}

func (s *Store) writePublicationsLocked() error {
	items := make([]Publication, 0, len(s.publications))
	for _, item := range s.publications {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Revision < items[j].Revision })
	raw, err := json.MarshalIndent(publicationSnapshot{SchemaVersion: schemaVersion, Revision: s.publicationRevision, Items: items}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomically(s.publicationsPath(), append(raw, '\n'), 0o600)
}

func (s *Store) publicationsPath() string { return s.root + "/publications.json" }
func publicationID(source, target pluginv1.ArtifactRef, digest string) string {
	sum := sha256.Sum256([]byte(refKey(source) + "\x00" + refKey(target) + "\x00" + digest))
	return hex.EncodeToString(sum[:])
}
func digestBytes(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func publicationInventoryDigest(values map[string]Publication) string {
	items := make([]Publication, 0, len(values))
	for _, item := range values {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	raw, _ := json.Marshal(items)
	return digestBytes(raw)
}
