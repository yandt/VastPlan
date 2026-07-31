package portalapi

import (
	"testing"

	"cdsoft.com.cn/VastPlan/contracts/generated/go/contractregistry"
)

func TestValidatePortalPreferenceRejectsOpenEndedValues(t *testing.T) {
	scope := PortalPreferenceScope{
		PortalID:  "operations",
		Workbench: PreferenceCatalogScope{ID: "cn.vastplan.workbench", ContractMajor: contractregistry.FrontendUIContractMajor},
	}
	if err := ValidatePortalPreferenceScope(scope); err != nil {
		t.Fatalf("valid scope rejected: %v", err)
	}
	if err := ValidatePortalPreferenceValues(PortalPreferenceValues{
		Collections: map[string]CollectionPreference{"services": {Density: "tiny"}},
	}); err == nil {
		t.Fatal("unknown density must be rejected")
	}
}

func TestPortalPreferenceChangedSectionsAreStable(t *testing.T) {
	before := PortalPreferenceValues{Collections: map[string]CollectionPreference{"services": {Columns: []string{"id"}}}}
	after := PortalPreferenceValues{Collections: map[string]CollectionPreference{"services": {Columns: []string{"id", "name"}}}}
	changed := PortalPreferenceChangedSections(before, after)
	if len(changed) != 1 || changed[0] != "workbench" {
		t.Fatalf("unexpected sections: %v", changed)
	}
}
