package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestAppendPublishedAPIExposureCatalogSkipsUnpublishedCatalog(t *testing.T) {
	base := []string{"portal-host.cjs", "--listen", "127.0.0.1:18444"}
	got, err := appendPublishedAPIExposureCatalog(base, filepath.Join(t.TempDir(), "api-exposure-gateway.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, base) {
		t.Fatalf("未发布目录不得成为 Portal 启动依赖: %v", got)
	}
}

func TestAppendPublishedAPIExposureCatalogKeepsStrictPublishedInput(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "api-exposure-gateway.json")
	if err := os.WriteFile(filename, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := appendPublishedAPIExposureCatalog([]string{"portal-host.cjs"}, filename)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"portal-host.cjs", "--api-exposure-catalog", filename}
	if !slices.Equal(got, want) {
		t.Fatalf("已发布目录必须继续交给 Portal 严格校验: got=%v want=%v", got, want)
	}
}
