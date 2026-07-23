package snapshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	provider "cdsoft.com.cn/VastPlan/extensions/sdk/go/artifactassessmentprovider"
)

func TestMaterializerPublishesPinnedSnapshotAndReusesIt(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "staging")
	prepareDatabase(t, source, "database-a")
	revision, err := provider.TrivyDatabaseRevision(source)
	if err != nil {
		t.Fatal(err)
	}
	materializer, err := New(Config{SourceDirectory: source, SnapshotRoot: filepath.Join(root, "materialized"), DatabaseRevision: revision})
	if err != nil {
		t.Fatal(err)
	}
	first, err := materializer.Materialize()
	if err != nil || !first.Ready || first.DatabaseRevision != revision || first.Files != 2 || first.Bytes <= 0 {
		t.Fatalf("首次物化失败: status=%+v err=%v", first, err)
	}
	if err := os.WriteFile(filepath.Join(source, "db", "trivy.db"), []byte("changed staging"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := materializer.Materialize()
	if err != nil || second != first {
		t.Fatalf("已发布 revision 必须按自身字节幂等复核: first=%+v second=%+v err=%v", first, second, err)
	}
	if got, err := provider.TrivyDatabaseRevision(materializer.config.SnapshotDirectory()); err != nil || got != revision {
		t.Fatalf("发布目录摘要无效: got=%s err=%v", got, err)
	}
}

func TestMaterializerRejectsDigestMismatchWithoutPublishing(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "staging")
	prepareDatabase(t, source, "database-a")
	materializer, err := New(Config{SourceDirectory: source, SnapshotRoot: filepath.Join(root, "materialized"), DatabaseRevision: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := materializer.Materialize(); err == nil {
		t.Fatal("候选摘要不符必须拒绝")
	}
	if _, err := os.Lstat(materializer.config.SnapshotDirectory()); !os.IsNotExist(err) {
		t.Fatalf("失败候选不得成为已发布 snapshot: %v", err)
	}
}

func TestMaterializerRejectsSymlinkedDatabaseFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "staging")
	prepareDatabase(t, source, "database-a")
	outside := filepath.Join(root, "outside.db")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(source, "db", "trivy.db")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "db", "trivy.db")); err != nil {
		t.Fatal(err)
	}
	materializer, err := New(Config{SourceDirectory: source, SnapshotRoot: filepath.Join(root, "materialized"), DatabaseRevision: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := materializer.Materialize(); err == nil {
		t.Fatal("符号链接数据库文件必须拒绝")
	}
}

func prepareDatabase(t *testing.T, root, database string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "db"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"metadata.json": []byte(`{"Version":2}`), "trivy.db": []byte(database)} {
		if err := os.WriteFile(filepath.Join(root, "db", name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
