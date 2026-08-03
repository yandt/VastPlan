package portalcomposer

import (
	"bytes"
	"testing"
)

func TestNormalizeLegacyComposerNavigationOnlyRemovesShellOwnedFields(t *testing.T) {
	raw := []byte(`{
  "profiles":[{
    "profile":{
      "shell":{
        "id":"cn.vastplan.foundation.frontend.structure.shell",
        "uiContract":"^9.0.0",
        "config":{
          "navigationGroups":[{"id":"legacy"}],
          "navigationPlacements":[{"semanticID":"legacy"}],
          "navigationOverrides":[{"target":"cn.vastplan.test/main","hidden":true}]
        }
      }
    }
  }]
}`)
	normalized, migrated, err := normalizeLegacyComposerNavigation(raw)
	if err != nil || !migrated {
		t.Fatalf("v4 导航字段必须迁移: migrated=%v err=%v", migrated, err)
	}
	if bytes.Contains(normalized, []byte("navigationGroups")) || bytes.Contains(normalized, []byte("navigationPlacements")) {
		t.Fatalf("旧 Shell 导航字段仍存在: %s", normalized)
	}
	if !bytes.Contains(normalized, []byte("navigationOverrides")) {
		t.Fatalf("Portal 覆盖不得被迁移删除: %s", normalized)
	}
}

func TestComposerStateDataFormatSeparatesMigrationFromCorruption(t *testing.T) {
	legacy, err := composerStateDataFormat([]byte(`{"applications":[]}`))
	if err != nil || legacy != 0 {
		t.Fatalf("无格式标记的历史数据必须进入迁移: version=%d err=%v", legacy, err)
	}
	current, err := composerStateDataFormat([]byte(`{"dataFormatVersion":5}`))
	if err != nil || current != composerDataFormatV5 {
		t.Fatalf("v5 数据不得重复执行兼容清理: version=%d err=%v", current, err)
	}
}
