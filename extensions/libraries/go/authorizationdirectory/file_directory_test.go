package authorizationdirectory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileDirectoryLoadsStrictProjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groups.json")
	raw := []byte(`{"version":1,"revision":7,"subjects":{"stable.alice":[{"id":"ops","issuer":"https://id.example"}]}}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	groups, revision, err := (FileDirectory{Path: path}).Groups("stable.alice")
	if err != nil || revision != 7 || len(groups) != 1 || groups[0].ID != "ops" {
		t.Fatalf("投影加载错误: groups=%+v revision=%d err=%v", groups, revision, err)
	}
	if _, err := Parse([]byte(`{"version":1,"revision":7,"subjects":{},"unknown":true}`)); err == nil {
		t.Fatal("未知字段必须拒绝")
	}
}
