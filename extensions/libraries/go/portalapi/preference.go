package portalapi

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
)

const PreferenceCapability = "platform.portal-preference"

var (
	ErrPreferenceConflict = errors.New("PortalPreference revision 冲突")
	preferenceIDPattern   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$`)
)

type PreferenceCatalogScope struct {
	ID            string `json:"id"`
	ContractMajor uint32 `json:"contractMajor"`
}

type PortalPreferenceScope struct {
	PortalID  string                 `json:"portalId"`
	Workbench PreferenceCatalogScope `json:"workbench"`
}

type CollectionPreference struct {
	Columns       []string `json:"columns,omitempty"`
	HiddenColumns []string `json:"hiddenColumns,omitempty"`
	Density       string   `json:"density,omitempty"`
	PageSize      uint32   `json:"pageSize,omitempty"`
}

type PortalPreferenceValues struct {
	Collections map[string]CollectionPreference `json:"collections,omitempty"`
}

type PortalPreference struct {
	Revision  uint64                 `json:"revision"`
	Scope     PortalPreferenceScope  `json:"scope"`
	Values    PortalPreferenceValues `json:"values"`
	UpdatedAt string                 `json:"updatedAt,omitempty"`
}

type GetPortalPreferenceRequest struct {
	Scope PortalPreferenceScope `json:"scope"`
}

type PutPortalPreferenceRequest struct {
	Scope            PortalPreferenceScope  `json:"scope"`
	ExpectedRevision uint64                 `json:"expectedRevision"`
	Values           PortalPreferenceValues `json:"values"`
}

func ValidatePortalPreferenceScope(scope PortalPreferenceScope) error {
	if !preferenceIDPattern.MatchString(scope.PortalID) {
		return errors.New("PortalPreference portalId 无效")
	}
	for label, catalog := range map[string]PreferenceCatalogScope{
		"workbench": scope.Workbench,
	} {
		if !preferenceIDPattern.MatchString(catalog.ID) || catalog.ContractMajor == 0 || catalog.ContractMajor > 65535 {
			return fmt.Errorf("PortalPreference %s catalog scope 无效", label)
		}
	}
	return nil
}

func ValidatePortalPreferenceValues(values PortalPreferenceValues) error {
	if len(values.Collections) > 128 {
		return errors.New("PortalPreference collections 超过上限")
	}
	for collectionID, preference := range values.Collections {
		if !preferenceIDPattern.MatchString(collectionID) {
			return errors.New("PortalPreference collection ID 无效")
		}
		if len(preference.Columns) > 128 {
			return errors.New("PortalPreference collection columns 超过上限")
		}
		seen := map[string]struct{}{}
		for _, column := range preference.Columns {
			if !preferenceIDPattern.MatchString(column) {
				return errors.New("PortalPreference column ID 无效")
			}
			if _, duplicate := seen[column]; duplicate {
				return errors.New("PortalPreference column ID 重复")
			}
			seen[column] = struct{}{}
		}
		if len(preference.HiddenColumns) > 128 {
			return errors.New("PortalPreference collection hiddenColumns 超过上限")
		}
		hidden := map[string]struct{}{}
		for _, column := range preference.HiddenColumns {
			if !preferenceIDPattern.MatchString(column) {
				return errors.New("PortalPreference hidden column ID 无效")
			}
			if _, duplicate := hidden[column]; duplicate {
				return errors.New("PortalPreference hidden column ID 重复")
			}
			hidden[column] = struct{}{}
		}
		if preference.Density != "" && preference.Density != "compact" && preference.Density != "standard" && preference.Density != "comfortable" {
			return errors.New("PortalPreference collection density 无效")
		}
		if preference.PageSize > 1000 {
			return errors.New("PortalPreference collection pageSize 超过上限")
		}
	}
	return nil
}

func PortalPreferenceScopeKey(scope PortalPreferenceScope) string {
	return fmt.Sprintf("%s\x00%s\x00%d",
		scope.PortalID,
		scope.Workbench.ID, scope.Workbench.ContractMajor,
	)
}

func PortalPreferenceChangedSections(before, after PortalPreferenceValues) []string {
	sections := make([]string, 0, 1)
	if !collectionsEqual(before.Collections, after.Collections) {
		sections = append(sections, "workbench")
	}
	sort.Strings(sections)
	return sections
}

func collectionsEqual(left, right map[string]CollectionPreference) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		other, ok := right[key]
		if !ok || value.Density != other.Density || value.PageSize != other.PageSize || len(value.Columns) != len(other.Columns) || len(value.HiddenColumns) != len(other.HiddenColumns) {
			return false
		}
		for index := range value.Columns {
			if value.Columns[index] != other.Columns[index] {
				return false
			}
		}
		for index := range value.HiddenColumns {
			if value.HiddenColumns[index] != other.HiddenColumns[index] {
				return false
			}
		}
	}
	return true
}
