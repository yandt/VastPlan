package pluginv1

import (
	"encoding/json"
	"testing"
)

func FuzzParseManifestNeverPanics(f *testing.F) {
	f.Add([]byte(`{"id":"com.acme.demo","name":"demo","description":"demo","version":"1.0.0","publisher":"acme","engines":{"backend":">=0.1.0"},"activation":[],"entry":{"backend":"bin/demo"},"contributes":{}}`))
	f.Add([]byte(`{"unknown":true}`))
	f.Add([]byte{0xff, 0x00, '{'})
	f.Fuzz(func(t *testing.T, raw []byte) {
		manifest, err := ParseManifest(raw)
		if err != nil {
			return
		}
		encoded, err := json.Marshal(manifest)
		if err != nil {
			t.Fatalf("有效清单无法重编码: %v", err)
		}
		if _, err := ParseManifest(encoded); err != nil {
			t.Fatalf("解析成功的清单重编码后不再满足 Schema: %v", err)
		}
	})
}

func FuzzValidateDescriptorNeverPanics(f *testing.F) {
	f.Add("tool.package", []byte(`{"operations":[{"name":"run"}]}`))
	f.Add("unknown.point", []byte(`{}`))
	f.Fuzz(func(t *testing.T, extensionPoint string, raw []byte) { _ = ValidateDescriptor(extensionPoint, raw) })
}
