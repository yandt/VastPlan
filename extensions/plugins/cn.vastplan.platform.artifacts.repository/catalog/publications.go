package catalog

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
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

type verifiedPackageReader interface {
	ReadWithAttestation(artifactrepository.Ref) (artifactrepository.Artifact, []byte, []byte, error)
}

type verifiedProvenanceReader interface {
	ReadProvenance(artifactrepository.Ref) ([]byte, []byte, error)
}

type verifiedSecurityAdmissionReader interface {
	ReadSecurityAdmission(artifactrepository.Ref) ([]byte, error)
}

type verifiedSecurityStatusReader interface {
	ReadSecurityStatusChain(artifactrepository.Ref) ([]byte, error)
}

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
	var provenanceRaw, verificationRaw []byte
	if reader, ok := s.repository.(verifiedProvenanceReader); ok {
		provenanceRaw, verificationRaw, err = reader.ReadProvenance(request.Source)
		if err != nil {
			return Publication{}, s.publicationRevision, errors.New("候选制品来源证明复验失败")
		}
	}
	var admissionRaw []byte
	if reader, ok := s.repository.(verifiedSecurityAdmissionReader); ok {
		admissionRaw, err = reader.ReadSecurityAdmission(request.Source)
		if err != nil {
			return Publication{}, s.publicationRevision, errors.New("候选制品安全准入记录复验失败")
		}
	}
	target := request.Source
	target.Channel = request.TargetChannel
	if existing, exists := s.entries[refKey(target)]; exists {
		if existing.SHA256 == entry.SHA256 {
			return Publication{}, s.publicationRevision, errors.New("目标 stable 制品已经发布")
		}
		return Publication{}, s.publicationRevision, errors.New("目标 stable 引用已被其他不可变制品占用")
	}
	provenanceSHA, verificationSHA, admissionSHA := digestOptional(provenanceRaw), digestOptional(verificationRaw), digestOptional(admissionRaw)
	id := publicationID(request.Source, target, entry.SHA256, provenanceSHA, verificationSHA, admissionSHA)
	if prior, exists := s.publications[id]; exists {
		return prior, s.publicationRevision, nil
	}
	record := Publication{ID: id, Revision: s.publicationRevision + 1, Status: PublicationPending, Source: request.Source, Target: target, SHA256: entry.SHA256, Publisher: entry.Publisher, KeyID: entry.KeyID, SourceAttestationSHA256: digestBytes(proof), SourceProvenanceSHA256: provenanceSHA, SourceProvenanceVerificationSHA256: verificationSHA, SourceSecurityAdmissionSHA256: admissionSHA, Reason: request.Reason, SubmittedBy: actor, SubmittedAt: now.UTC().Format(time.RFC3339Nano), ExpiresAt: expiresAt.UTC().Format(time.RFC3339Nano)}
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
