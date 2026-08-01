package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	seedaccess "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.seed-access/seedaccess"
)

func TestPrintStateHumanOutput(t *testing.T) {
	var output bytes.Buffer
	writeState(&output, seedaccess.State{Version: 1, Generation: 3, Phase: seedaccess.SeedActive, UpdatedAt: time.Date(2026, 8, 1, 3, 37, 52, 0, time.UTC)}, "human", "初始化成功")
	for _, expected := range []string{"✓ Seed 管理员初始化成功", "generation: 3", "phase: seed-active", "updatedAt: 2026-08-01T03:37:52Z"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("friendly output missing %q: %s", expected, output.String())
		}
	}
}

func TestWriteStateKeepsJSONDefaultForAutomation(t *testing.T) {
	var output bytes.Buffer
	writeState(&output, seedaccess.State{Version: 1, Generation: 2, Phase: seedaccess.SeedActive, UpdatedAt: time.Date(2026, 8, 1, 3, 37, 52, 0, time.UTC)}, "json", "ignored")
	var document struct {
		Version    int    `json:"version"`
		Generation uint64 `json:"generation"`
		Phase      string `json:"phase"`
	}
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.Version != 1 || document.Generation != 2 || document.Phase != "seed-active" {
		t.Fatalf("unexpected JSON projection: %+v", document)
	}
}
