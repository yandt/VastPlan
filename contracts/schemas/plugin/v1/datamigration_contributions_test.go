package pluginv1

import (
	"strings"
	"testing"
)

func TestManifestDataMigrations(t *testing.T) {
	raw := strings.Replace(string(dataModelManifest()), `]}}`, `],"dataMigrations":[{"id":"example.record.v2","contractVersion":"1.0.0","modelId":"example.record","fromVersion":1,"toVersion":2,"path":"data-migrations/record-v2.json","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]}}`, 1)
	manifest, err := ParseManifest([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	references, err := ManifestDataMigrations(manifest)
	if err != nil || len(references) != 1 || references[0].ModelID != "example.record" || references[0].ToVersion != 2 {
		t.Fatalf("DataMigration 引用解析错误: %+v %v", references, err)
	}
}

func TestManifestDataMigrationsRejectsDuplicateEdges(t *testing.T) {
	item := `{"id":"example.record.v2","contractVersion":"1.0.0","modelId":"example.record","fromVersion":1,"toVersion":2,"path":"data-migrations/record-v2.json","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`
	other := strings.Replace(item, "example.record.v2", "example.record.second-v2", 1)
	raw := strings.Replace(string(dataModelManifest()), `]}}`, `],"dataMigrations":[`+item+`,`+other+`]}}`, 1)
	manifest, err := ParseManifest([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ManifestDataMigrations(manifest); err == nil {
		t.Fatal("同模型同版本边的迁移必须拒绝")
	}
}

func TestManifestDataMigrationsRejectsUnknownModel(t *testing.T) {
	item := `{"id":"example.other.v2","contractVersion":"1.0.0","modelId":"example.other","fromVersion":1,"toVersion":2,"path":"data-migrations/other-v2.json","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`
	raw := strings.Replace(string(dataModelManifest()), `]}}`, `],"dataMigrations":[`+item+`]}}`, 1)
	manifest, err := ParseManifest([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ManifestDataMigrations(manifest); err == nil {
		t.Fatal("迁移必须绑定同制品声明的 DataModel")
	}
}
