package releaseorchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncDeploymentReferencesPreservesDocumentLayout(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "engineering/deploy/profile.json", "{\n  \"plugins\": [\n    { \"id\": \"cn.vastplan.demo\", \"version\": \"1.0.0\", \"channel\": \"stable\" }\n  ]\n}\n")
	changes, err := SyncDeploymentReferences(root, map[string]string{"cn.vastplan.demo": "1.1.0"})
	if err != nil || len(changes) != 1 || changes[0].Occurrences != 1 {
		t.Fatalf("部署引用同步结果错误: %+v err=%v", changes, err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "engineering/deploy/profile.json"))
	if err != nil || !strings.Contains(string(raw), `{ "id": "cn.vastplan.demo", "version": "1.1.0", "channel": "stable" }`) {
		t.Fatalf("部署文件布局或版本错误: %s err=%v", raw, err)
	}
}

func TestSyncDeploymentReferencesAlsoMovesPortalProfileDigestLocks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "engineering/deploy/portal-platform-catalog.json", `{
  "profiles": [{"version":1,"revision":2,"id":"portal-default","target":{"kernel":"frontend"},"updates":{"mode":"automatic"},"runtimeEngine":{"id":"cn.vastplan.demo","version":"1.0.0","channel":"stable","engineContract":"^1.0.0","family":"react"},"renderAdapter":{"id":"cn.vastplan.render","version":"1.0.0","channel":"stable","uiContract":"^1.0.0"},"shell":{"id":"cn.vastplan.shell","version":"1.0.0","channel":"stable","uiContract":"^1.0.0"},"workbench":{"id":"cn.vastplan.workbench","version":"1.0.0","channel":"stable","uiContract":"^1.0.0"},"plugins":[],"security":{"firstPartyOnly":true,"requireIntegrity":true}}],
  "bindings": [{"platformProfile":{"id":"portal-default","revision":2,"digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}]
}`)
	writeFile(t, root, "engineering/deploy/portal-access-profile-catalog.json", `{"platformProfile":{"id":"portal-default","revision":2,"digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`)
	if _, err := SyncDeploymentReferences(root, map[string]string{"cn.vastplan.demo": "1.1.0"}); err != nil {
		t.Fatal(err)
	}
	platform, _ := os.ReadFile(filepath.Join(root, "engineering/deploy/portal-platform-catalog.json"))
	access, _ := os.ReadFile(filepath.Join(root, "engineering/deploy/portal-access-profile-catalog.json"))
	if strings.Contains(string(platform), strings.Repeat("a", 64)) || strings.Contains(string(access), strings.Repeat("a", 64)) {
		t.Fatalf("Portal Profile 派生摘要未全链条同步:\n%s\n%s", platform, access)
	}
}
