package pluginservice

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactRefUsesStableLowerCamelJSON(t *testing.T) {
	raw, err := json.Marshal(Ref{PluginID: "com.example.plugin", Version: "1.0.0", Channel: "testing"})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"pluginId":"com.example.plugin","version":"1.0.0","channel":"testing"}` {
		t.Fatalf("ArtifactRef JSON 字段名不是稳定 lowerCamel: %s", raw)
	}
}

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

func TestRepositoryListRefsExposesOnlyExactValidatedReferences(t *testing.T) {
	packageBytes, manifest, err := PackageDirectory(writeTestPlugin(t))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := NewRepository(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	if refs, err := repo.ListRefs(); err != nil || len(refs) != 0 {
		t.Fatalf("空仓库应返回空引用: refs=%#v err=%v", refs, err)
	}
	for _, channel := range []string{"testing", "stable"} {
		if _, err := repo.Publish(channel, packageBytes); err != nil {
			t.Fatal(err)
		}
	}
	refs, err := repo.ListRefs()
	if err != nil {
		t.Fatal(err)
	}
	want := []Ref{
		{PluginID: manifest.ID, Version: manifest.Version, Channel: "stable"},
		{PluginID: manifest.ID, Version: manifest.Version, Channel: "testing"},
	}
	if len(refs) != len(want) || refs[0] != want[0] || refs[1] != want[1] {
		t.Fatalf("引用必须稳定排序且不泄露路径: got=%#v want=%#v", refs, want)
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
	if metadata, err := repo.ReadMetadata(Ref{PluginID: manifest.ID, Version: manifest.Version, Channel: "stable"}); err != nil || metadata.SHA256 != artifact.SHA256 {
		t.Fatalf("元数据路径不应为 Catalog 重读大对象: metadata=%#v err=%v", metadata, err)
	}
	if _, _, err := repo.Read(Ref{PluginID: manifest.ID, Version: manifest.Version, Channel: "stable"}); err == nil {
		t.Fatal("对象 SHA 不匹配必须 fail-closed")
	}
}

func TestRepositoryArtifactRefAcceptsFirstPartyNamespace(t *testing.T) {
	repo, err := NewRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []Ref{
		{PluginID: "cn.vastplan.platform.data.database", Version: "1.0.0", Channel: "stable"},
		{PluginID: "com.example.integration.reader", Version: "1.0.0", Channel: "stable"},
	} {
		if _, err := repo.artifactDir(ref); err != nil {
			t.Fatalf("合法制品引用 %s 应通过: %v", ref.PluginID, err)
		}
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

func TestPackageDirectory_RequiresDeclaredLicenseFile(t *testing.T) {
	dir := writeTestPlugin(t)
	manifestPath := filepath.Join(dir, manifestName)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte(`"publisher":"example",`), []byte(`"publisher":"example","license":"Apache-2.0","licenseFile":"LICENSE","noticeFile":"NOTICE",`), 1)
	if err := os.WriteFile(manifestPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := PackageDirectory(dir); err == nil {
		t.Fatal("声明许可证但未携带许可证文本的插件必须拒绝打包")
	}
	if err := os.WriteFile(filepath.Join(dir, "LICENSE"), []byte("Apache License 2.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := PackageDirectory(dir); err == nil {
		t.Fatal("声明归属告示但未携带 NOTICE 的插件必须拒绝打包")
	}
	if err := os.WriteFile(filepath.Join(dir, "NOTICE"), []byte("Copyright example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	packageBytes, _, err := PackageDirectory(dir)
	if err != nil {
		t.Fatalf("携带许可证文本后应可打包: %v", err)
	}
	if _, _, err := inspectPackage(packageBytes); err != nil {
		t.Fatalf("制品读取必须接受唯一、非空的许可证文本: %v", err)
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
