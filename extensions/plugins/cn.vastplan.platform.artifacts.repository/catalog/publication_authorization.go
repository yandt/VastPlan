package catalog

import (
	"errors"
	"fmt"
	"sort"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
)

func (s *Store) AuthorizePublication(attestation pluginservice.Attestation, now time.Time) (string, error) {
	return s.AuthorizePublicationWithProvenance(attestation, nil, nil, now)
}

func (s *Store) AuthorizePublicationWithProvenance(attestation pluginservice.Attestation, provenanceRaw, verificationRaw []byte, now time.Time) (string, error) {
	return s.AuthorizePublicationWithSupplyChain(attestation, provenanceRaw, verificationRaw, nil, now)
}

func (s *Store) AuthorizePublicationWithSupplyChain(attestation pluginservice.Attestation, provenanceRaw, verificationRaw, admissionRaw []byte, now time.Time) (string, error) {
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
		if record.SourceProvenanceSHA256 != digestOptional(provenanceRaw) || record.SourceProvenanceVerificationSHA256 != digestOptional(verificationRaw) {
			return "", errors.New("stable 来源证明与已批准 testing sidecar 不一致")
		}
		if record.SourceSecurityAdmissionSHA256 != digestOptional(admissionRaw) {
			return "", errors.New("stable 安全准入记录与已批准 testing sidecar 不一致")
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
	return s.MarkPublicationPublishedWithProvenance(id, attestationRaw, nil, nil, now)
}

func (s *Store) MarkPublicationPublishedWithProvenance(id string, attestationRaw, provenanceRaw, verificationRaw []byte, now time.Time) error {
	return s.MarkPublicationPublishedWithSupplyChain(id, attestationRaw, provenanceRaw, verificationRaw, nil, now)
}

func (s *Store) MarkPublicationPublishedWithSupplyChain(id string, attestationRaw, provenanceRaw, verificationRaw, admissionRaw []byte, now time.Time) error {
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
	provenanceDigest, verificationDigest, admissionDigest := digestOptional(provenanceRaw), digestOptional(verificationRaw), digestOptional(admissionRaw)
	if record.Status == PublicationPublished {
		if record.PublishedAttestationSHA256 != proofDigest || record.PublishedProvenanceSHA256 != provenanceDigest || record.PublishedProvenanceVerificationSHA256 != verificationDigest || record.PublishedSecurityAdmissionSHA256 != admissionDigest {
			return errors.New("已发布证明摘要不一致")
		}
		return nil
	}
	prior := record
	previousRevision := s.publicationRevision
	record.Revision, record.Status, record.PublishedAttestationSHA256, record.PublishedProvenanceSHA256, record.PublishedProvenanceVerificationSHA256, record.PublishedSecurityAdmissionSHA256, record.PublishedAt = s.publicationRevision+1, PublicationPublished, proofDigest, provenanceDigest, verificationDigest, admissionDigest, now.UTC().Format(time.RFC3339Nano)
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
