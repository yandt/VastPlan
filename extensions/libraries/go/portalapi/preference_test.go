package portalapi

import (
	"testing"

	"cdsoft.com.cn/VastPlan/contracts/generated/go/contractregistry"
)

func TestValidatePortalPreferenceRejectsOpenEndedValues(t *testing.T) {
	scope := PortalPreferenceScope{
		PortalID:  "operations",
		Renderer:  PreferenceCatalogScope{ID: "cn.vastplan.render", ContractMajor: contractregistry.FrontendUIContractMajor},
		Shell:     PreferenceCatalogScope{ID: "cn.vastplan.shell", ContractMajor: contractregistry.FrontendUIContractMajor},
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
	before := PortalPreferenceValues{RendererID: "arco", Collections: map[string]CollectionPreference{"services": {Columns: []string{"id"}}}}
	after := PortalPreferenceValues{RendererID: "mui", ShellTemplateID: "top-navigation", Collections: map[string]CollectionPreference{"services": {Columns: []string{"id", "name"}}}}
	changed := PortalPreferenceChangedSections(before, after)
	if len(changed) != 3 || changed[0] != "renderer" || changed[1] != "shell" || changed[2] != "workbench" {
		t.Fatalf("unexpected sections: %v", changed)
	}
}
