// Package pluginsbom derives a deterministic plugin SBOM from language-specific
// build facts. It never guesses dependencies from source imports alone.
package pluginsbom

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactsupplychain"
	"cdsoft.com.cn/VastPlan/engineering/internal/cyclonedx"
)

type Options struct {
	Root       string
	PluginDir  string
	GoBinaries []string
	Metafiles  []string
}

type Result struct {
	Raw        []byte
	Components int
}

func Generate(options Options) (Result, error) {
	root, pluginDir, err := normalizeOptions(options)
	if err != nil {
		return Result{}, err
	}
	manifestRaw, err := os.ReadFile(filepath.Join(pluginDir, "vastplan.plugin.json"))
	if err != nil {
		return Result{}, fmt.Errorf("读取插件清单: %w", err)
	}
	manifest, err := pluginv1.ParseManifest(manifestRaw)
	if err != nil {
		return Result{}, err
	}
	rootRef := "pkg:generic/" + manifest.ID + "@" + manifest.Version
	rootComponent := cyclonedx.Component{Type: "application", BOMRef: rootRef, Name: manifest.ID, Version: manifest.Version, PURL: rootRef}
	components := make([]cyclonedx.Component, 0)
	goBinaries := cleanPaths(options.GoBinaries)
	soleGoBinaryHash := ""
	for _, filename := range goBinaries {
		goValues, binaryHash, goVersion, err := goDependencies(filename)
		if err != nil {
			return Result{}, err
		}
		components = append(components, goValues...)
		if len(goBinaries) == 1 {
			soleGoBinaryHash = binaryHash
		}
		artifactName := filepath.Base(filename)
		rootComponent.Properties = append(rootComponent.Properties,
			cyclonedx.Property{Name: "vastplan:build.goArtifact." + artifactName + ".sha256", Value: binaryHash},
			cyclonedx.Property{Name: "vastplan:build.goArtifact." + artifactName + ".goVersion", Value: goVersion},
		)
	}
	if len(goBinaries) == 1 {
		rootComponent.Hashes = append(rootComponent.Hashes, cyclonedx.Hash{Alg: "SHA-256", Content: soleGoBinaryHash})
	}
	metafiles := cleanMetafiles(options.Metafiles)
	if len(metafiles) > 0 {
		nodeValues, err := nodeDependencies(root, pluginDir, metafiles)
		if err != nil {
			return Result{}, err
		}
		components = append(components, nodeValues...)
	}
	pythonValues, runtimeProperties, err := manifestDependencies(manifest)
	if err != nil {
		return Result{}, err
	}
	components = append(components, pythonValues...)
	rootComponent.Properties = append(rootComponent.Properties, runtimeProperties...)
	if err := requireBuildFacts(manifest, goBinaries, metafiles); err != nil {
		return Result{}, err
	}
	document, err := cyclonedx.Build(rootComponent, components, nil)
	if err != nil {
		return Result{}, err
	}
	raw, err := cyclonedx.Marshal(document)
	if err != nil {
		return Result{}, err
	}
	summary, err := artifactsupplychain.InspectCycloneDX(raw)
	if err != nil || summary.RootName != manifest.ID || summary.RootVersion != manifest.Version {
		return Result{}, errors.New("生成的插件 SBOM 未通过共享信任校验")
	}
	return Result{Raw: raw, Components: len(document.Components)}, nil
}

func normalizeOptions(options Options) (string, string, error) {
	if strings.TrimSpace(options.Root) == "" || strings.TrimSpace(options.PluginDir) == "" {
		return "", "", errors.New("插件 SBOM 必须提供工作区根和插件目录")
	}
	root, err := filepath.Abs(options.Root)
	if err != nil {
		return "", "", err
	}
	pluginDir, err := filepath.Abs(options.PluginDir)
	if err != nil {
		return "", "", err
	}
	relative, err := filepath.Rel(root, pluginDir)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", errors.New("插件目录必须位于工作区根内")
	}
	return root, pluginDir, nil
}

func cleanMetafiles(values []string) []string {
	return cleanPaths(values)
}

func cleanPaths(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		absolute, err := filepath.Abs(value)
		if err == nil {
			set[absolute] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func requireBuildFacts(manifest pluginv1.Manifest, goBinaries, metafiles []string) error {
	backendEntry := strings.TrimSpace(manifest.Entry["backend"])
	driver := "native"
	if manifest.Execution != nil && manifest.Execution.Backend != nil && manifest.Execution.Backend.Driver != "" {
		driver = manifest.Execution.Backend.Driver
	}
	switch driver {
	case "native":
		if backendEntry != "" && len(goBinaries) == 0 {
			return errors.New("native 插件自动生成 SBOM 必须提供实际 Go 二进制；其他原生语言请显式提供外部 SBOM")
		}
	case "node-worker":
		if len(metafiles) == 0 {
			return errors.New("node-worker 插件自动生成 SBOM 必须提供实际 esbuild metafile")
		}
	case "python", "python-subinterpreter":
		// Python runtime requirements are carried by the signed manifest.
	default:
		return fmt.Errorf("运行驱动 %s 尚无自动 SBOM 取证器，请显式提供外部 SBOM", driver)
	}
	if manifest.Entry["frontend"] != "" && len(metafiles) == 0 {
		return errors.New("Frontend 插件自动生成 SBOM 必须提供实际 esbuild metafile")
	}
	return nil
}

func manifestDependencies(manifest pluginv1.Manifest) ([]cyclonedx.Component, []cyclonedx.Property, error) {
	if manifest.Execution == nil || manifest.Execution.Backend == nil {
		return nil, nil, nil
	}
	execution := manifest.Execution.Backend
	keys := make([]string, 0, len(execution.Requirements))
	for key := range execution.Requirements {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	components := make([]cyclonedx.Component, 0)
	properties := make([]cyclonedx.Property, 0)
	pythonDriver := execution.Driver == "python" || execution.Driver == "python-subinterpreter"
	for _, key := range keys {
		value := strings.TrimSpace(execution.Requirements[key])
		if key == "python" || key == "node" || !pythonDriver {
			properties = append(properties, cyclonedx.Property{Name: "vastplan:runtime.requirement." + key, Value: value})
			continue
		}
		if !exactPackageVersion(value) {
			return nil, nil, fmt.Errorf("Python 依赖 %s 必须使用精确版本生成 SBOM，当前为 %s", key, value)
		}
		name := normalizePyPIName(key)
		purl := "pkg:pypi/" + name + "@" + value
		components = append(components, cyclonedx.Component{Type: "library", BOMRef: purl, Name: name, Version: value, PURL: purl, Properties: []cyclonedx.Property{{Name: "vastplan:dependency.evidence", Value: "signed-manifest-requirement"}}})
	}
	return components, properties, nil
}

func exactPackageVersion(value string) bool {
	if value == "" || strings.ContainsAny(value, "<>=~^* ,") {
		return false
	}
	for _, part := range strings.Split(value, ".") {
		if part == "" {
			return false
		}
	}
	return true
}

func normalizePyPIName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer("_", "-", ".", "-")
	return replacer.Replace(value)
}

func decodeJSONFile(filename string, target any) error {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}
