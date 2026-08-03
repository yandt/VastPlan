package pluginv1

import (
	"encoding/json"
	"testing"
)

func TestPublicInterfaceFingerprintIgnoresImplementationMetadata(t *testing.T) {
	manifest := fingerprintManifest(t)
	first, err := PublicInterfaceFingerprint(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Version = "1.0.1"
	manifest.Name = "Renamed"
	manifest.Description = "New implementation details"
	manifest.Entry["frontend"] = "frontend/dist/renamed.js"
	second, err := PublicInterfaceFingerprint(manifest)
	if err != nil || first != second {
		t.Fatalf("implementation metadata must not change fingerprint: %s %s err=%v", first, second, err)
	}
}

func TestPublicInterfaceFingerprintChangesWithPublicContribution(t *testing.T) {
	manifest := fingerprintManifest(t)
	first, err := PublicInterfaceFingerprint(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Contributes["frontend"] = json.RawMessage(`{"views":[{"id":"settings","route":"/settings","uiContract":"^10.0.0"}]}`)
	second, err := PublicInterfaceFingerprint(manifest)
	if err != nil || first == second {
		t.Fatalf("public contribution must change fingerprint: %s %s err=%v", first, second, err)
	}
}

func fingerprintManifest(t *testing.T) Manifest {
	t.Helper()
	return Manifest{
		ID: "cn.vastplan.example.fingerprint", Name: "Fingerprint", Description: "test", Version: "1.0.0", Publisher: "vastplan",
		Engines: map[string]string{"frontend": "^1.0"}, Entry: map[string]string{"frontend": "frontend/dist/index.js"},
		Contributes: map[string]json.RawMessage{"frontend": json.RawMessage(`{"views":[{"id":"home","route":"/","uiContract":"^10.0.0"}]}`)},
	}
}
