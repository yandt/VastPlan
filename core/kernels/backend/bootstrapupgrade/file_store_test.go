package bootstrapupgrade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/bootstrapinventory"
)

func TestFileInventoryStoreAtomicCASAndExactRetry(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "inventory.json")
	initial := initialInventory(t)
	writeInventory(t, path, initial)
	store := FileInventoryStore{Path: path}
	next := initial
	next.Generation++
	next.Seed = append(next.Seed, bootstrapItem("2.0.0", strings.Repeat("2", 64)))
	updated, err := store.Update(initial.Generation, next)
	if err != nil {
		t.Fatal(err)
	}
	if repeated, err := store.Update(initial.Generation, next); err != nil || repeated.Generation != updated.Generation {
		t.Fatalf("已提交更新的精确重试必须成功: %+v err=%v", repeated, err)
	}
	conflict := next
	conflict.Generation++
	if _, err := store.Update(initial.Generation, conflict); err == nil {
		t.Fatal("过期 generation 必须 CAS 失败")
	}
}

func writeInventory(t *testing.T, path string, value bootstrapinventory.Inventory) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
