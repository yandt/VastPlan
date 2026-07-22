package pluginv1

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseManifestAuthorizationContract(t *testing.T) {
	manifest, err := ParseManifest(authorizationManifest("cn.vastplan.platform.infrastructure.demo", "platform.demo", "platform.demo.read", "listItems", false))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Authorization == nil || len(manifest.Authorization.Permissions) != 1 || len(manifest.Authorization.OperationGuards) != 1 {
		t.Fatalf("授权声明未解析: %+v", manifest.Authorization)
	}

	invalid := map[string][]byte{
		"高风险离线授权":      authorizationManifest("cn.vastplan.platform.infrastructure.demo", "platform.demo", "platform.demo.read", "listItems", true),
		"外部插件占用平台命名空间": []byte(`{"id":"com.example.demo","name":"demo","description":"demo","version":"1.0.0","publisher":"example","engines":{"backend":"^0.1"},"authorization":{"namespace":"platform.demo","permissions":[{"code":"platform.demo.read","title":"read","scope":"platform","risk":"low","assignable":true,"offlineAllowed":false}],"operationGuards":[{"extensionPoint":"tool.package","capability":"platform.demo","operation":"listItems","permissions":["platform.demo.read"],"access":"read","approval":"none"}]},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[{"id":"platform.demo","service_role":"backend","subcommands":[{"name":"listItems","description":"list"}]}]}}}`),
	}
	for name, raw := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseManifest(raw); err == nil {
				t.Fatal("非法授权声明必须拒绝")
			}
		})
	}
}

func TestAuthorizationGuardMustBindDeclaredOperationAndPermission(t *testing.T) {
	valid := authorizationManifest("cn.vastplan.platform.infrastructure.demo", "platform.demo", "platform.demo.read", "listItems", false)
	for name, raw := range map[string]string{
		"未知操作": strings.ReplaceAll(string(valid), `"operation":"listItems"`, `"operation":"futureOperation"`),
		"未知权限": strings.ReplaceAll(string(valid), `"permissions":["platform.demo.read"]`, `"permissions":["platform.demo.write"]`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(raw)); err == nil {
				t.Fatal("未绑定声明必须拒绝")
			}
		})
	}
}

func TestBuildPermissionCatalogIsDeterministicAndRejectsConflicts(t *testing.T) {
	first, err := ParseManifest(authorizationManifest("cn.vastplan.platform.infrastructure.first", "platform.first", "platform.first.read", "listItems", false))
	if err != nil {
		t.Fatal(err)
	}
	second, err := ParseManifest(authorizationManifest("cn.vastplan.platform.infrastructure.second", "platform.second", "platform.second.write", "updateItem", false))
	if err != nil {
		t.Fatal(err)
	}
	a := strings.Repeat("a", 64)
	b := strings.Repeat("b", 64)
	left, err := BuildPermissionCatalog([]PermissionCatalogSource{{Manifest: first, ArtifactSHA256: a}, {Manifest: second, ArtifactSHA256: b}})
	if err != nil {
		t.Fatal(err)
	}
	right, err := BuildPermissionCatalog([]PermissionCatalogSource{{Manifest: second, ArtifactSHA256: b}, {Manifest: first, ArtifactSHA256: a}})
	if err != nil {
		t.Fatal(err)
	}
	if left.Digest == "" || left.Digest != right.Digest || len(left.Permissions) != 2 || len(left.Operations) != 2 {
		t.Fatalf("权限目录必须确定且完整: left=%+v right=%+v", left, right)
	}
	recomputed, err := PermissionCatalogDigest(left)
	if err != nil || recomputed != left.Digest {
		t.Fatalf("权限目录 digest 必须可独立复核: digest=%s recomputed=%s err=%v", left.Digest, recomputed, err)
	}
	if _, err := BuildPermissionCatalog([]PermissionCatalogSource{{Manifest: first, ArtifactSHA256: a}, {Manifest: first, ArtifactSHA256: b}}); err == nil {
		t.Fatal("权限代码或操作重复所有者必须拒绝")
	}
	raw, err := json.Marshal(left)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParsePermissionCatalog(raw)
	if err != nil || parsed.Digest != left.Digest {
		t.Fatalf("权限目录必须可严格复核: parsed=%+v err=%v", parsed, err)
	}
	tampered := strings.Replace(string(raw), left.Digest, strings.Repeat("f", 64), 1)
	if _, err := ParsePermissionCatalog([]byte(tampered)); err == nil {
		t.Fatal("被篡改的权限目录必须拒绝")
	}
}

func TestBuildPermissionCatalogFromSystemManagementPlugins(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "extensions", "plugins")
	plugins := []string{
		"cn.vastplan.platform.artifacts.repository",
		"cn.vastplan.platform.infrastructure.deployment-manager",
	}
	sources := make([]PermissionCatalogSource, 0, len(plugins))
	for index, pluginID := range plugins {
		raw, err := os.ReadFile(filepath.Join(root, pluginID, "vastplan.plugin.json"))
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := ParseManifest(raw)
		if err != nil {
			t.Fatalf("%s 授权清单无效: %v", pluginID, err)
		}
		digestByte := byte('a' + index)
		sources = append(sources, PermissionCatalogSource{Manifest: manifest, ArtifactSHA256: strings.Repeat(string(digestByte), 64)})
	}
	catalog, err := BuildPermissionCatalog(sources)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.SchemaVersion != PermissionCatalogSchemaVersion || len(catalog.Permissions) != 11 || len(catalog.Operations) != 37 || len(catalog.Digest) != 64 {
		t.Fatalf("系统管理权限目录不完整: permissions=%d operations=%d digest=%s", len(catalog.Permissions), len(catalog.Operations), catalog.Digest)
	}
}

func authorizationManifest(id, namespace, permission, operation string, offline bool) []byte {
	return []byte(`{"id":"` + id + `","name":"demo","description":"demo","version":"1.0.0","publisher":"vastplan","engines":{"backend":"^0.1"},"authorization":{"namespace":"` + namespace + `","permissions":[{"code":"` + permission + `","title":"permission","scope":"platform","risk":"high","assignable":true,"offlineAllowed":` + boolJSON(offline) + `}],"operationGuards":[{"extensionPoint":"tool.package","capability":"` + namespace + `","operation":"` + operation + `","permissions":["` + permission + `"],"access":"read","approval":"none"}]},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[{"id":"` + namespace + `","service_role":"backend","subcommands":[{"name":"` + operation + `","description":"operation"}]}]}}}`)
}

func boolJSON(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
