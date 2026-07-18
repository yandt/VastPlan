package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCalculateIsDeterministicAndBindsBuildTags(t *testing.T) {
	root := t.TempDir()
	for name, value := range map[string]string{"go.mod": "module example.test\n", "go.sum": ""} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	first, err := calculate(root, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := calculate(root, "")
	if err != nil || first != second || len(first) != 64 {
		t.Fatalf("同一输入必须得到稳定 SHA-256: first=%s second=%s err=%v", first, second, err)
	}
	withTags, err := calculate(root, "enterprise")
	if err != nil || withTags == first {
		t.Fatalf("build tags 必须进入指纹: base=%s tagged=%s err=%v", first, withTags, err)
	}
}
