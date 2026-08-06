package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type retiredStateGovernanceEntry struct {
	Path        string `json:"path"`
	Replacement string `json:"replacement"`
}

func TestQuarantineRetiredDevelopmentStateFiles(t *testing.T) {
	root := t.TempDir()
	for _, item := range retiredDevelopmentStateFiles {
		path := filepath.Join(root, filepath.FromSlash(item.path))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(item.replacement), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Date(2026, 8, 6, 1, 2, 3, 4, time.UTC)
	got, err := quarantineRetiredDevelopmentStateFiles(root, now)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"state/quarantine/retired-local-truth/20260806T010203.000000004Z/state/api-exposure.json",
		"state/quarantine/retired-local-truth/20260806T010203.000000004Z/state/database-connections.json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("隔离结果不符: got=%v want=%v", got, want)
	}
	for index, item := range retiredDevelopmentStateFiles {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(item.path))); !os.IsNotExist(err) {
			t.Fatalf("退役状态文件仍位于活动目录: %s err=%v", item.path, err)
		}
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(want[index])))
		if err != nil || string(raw) != item.replacement {
			t.Fatalf("隔离副本不完整: path=%s raw=%q err=%v", want[index], raw, err)
		}
	}

	again, err := quarantineRetiredDevelopmentStateFiles(root, now.Add(time.Second))
	if err != nil || len(again) != 0 {
		t.Fatalf("重复清理应为空操作: moved=%v err=%v", again, err)
	}
}

func TestQuarantineRetiredDevelopmentStateFilesRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(retiredDevelopmentStateFiles[0].path))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := quarantineRetiredDevelopmentStateFiles(root, time.Now()); err == nil || !strings.Contains(err.Error(), "不是普通文件") {
		t.Fatalf("符号链接必须拒绝: %v", err)
	}
}

func TestRetiredDevelopmentStateRegistryMatchesGovernanceInventory(t *testing.T) {
	inventory := readFirstPartyBoundaryInventoryForPlatformDev(t)
	if len(inventory) != len(retiredDevelopmentStateFiles) {
		t.Fatalf("退役状态清理器与治理清单数量不一致: cleanup=%d governance=%d", len(retiredDevelopmentStateFiles), len(inventory))
	}
	for index, item := range retiredDevelopmentStateFiles {
		if inventory[index].Path != item.path || inventory[index].Replacement != item.replacement {
			t.Fatalf("退役状态清理器与治理清单漂移: cleanup=%+v governance=%+v", item, inventory[index])
		}
	}
}

func readFirstPartyBoundaryInventoryForPlatformDev(t *testing.T) []retiredStateGovernanceEntry {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "engineering", "governance", "first-party-plugin-boundaries.json"))
	if err != nil {
		t.Fatal(err)
	}
	var inventory struct {
		RetiredDevelopmentStateFiles []retiredStateGovernanceEntry `json:"retiredDevelopmentStateFiles"`
	}
	if err := json.Unmarshal(raw, &inventory); err != nil {
		t.Fatal(err)
	}
	return inventory.RetiredDevelopmentStateFiles
}
