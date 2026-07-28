// Package releaseorchestrator coordinates contract and plugin release facts
// outside the kernels. It may generate repository files and development
// candidates, but it never participates in kernel startup.
package releaseorchestrator

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"
)

const (
	ContractRegistryPath = "contracts/registry.yaml"
	FrontendUIContractID = "frontend.ui"
)

var (
	contractIDPattern        = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$`)
	uiContractJSONPattern    = regexp.MustCompile(`("uiContract"\s*:\s*")([^"]+)(")`)
	hardcodedUIExportPattern = regexp.MustCompile(`uiContract\s*:\s*"[0-9]+\.[0-9]+\.[0-9]+"`)
)

type ContractRegistry struct {
	SchemaVersion int                           `yaml:"schemaVersion"`
	Contracts     map[string]ContractDefinition `yaml:"contracts"`
}

type ContractDefinition struct {
	Version       string `yaml:"version"`
	Compatibility string `yaml:"compatibility"`
	Description   string `yaml:"description"`
}

type DerivedChange struct {
	Path   string
	Reason string
}

func LoadContractRegistry(repositoryRoot string) (ContractRegistry, error) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(ContractRegistryPath)))
	if err != nil {
		return ContractRegistry{}, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	var registry ContractRegistry
	if err := decoder.Decode(&registry); err != nil {
		return ContractRegistry{}, fmt.Errorf("解析 Contract Registry: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ContractRegistry{}, errors.New("Contract Registry 包含多余 YAML 文档")
	}
	if registry.SchemaVersion != 1 || len(registry.Contracts) == 0 {
		return ContractRegistry{}, errors.New("Contract Registry schemaVersion 或 contracts 无效")
	}
	for id, definition := range registry.Contracts {
		if !contractIDPattern.MatchString(id) || strings.TrimSpace(definition.Description) == "" {
			return ContractRegistry{}, fmt.Errorf("Contract Registry 条目 %q 身份或描述无效", id)
		}
		version, err := semver.StrictNewVersion(definition.Version)
		if err != nil || version.Prerelease() != "" || version.Metadata() != "" {
			return ContractRegistry{}, fmt.Errorf("Contract Registry 条目 %s 必须使用稳定严格 SemVer", id)
		}
		constraint, err := semver.NewConstraint(definition.Compatibility)
		if err != nil || !constraint.Check(version) {
			return ContractRegistry{}, fmt.Errorf("Contract Registry 条目 %s 的 compatibility 不接受自身版本", id)
		}
	}
	if _, ok := registry.Contracts[FrontendUIContractID]; !ok {
		return ContractRegistry{}, errors.New("Contract Registry 缺少 frontend.ui")
	}
	return registry, nil
}

func (r ContractRegistry) FrontendUI() (ContractDefinition, uint64, error) {
	definition, ok := r.Contracts[FrontendUIContractID]
	if !ok {
		return ContractDefinition{}, 0, errors.New("Contract Registry 缺少 frontend.ui")
	}
	version, err := semver.StrictNewVersion(definition.Version)
	if err != nil {
		return ContractDefinition{}, 0, err
	}
	return definition, version.Major(), nil
}

// SyncContracts writes only mechanically derived files. Plugin manifests are
// compatibility claims owned by each plugin and are checked, never rewritten.
func SyncContracts(repositoryRoot string, write bool) ([]DerivedChange, error) {
	registry, err := LoadContractRegistry(repositoryRoot)
	if err != nil {
		return nil, err
	}
	definition, major, err := registry.FrontendUI()
	if err != nil {
		return nil, err
	}
	expected := map[string][]byte{
		"contracts/generated/go/contractregistry/registry.go":                                  generatedGoRegistry(definition, major),
		"extensions/sdk/ts/ui-contract/src/version.generated.ts":                               generatedTypeScriptRegistry(definition),
		"contracts/schemas/composition/frontend/v1/vastplan.ui-contract.generated.schema.json": generatedUIContractSchema(major),
	}
	changes := make([]DerivedChange, 0, len(expected)+1)
	paths := make([]string, 0, len(expected))
	for path := range expected {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, relative := range paths {
		path := filepath.Join(repositoryRoot, filepath.FromSlash(relative))
		actual, readErr := os.ReadFile(path)
		if readErr == nil && bytes.Equal(actual, expected[relative]) {
			continue
		}
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return nil, readErr
		}
		changes = append(changes, DerivedChange{Path: relative, Reason: "contract registry generated output"})
		if write {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(path, expected[relative], 0o644); err != nil {
				return nil, err
			}
		}
	}
	catalogChange, err := syncPortalCatalogContract(repositoryRoot, definition.Compatibility, write)
	if err != nil {
		return nil, err
	}
	if catalogChange != nil {
		changes = append(changes, *catalogChange)
	}
	if err := validatePluginUICompatibility(repositoryRoot, definition); err != nil {
		return changes, err
	}
	if err := validateNoHardcodedFoundationUIVersion(repositoryRoot); err != nil {
		return changes, err
	}
	return changes, nil
}

func generatedGoRegistry(definition ContractDefinition, major uint64) []byte {
	return []byte(fmt.Sprintf("// Code generated by engineering/tools/pluginrelease contracts; DO NOT EDIT.\npackage contractregistry\n\nconst (\n\tFrontendUIContractVersion = %s\n\tFrontendUIContractRange   = %s\n\tFrontendUIContractMajor   = %d\n)\n", strconv.Quote(definition.Version), strconv.Quote(definition.Compatibility), major))
}

func generatedTypeScriptRegistry(definition ContractDefinition) []byte {
	version, _ := semver.StrictNewVersion(definition.Version)
	return []byte(fmt.Sprintf("// Code generated by engineering/tools/pluginrelease contracts; DO NOT EDIT.\nexport const uiContractVersion = %s as const;\nexport const uiContractRange = %s as const;\nexport const uiContractMajor = %d as const;\n", strconv.Quote(definition.Version), strconv.Quote(definition.Compatibility), version.Major()))
}

func generatedUIContractSchema(major uint64) []byte {
	return []byte(fmt.Sprintf("{\n  \"$schema\": \"https://json-schema.org/draft/2020-12/schema\",\n  \"$id\": \"https://schemas.cdsoft.com.cn/vastplan/composition/frontend/v1/vastplan.ui-contract.generated.schema.json\",\n  \"title\": \"Generated VastPlan Frontend UI Contract Range\",\n  \"type\": \"string\",\n  \"pattern\": \"^(?:\\\\^)?%d\\\\.\",\n  \"maxLength\": 128\n}\n", major))
}

func syncPortalCatalogContract(repositoryRoot, expected string, write bool) (*DerivedChange, error) {
	relative := "engineering/deploy/portal-platform-catalog.json"
	path := filepath.Join(repositoryRoot, filepath.FromSlash(relative))
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	count := 0
	next := uiContractJSONPattern.ReplaceAllFunc(raw, func(value []byte) []byte {
		count++
		match := uiContractJSONPattern.FindSubmatch(value)
		return append(append(append([]byte(nil), match[1]...), expected...), match[3]...)
	})
	if count == 0 {
		return nil, errors.New("Portal Platform Catalog 未声明 UI Contract")
	}
	if bytes.Equal(raw, next) {
		return nil, nil
	}
	change := &DerivedChange{Path: relative, Reason: "frontend.ui compatibility range"}
	if write {
		if err := os.WriteFile(path, next, 0o644); err != nil {
			return nil, err
		}
	}
	return change, nil
}

func validatePluginUICompatibility(repositoryRoot string, definition ContractDefinition) error {
	version, _ := semver.StrictNewVersion(definition.Version)
	var mismatches []string
	for _, root := range []string{"extensions/plugins", "examples/plugins"} {
		entries, err := os.ReadDir(filepath.Join(repositoryRoot, root))
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			relative := filepath.ToSlash(filepath.Join(root, entry.Name(), "vastplan.plugin.json"))
			raw, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(relative)))
			if err != nil {
				return err
			}
			for _, match := range uiContractJSONPattern.FindAllSubmatch(raw, -1) {
				actual := string(match[2])
				constraint, err := semver.NewConstraint(actual)
				if err != nil || !constraint.Check(version) {
					mismatches = append(mismatches, relative+" declares "+actual)
				}
			}
		}
	}
	if len(mismatches) > 0 {
		sort.Strings(mismatches)
		return fmt.Errorf("插件尚未声明兼容 frontend.ui %s:\n- %s", definition.Version, strings.Join(mismatches, "\n- "))
	}
	return nil
}

func validateNoHardcodedFoundationUIVersion(repositoryRoot string) error {
	root := filepath.Join(repositoryRoot, "extensions", "plugins")
	var matches []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "dist" || entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.Contains(filepath.ToSlash(path), "/cn.vastplan.foundation.frontend.") || (filepath.Ext(path) != ".ts" && filepath.Ext(path) != ".tsx") || strings.Contains(path, ".test.") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if hardcodedUIExportPattern.Match(raw) {
			relative, _ := filepath.Rel(repositoryRoot, path)
			matches = append(matches, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(matches) > 0 {
		sort.Strings(matches)
		return fmt.Errorf("Foundation 前端导出必须使用生成的 uiContractVersion:\n- %s", strings.Join(matches, "\n- "))
	}
	return nil
}
