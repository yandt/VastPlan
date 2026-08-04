package pluginv1

import (
	"strings"
	"testing"
)

func TestManifestDataModels(t *testing.T) {
	manifest, err := ParseManifest(dataModelManifest())
	if err != nil {
		t.Fatal(err)
	}
	references, err := ManifestDataModels(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(references) != 1 || references[0].ID != "example.record" || references[0].ContractVersion != "1.0.0" {
		t.Fatalf("DataModel 引用解析错误: %+v", references)
	}
}

func TestManifestDataModelsRejectsDuplicates(t *testing.T) {
	raw := strings.Replace(string(dataModelManifest()), `]}}`, `,{"id":"example.record","contractVersion":"1.0.0","path":"data-models/other.json","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]}}`, 1)
	manifest, err := ParseManifest([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ManifestDataModels(manifest); err == nil {
		t.Fatal("同一插件内重复 DataModel ID 必须拒绝")
	}
}

func dataModelManifest() []byte {
	return []byte(`{
  "id":"com.example.data","name":"data","description":"data","version":"1.0.0","publisher":"example",
  "engines":{"backend":"^1.0"},"activation":["onStartup"],"entry":{"backend":"backend/main"},
  "contributes":{"backend":{"dataModels":[{"id":"example.record","contractVersion":"1.0.0","path":"data-models/record.json","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}}
}`)
}
