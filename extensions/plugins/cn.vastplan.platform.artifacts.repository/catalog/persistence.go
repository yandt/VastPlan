package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
)

func (s *Store) rebuild() error {
	refs, err := s.repository.ListRefs()
	if err != nil {
		return fmt.Errorf("枚举签名制品: %w", err)
	}
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		artifact, proof, provenanceRaw, verificationRaw, admissionRaw, err := s.readMetadata(ref)
		if err != nil {
			return fmt.Errorf("重建 Catalog 读取 %s: %w", refKey(ref), err)
		}
		entry, err := entryFrom(artifact, proof)
		if err != nil {
			return fmt.Errorf("重建 Catalog 解析 %s: %w", refKey(ref), err)
		}
		if err := populateProvenance(&entry, provenanceRaw, verificationRaw); err != nil {
			return fmt.Errorf("重建 Catalog 来源证明 %s: %w", refKey(ref), err)
		}
		if err := populateSecurityAdmission(&entry, admissionRaw); err != nil {
			return fmt.Errorf("重建 Catalog 安全准入记录 %s: %w", refKey(ref), err)
		}
		key := refKey(ref)
		seen[key] = struct{}{}
		if prior, ok := s.entries[key]; ok {
			if !sameIdentity(prior, entry) {
				return fmt.Errorf("发布流水账与制品不一致: %s", key)
			}
			entry.RepositoryRevision = prior.RepositoryRevision
			entry.PublishedAt = prior.PublishedAt
			applyLifecycle(&entry, lifecycleAt(s.lifecycle[key], s.revision))
			s.entries[key] = entry
			continue
		}
		event := eventFrom(entry, entry.SignedAt, true)
		if err := s.appendEventLocked(&event); err != nil {
			return fmt.Errorf("恢复发布流水账 %s: %w", key, err)
		}
		entry.RepositoryRevision = event.Revision
		entry.PublishedAt = event.OccurredAt
		entry.LifecycleStatus = LifecycleActive
		s.entries[key] = entry
	}
	for key := range s.entries {
		if _, ok := seen[key]; !ok {
			entry := s.entries[key]
			if s.retired == nil || !s.retired.AllowsMissing(entry.Ref, entry.SHA256) || (entry.LifecycleStatus != LifecycleYanked && entry.LifecycleStatus != LifecycleRevoked) {
				return fmt.Errorf("发布流水账引用的制品缺失: %s", key)
			}
		}
	}
	return s.writeSnapshotLocked()
}

func (s *Store) readMetadata(ref artifactrepository.Ref) (artifactrepository.Artifact, []byte, []byte, []byte, []byte, error) {
	if reader, ok := s.repository.(VerifiedMetadataSupplyChainReader); ok {
		return reader.ReadMetadataWithSupplyChain(ref)
	}
	if reader, ok := s.repository.(VerifiedMetadataProvenanceReader); ok {
		artifact, proof, provenance, verification, err := reader.ReadMetadataWithProvenance(ref)
		return artifact, proof, provenance, verification, nil, err
	}
	artifact, proof, err := s.repository.ReadMetadataWithAttestation(ref)
	return artifact, proof, nil, nil, nil, err
}

func (s *Store) loadJournal() error {
	entries, err := os.ReadDir(s.journalDir())
	if err != nil {
		return fmt.Errorf("读取发布流水账: %w", err)
	}
	for _, item := range entries {
		if strings.HasPrefix(item.Name(), ".tmp-") {
			continue
		}
		if item.IsDir() || len(item.Name()) != 25 || !strings.HasSuffix(item.Name(), ".json") {
			return fmt.Errorf("发布流水账包含未知文件: %s", item.Name())
		}
		revision, err := strconv.ParseUint(strings.TrimSuffix(item.Name(), ".json"), 10, 64)
		if err != nil || revision != s.revision+1 {
			return fmt.Errorf("发布流水账 revision 不连续: %s", item.Name())
		}
		raw, err := os.ReadFile(filepath.Join(s.journalDir(), item.Name()))
		if err != nil {
			return err
		}
		var event Event
		if err := decodeStrict(raw, &event); err != nil {
			return fmt.Errorf("解析发布流水账 %s: %w", item.Name(), err)
		}
		if err := validateEvent(event, revision); err != nil {
			return fmt.Errorf("校验发布流水账 %s: %w", item.Name(), err)
		}
		key := refKey(event.Ref)
		s.revision = revision
		s.events = append(s.events, event)
		switch event.Type {
		case "artifact.published":
			if _, duplicate := s.entries[key]; duplicate {
				return fmt.Errorf("发布流水账重复引用: %s", key)
			}
			s.entries[key] = Entry{
				Ref: event.Ref, SHA256: event.SHA256, Size: event.Size, Publisher: event.Publisher, KeyID: event.KeyID,
				SignedAt: event.SignedAt, PublishedAt: event.OccurredAt, RepositoryRevision: event.Revision, LifecycleStatus: LifecycleActive,
			}
		case "artifact.lifecycle":
			entry, exists := s.entries[key]
			if !exists || entry.SHA256 != event.SHA256 || currentLifecycleStatus(s.lifecycle[key]) != event.PreviousStatus {
				return fmt.Errorf("生命周期流水账前置状态不一致: %s", key)
			}
			transition := LifecycleTransition{Revision: event.Revision, Status: event.Status, Reason: event.Reason, Replacement: cloneRequirement(event.Replacement), OccurredAt: event.OccurredAt}
			s.lifecycle[key] = append(s.lifecycle[key], transition)
			applyLifecycle(&entry, transition)
			s.entries[key] = entry
		}
	}
	return nil
}

func (s *Store) appendEventLocked(event *Event) error {
	event.SchemaVersion = schemaVersion
	event.Revision = s.revision + 1
	if event.Type == "" {
		event.Type = "artifact.published"
	}
	raw, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return err
	}
	filename := filepath.Join(s.journalDir(), fmt.Sprintf("%020d.json", event.Revision))
	if err := writeNewFileAtomically(filename, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("写入发布流水账: %w", err)
	}
	s.revision = event.Revision
	s.events = append(s.events, *event)
	return nil
}

func (s *Store) writeSnapshotLocked() error {
	items := make([]Entry, 0, len(s.entries))
	for _, entry := range s.entries {
		items = append(items, entry)
	}
	sortEntries(items)
	raw, err := json.MarshalIndent(snapshot{SchemaVersion: schemaVersion, Revision: s.revision, Items: items}, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomically(filepath.Join(s.root, "index.json"), append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("写入 Catalog 快照: %w", err)
	}
	return nil
}

func (s *Store) journalDir() string { return filepath.Join(s.root, "journal") }
