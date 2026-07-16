package pluginservice

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRepository_PublishReadAndVerify(t *testing.T) {
	pluginDir := writeTestPlugin(t)
	packageBytes, manifest, err := PackageDirectory(pluginDir)
	if err != nil {
		t.Fatalf("打包测试插件失败: %v", err)
	}
	repo, err := NewRepository(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("创建制品仓库失败: %v", err)
	}
	published, err := repo.Publish("stable", packageBytes)
	if err != nil {
		t.Fatalf("发布制品失败: %v", err)
	}
	if published.PluginID != manifest.ID || published.Version != manifest.Version || published.SHA256 == "" {
		t.Fatalf("发布元数据不正确: %+v", published)
	}

	got, downloaded, err := repo.Read(Ref{PluginID: manifest.ID, Version: manifest.Version, Channel: "stable"})
	if err != nil {
		t.Fatalf("读取已发布制品失败: %v", err)
	}
	if got.SHA256 != published.SHA256 || !bytes.Equal(downloaded, packageBytes) {
		t.Fatal("读取的制品必须与发布字节和索引哈希完全一致")
	}

	if _, err := repo.Publish("stable", packageBytes); err != nil {
		t.Fatalf("相同制品重传应幂等: %v", err)
	}
	if _, err := repo.Publish("stable", append(append([]byte{}, packageBytes...), 0)); err == nil {
		t.Fatal("同一版本不同 SHA 必须被拒绝")
	}
}

func TestRepository_ReadFailsClosedOnCorruption(t *testing.T) {
	pluginDir := writeTestPlugin(t)
	packageBytes, manifest, err := PackageDirectory(pluginDir)
	if err != nil {
		t.Fatalf("打包测试插件失败: %v", err)
	}
	root := filepath.Join(t.TempDir(), "repository")
	repo, err := NewRepository(root)
	if err != nil {
		t.Fatalf("创建制品仓库失败: %v", err)
	}
	artifact, err := repo.Publish("stable", packageBytes)
	if err != nil {
		t.Fatalf("发布制品失败: %v", err)
	}
	object := filepath.Join(root, "artifacts", manifest.ID, manifest.Version, "stable", artifact.Object)
	if err := os.WriteFile(object, []byte("tampered"), 0o644); err != nil {
		t.Fatalf("篡改测试对象失败: %v", err)
	}
	if _, _, err := repo.Read(Ref{PluginID: manifest.ID, Version: manifest.Version, Channel: "stable"}); err == nil {
		t.Fatal("对象 SHA 不匹配必须 fail-closed")
	}
}

func TestRepository_PublishRejectsPackageWithoutManifest(t *testing.T) {
	repo, err := NewRepository(t.TempDir())
	if err != nil {
		t.Fatalf("创建制品仓库失败: %v", err)
	}
	if _, err := repo.Publish("stable", []byte("not a tarball")); err == nil {
		t.Fatal("没有合法根清单的包必须被拒绝")
	}
}

func writeTestPlugin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	manifest := `{
  "id":"com.example.package-test",
  "name":"Package Test",
  "description":"本地制品仓库测试插件",
  "version":"1.2.3",
  "publisher":"example",
  "engines":{"backend":"^0.1"},
  "activation":["onStartup"],
  "entry":{"backend":"backend/main"},
  "contributes":{"backend":{"tools":[{"id":"example.package-test","service_role":"backend","title":"测试工具","subcommands":[{"name":"run","description":"run"}]}]}}
}`
	if err := os.WriteFile(filepath.Join(dir, manifestName), []byte(manifest), 0o644); err != nil {
		t.Fatalf("写入测试清单失败: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "backend"), 0o755); err != nil {
		t.Fatalf("创建测试入口目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "backend", "main"), []byte("binary"), 0o755); err != nil {
		t.Fatalf("写入测试入口失败: %v", err)
	}
	return dir
}
