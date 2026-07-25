package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

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
func publicationID(source, target pluginv1.ArtifactRef, digest, provenanceDigest, verificationDigest, admissionDigest string) string {
	sum := sha256.Sum256([]byte(refKey(source) + "\x00" + refKey(target) + "\x00" + digest + "\x00" + provenanceDigest + "\x00" + verificationDigest + "\x00" + admissionDigest))
	return hex.EncodeToString(sum[:])
}
func digestBytes(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func digestOptional(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	return digestBytes(raw)
}
func publicationInventoryDigest(values map[string]Publication) string {
	items := make([]Publication, 0, len(values))
	for _, item := range values {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	raw, _ := json.Marshal(items)
	return digestBytes(raw)
}
