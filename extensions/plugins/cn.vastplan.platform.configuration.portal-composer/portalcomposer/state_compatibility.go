package portalcomposer

import (
	"encoding/json"
	"fmt"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func composerStateDataFormat(raw []byte) (int, error) {
	var header struct {
		DataFormatVersion int `json:"dataFormatVersion"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return 0, err
	}
	return header.DataFormatVersion, nil
}

// normalizeLegacyComposerNavigation performs the v4 to v5 data migration for
// persisted Portal history. Only the two retired Shell-owned navigation fields
// are removed; plugin-owned navigationOverrides and all other configuration
// remain untouched.
func normalizeLegacyComposerNavigation(raw []byte) ([]byte, bool, error) {
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, false, err
	}
	migrated := removeLegacyShellNavigation(document)
	if !migrated {
		return raw, false, nil
	}
	normalized, err := json.Marshal(document)
	if err != nil {
		return nil, false, fmt.Errorf("编码已迁移 Portal 状态: %w", err)
	}
	return normalized, true, nil
}

func removeLegacyShellNavigation(value any) bool {
	migrated := false
	switch typed := value.(type) {
	case map[string]any:
		if _, hasContract := typed["uiContract"]; hasContract {
			if config, ok := typed["config"].(map[string]any); ok {
				if _, exists := config["navigationGroups"]; exists {
					delete(config, "navigationGroups")
					migrated = true
				}
				if _, exists := config["navigationPlacements"]; exists {
					delete(config, "navigationPlacements")
					migrated = true
				}
			}
		}
		for _, child := range typed {
			migrated = removeLegacyShellNavigation(child) || migrated
		}
	case []any:
		for _, child := range typed {
			migrated = removeLegacyShellNavigation(child) || migrated
		}
	}
	return migrated
}

func migrateComposerConfigurationDigests(value *state) error {
	service := New(nil)
	service.state = *value
	digests := make(map[uint64]string)
	for index := range value.Revisions {
		revision := value.Revisions[index]
		if revision.Status == portalapi.StatusDraft {
			continue
		}
		configuration, err := service.portalConfigurationLocked(revision.TenantID, revision)
		if err != nil {
			return fmt.Errorf("重建 PortalVersion %d 配置: %w", revision.ID, err)
		}
		digest, err := portalConfigurationDigest(configuration)
		if err != nil {
			return err
		}
		value.Revisions[index].ConfigurationDigest = digest
		digests[revision.ID] = digest
	}
	for portalID, control := range value.VersionControls {
		for index := range control.History {
			if digest, ok := digests[control.History[index].Entry.PublicationID]; ok {
				control.History[index].Entry.ConfigurationDigest = digest
			}
		}
		value.VersionControls[portalID] = control
	}
	return nil
}
