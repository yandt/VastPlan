package workspacelease

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestStorePersistsLeaseAndExpiresOnlyUnprotectedCandidates(t *testing.T) {
	root, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 24, 1, 0, 0, 0, time.UTC)
	firstRef := pluginv1.ArtifactRef{PluginID: "cn.vastplan.product.first", Version: "1.0.0-dev.1", Channel: "workspace"}
	secondRef := pluginv1.ArtifactRef{PluginID: "cn.vastplan.product.second", Version: "1.0.0-dev.1", Channel: "workspace"}
	first, revision, err := store.Grant(firstRef, strings.Repeat("a", 64), time.Minute, 2, now)
	if err != nil || revision != 1 || first.Token == "" {
		t.Fatalf("签发首个 lease 失败: lease=%+v revision=%d err=%v", first, revision, err)
	}
	if _, _, err := store.Grant(secondRef, strings.Repeat("b", 64), time.Minute, 2, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Grant(pluginv1.ArtifactRef{PluginID: "cn.vastplan.product.third", Version: "1.0.0-dev.1", Channel: "workspace"}, strings.Repeat("c", 64), time.Minute, 2, now); err == nil {
		t.Fatal("workspace 必须执行 Profile 容量上限")
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if persisted, ok := reopened.Active(firstRef, now.Add(30*time.Second)); !ok || persisted.Token != first.Token {
		t.Fatalf("lease 重启后未持久恢复: %+v ok=%t", persisted, ok)
	}
	protected := func(ref pluginv1.ArtifactRef, _ string) bool { return ref == firstRef }
	_, expired, err := reopened.Expire(now.Add(2*time.Minute), protected)
	if err != nil || expired != 1 {
		t.Fatalf("过期必须只清理未引用候选: expired=%d err=%v", expired, err)
	}
	if _, ok := reopened.Active(firstRef, now.Add(2*time.Minute)); ok {
		t.Fatal("已过期 lease 即使受保护也不得继续用于新读取")
	}
	_, expired, err = reopened.Expire(now.Add(2*time.Minute), nil)
	if err != nil || expired != 1 {
		t.Fatalf("引用释放后应清理剩余过期 lease: expired=%d err=%v", expired, err)
	}
}

func TestStoreRejectsImmutableRefDigestDrift(t *testing.T) {
	root, _ := filepath.Abs(t.TempDir())
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	ref := pluginv1.ArtifactRef{PluginID: "cn.vastplan.product.demo", Version: "1.0.0-dev.1", Channel: "workspace"}
	now := time.Now().UTC()
	if _, _, err := store.Grant(ref, strings.Repeat("a", 64), time.Minute, 1, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Grant(ref, strings.Repeat("b", 64), time.Minute, 1, now); err == nil {
		t.Fatal("同一 workspace ref 不得覆盖为其他摘要")
	}
}
