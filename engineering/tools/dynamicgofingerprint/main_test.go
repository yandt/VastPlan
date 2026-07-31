package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCalculateUsesOnlyActualDependencyClosureAndBuildIdentity(t *testing.T) {
	root := t.TempDir()
	for name, value := range map[string]string{
		"go.mod":          "module example.test\n\ngo 1.24\n",
		"go.sum":          "",
		"shared/value.go": "package shared\nconst Value = 1\n",
		"host/main.go":    "package main\nimport _ \"example.test/shared\"\nfunc main() {}\n",
		"plugin/main.go":  "package main\nimport _ \"example.test/shared\"\nfunc main() {}\n",
		"unrelated.txt":   "first\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	packages := []string{"./host", "./plugin"}
	files, err := localDependencyFiles(root, "", packages)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["shared/value.go"]; !ok {
		t.Fatalf("共享依赖没有进入 ABI 闭包: %v", files)
	}
	first, err := calculateForPackages(root, "", packages)
	if err != nil {
		t.Fatal(err)
	}
	second, err := calculateForPackages(root, "", packages)
	if err != nil || first != second || len(first) != 64 {
		t.Fatalf("同一依赖闭包必须得到稳定 SHA-256: first=%s second=%s err=%v", first, second, err)
	}
	if err := os.WriteFile(filepath.Join(root, "unrelated.txt"), []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unrelated, err := calculateForPackages(root, "", packages)
	if err != nil || unrelated != first {
		t.Fatalf("无关文件不得改变 ABI 指纹: first=%s unrelated=%s err=%v", first, unrelated, err)
	}
	if err := os.WriteFile(filepath.Join(root, "shared", "value.go"), []byte("package shared\nconst Value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := calculateForPackages(root, "", packages)
	if err != nil || changed == first {
		t.Fatalf("共享依赖变化必须改变 ABI 指纹: first=%s changed=%s err=%v", first, changed, err)
	}
	withTags, err := calculateForPackages(root, "enterprise", packages)
	if err != nil || withTags == changed {
		t.Fatalf("build tags 必须进入指纹: base=%s tagged=%s err=%v", changed, withTags, err)
	}
}
