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
