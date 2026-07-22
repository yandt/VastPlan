package portalcomposer

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

func TestPreferenceStorePersistsCASAndIsolatesSubjects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "portal-preferences.json")
	store, err := openPreferenceStore(path)
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.Date(2026, 7, 23, 1, 2, 3, 0, time.UTC) }
	scope := testPreferenceScope()
	alice := portalapi.Principal{ID: "alice", TenantID: "tenant-a"}
	bob := portalapi.Principal{ID: "bob", TenantID: "tenant-a"}
	created, err := store.Put(alice, portalapi.PutPortalPreferenceRequest{
		Scope: scope, ExpectedRevision: 0,
		Values: portalapi.PortalPreferenceValues{RendererID: "mui", Collections: map[string]portalapi.CollectionPreference{"services": {Columns: []string{"name", "id"}, Density: "compact", PageSize: 20}}},
	})
	if err != nil || created.Revision != 1 || created.UpdatedAt == "" {
		t.Fatalf("create failed: value=%+v err=%v", created, err)
	}
	if _, err := store.Put(alice, portalapi.PutPortalPreferenceRequest{Scope: scope, ExpectedRevision: 0, Values: portalapi.PortalPreferenceValues{RendererID: "arco"}}); !errors.Is(err, portalapi.ErrPreferenceConflict) {
		t.Fatalf("stale CAS must conflict: %v", err)
	}
	missing, err := store.Get(bob, scope)
	if err != nil || missing.Revision != 0 || missing.Values.RendererID != "" {
		t.Fatalf("subject isolation failed: value=%+v err=%v", missing, err)
	}
	reopened, err := openPreferenceStore(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.Get(alice, scope)
	if err != nil || loaded.Revision != 1 || loaded.Values.RendererID != "mui" || loaded.Values.Collections["services"].Density != "compact" {
		t.Fatalf("persisted preference mismatch: value=%+v err=%v", loaded, err)
	}
}

func TestPreferenceStoreTreatsRepeatedWriteAsIdempotent(t *testing.T) {
	store, err := openPreferenceStore(filepath.Join(t.TempDir(), "portal-preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	principal := portalapi.Principal{ID: "alice", TenantID: "tenant-a"}
	request := portalapi.PutPortalPreferenceRequest{Scope: testPreferenceScope(), Values: portalapi.PortalPreferenceValues{ShellTemplateID: "standard"}}
	first, err := store.Put(principal, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Put(principal, request)
	if err != nil || second.Revision != first.Revision {
		t.Fatalf("replayed write should return current revision: value=%+v err=%v", second, err)
	}
}

func testPreferenceScope() portalapi.PortalPreferenceScope {
	return portalapi.PortalPreferenceScope{
		PortalID:  "operations",
		Renderer:  portalapi.PreferenceCatalogScope{ID: "cn.vastplan.render", ContractMajor: 4},
		Shell:     portalapi.PreferenceCatalogScope{ID: "cn.vastplan.shell", ContractMajor: 4},
		Workbench: portalapi.PreferenceCatalogScope{ID: "cn.vastplan.workbench", ContractMajor: 4},
	}
}
