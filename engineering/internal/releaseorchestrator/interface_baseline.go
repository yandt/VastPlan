package releaseorchestrator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

// InterfaceBaseline is the trusted active-generation input for public surface
// comparison. Production controllers obtain it through their active Inventory
// port; the engineering CLI is only a file adapter for local development.
type InterfaceBaseline struct {
	Source    string
	Inventory pluginv1.PluginInventorySnapshot
}

func loadInterfaceBaseline(repositoryRoot string, spec ReleaseSpec) (*InterfaceBaseline, error) {
	path := strings.TrimSpace(spec.BaselineInventory)
	if path == "" {
		if spec.Mode == ReleaseModeProduction {
			return nil, errors.New("生产 Release Spec 必须由发布控制器注入活动 Plugin Inventory 基线")
		}
		return nil, nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(repositoryRoot, path)
	}
	baseline, err := LoadInterfaceBaselineFile(path)
	if err != nil {
		return nil, err
	}
	return baseline, nil
}

// LoadInterfaceBaselineFile is the development adapter. It rejects unknown
// fields and validates the snapshot digest before exposing it to release
// policy, so a hand-edited JSON file cannot silently become an authority.
func LoadInterfaceBaselineFile(path string) (*InterfaceBaseline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取活动 Plugin Inventory 基线: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var inventory pluginv1.PluginInventorySnapshot
	if err := decoder.Decode(&inventory); err != nil {
		return nil, fmt.Errorf("解析活动 Plugin Inventory 基线: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("活动 Plugin Inventory 基线只能包含一个 JSON 对象")
	}
	if err := pluginv1.ValidatePluginInventory(inventory); err != nil {
		return nil, fmt.Errorf("验证活动 Plugin Inventory 基线: %w", err)
	}
	return &InterfaceBaseline{Source: path, Inventory: inventory}, nil
}
