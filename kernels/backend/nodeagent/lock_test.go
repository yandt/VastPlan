package nodeagent

import (
	"path/filepath"
	"testing"
)

func TestProcessLockRejectsSecondOwnerAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node-agent.lock")
	first, err := AcquireProcessLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireProcessLock(path); err == nil {
		t.Fatal("同一路径不应允许第二个 Node Agent 持锁")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := AcquireProcessLock(path)
	if err != nil {
		t.Fatalf("释放后应可重新获取锁: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}
