package pythonlock

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

var (
	normalizedNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	nameSeparatorPattern  = regexp.MustCompile(`[-_.]+`)
	versionPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+!-]*$`)
	wheelNamePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*\.whl$`)
	sha256Pattern         = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

func Inspect(raw []byte) (Summary, error) {
	if len(raw) == 0 || len(raw) > MaxLockBytes {
		return Summary{}, fmt.Errorf("pylock.toml 大小必须在 1..%d 字节内", MaxLockBytes)
	}
	var top map[string]any
	if err := toml.Unmarshal(raw, &top); err != nil {
		return Summary{}, fmt.Errorf("解析 pylock.toml: %w", err)
	}
	rawPackageValue, exists := top["packages"]
	if !exists {
		return Summary{}, errors.New("pylock.toml 缺少必需 packages 数组")
	}
	var value document
	if err := toml.Unmarshal(raw, &value); err != nil {
		return Summary{}, fmt.Errorf("解析 pylock.toml 字段: %w", err)
	}
	if value.LockVersion != SpecVersion || strings.TrimSpace(value.CreatedBy) == "" || strings.TrimSpace(value.CreatedBy) != value.CreatedBy || hasLineBreak(value.CreatedBy) {
		return Summary{}, errors.New("pylock.toml 必须使用 lock-version 1.0 并声明规范 created-by")
	}
	if strings.TrimSpace(value.RequiresPython) == "" || strings.TrimSpace(value.RequiresPython) != value.RequiresPython || hasLineBreak(value.RequiresPython) {
		return Summary{}, errors.New("VastPlan Python 锁必须声明 requires-python")
	}
	if len(value.Environments) != 0 || len(value.Extras) != 0 || len(value.DependencyGroups) != 0 || len(value.DefaultGroups) != 0 {
		return Summary{}, errors.New("当前离线插件锁不接受 environments、extras 或 dependency groups；请为插件生成单一默认安装集合")
	}
	if len(value.Packages) > MaxPackages {
		return Summary{}, fmt.Errorf("pylock.toml 包数量超过上限 %d", MaxPackages)
	}
	rawPackages, err := packageTables(rawPackageValue)
	if err != nil || len(rawPackages) != len(value.Packages) {
		return Summary{}, errors.New("pylock.toml packages 必须是规范数组")
	}
	summary := Summary{RequiresPython: value.RequiresPython, CreatedBy: value.CreatedBy}
	packageIdentities := map[string]struct{}{}
	wheelPaths := map[string]struct{}{}
	for index, item := range value.Packages {
		for _, source := range []string{"vcs", "directory", "archive", "sdist"} {
			if _, present := rawPackages[index][source]; present {
				return Summary{}, fmt.Errorf("pylock.toml package %d: 离线插件锁禁止 %s source", index, source)
			}
		}
		if err := validatePackage(item); err != nil {
			return Summary{}, fmt.Errorf("pylock.toml package %d: %w", index, err)
		}
		if _, duplicate := packageIdentities[item.Name]; duplicate {
			return Summary{}, fmt.Errorf("当前离线插件锁要求每个规范包名唯一: %s", item.Name)
		}
		packageIdentities[item.Name] = struct{}{}
		dependencies := append([]Dependency(nil), item.Dependencies...)
		sort.Slice(dependencies, func(i, j int) bool {
			if dependencies[i].Name != dependencies[j].Name {
				return dependencies[i].Name < dependencies[j].Name
			}
			if dependencies[i].Version != dependencies[j].Version {
				return dependencies[i].Version < dependencies[j].Version
			}
			return dependencies[i].Marker < dependencies[j].Marker
		})
		summary.Packages = append(summary.Packages, Package{Name: item.Name, Version: item.Version, Marker: item.Marker, RequiresPython: item.RequiresPython, Dependencies: dependencies})
		for _, lockedWheel := range item.Wheels {
			wheel, err := validateWheel(item, lockedWheel)
			if err != nil {
				return Summary{}, fmt.Errorf("pylock.toml %s@%s wheel: %w", item.Name, item.Version, err)
			}
			if _, duplicate := wheelPaths[wheel.PackagePath]; duplicate {
				return Summary{}, fmt.Errorf("pylock.toml wheel 路径重复: %s", wheel.PackagePath)
			}
			wheelPaths[wheel.PackagePath] = struct{}{}
			summary.Wheels = append(summary.Wheels, wheel)
			if len(summary.Wheels) > MaxTotalWheels {
				return Summary{}, fmt.Errorf("pylock.toml wheel 数量超过上限 %d", MaxTotalWheels)
			}
		}
	}
	sort.Slice(summary.Packages, func(i, j int) bool {
		if summary.Packages[i].Name != summary.Packages[j].Name {
			return summary.Packages[i].Name < summary.Packages[j].Name
		}
		if summary.Packages[i].Version != summary.Packages[j].Version {
			return summary.Packages[i].Version < summary.Packages[j].Version
		}
		return summary.Packages[i].Marker < summary.Packages[j].Marker
	})
	sort.Slice(summary.Wheels, func(i, j int) bool { return summary.Wheels[i].PackagePath < summary.Wheels[j].PackagePath })
	digest := sha256.Sum256(raw)
	summary.SHA256 = hex.EncodeToString(digest[:])
	return summary, nil
}

func packageTables(value any) ([]map[string]any, error) {
	switch values := value.(type) {
	case []map[string]any:
		return values, nil
	case []any:
		result := make([]map[string]any, 0, len(values))
		for _, value := range values {
			item, ok := value.(map[string]any)
			if !ok {
				return nil, errors.New("package 不是 table")
			}
			result = append(result, item)
		}
		return result, nil
	default:
		return nil, errors.New("packages 不是数组")
	}
}

func ValidateManifestRequirements(requirements map[string]string, summary Summary) error {
	pythonRequirement := strings.TrimSpace(requirements["python"])
	if pythonRequirement == "" || pythonRequirement != summary.RequiresPython {
		return errors.New("pylock.toml requires-python 必须与签名清单 requirements.python 完全一致")
	}
	versions := make(map[string]map[string]struct{}, len(summary.Packages))
	for _, item := range summary.Packages {
		if versions[item.Name] == nil {
			versions[item.Name] = map[string]struct{}{}
		}
		versions[item.Name][item.Version] = struct{}{}
	}
	for rawName, rawVersion := range requirements {
		if rawName == "python" {
			continue
		}
		name := NormalizeName(rawName)
		version := strings.TrimSpace(rawVersion)
		if !ExactVersion(version) {
			return fmt.Errorf("签名清单 Python 直接依赖 %s 必须使用精确版本", rawName)
		}
		if _, exists := versions[name][version]; !exists {
			return fmt.Errorf("pylock.toml 未包含签名清单直接依赖 %s==%s", name, version)
		}
	}
	return nil
}

func NormalizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return nameSeparatorPattern.ReplaceAllString(value, "-")
}

func ExactVersion(value string) bool {
	return versionPattern.MatchString(value) && !strings.ContainsAny(value, "<>=~^*, ")
}

func validatePackage(item lockPackage) error {
	if !normalizedNamePattern.MatchString(item.Name) || !ExactVersion(item.Version) || strings.TrimSpace(item.Marker) != item.Marker || strings.TrimSpace(item.RequiresPython) != item.RequiresPython || hasLineBreak(item.Marker) || hasLineBreak(item.RequiresPython) {
		return errors.New("name/version/marker/requires-python 不规范")
	}
	if item.VCS != nil || item.Directory != nil || item.Archive != nil || item.SDist != nil {
		return errors.New("离线插件锁只允许 wheel，禁止 VCS、directory、archive 和 sdist")
	}
	if len(item.Wheels) == 0 || len(item.Wheels) > MaxWheelsPerPackage {
		return fmt.Errorf("每个包必须提供 1..%d 个 wheel", MaxWheelsPerPackage)
	}
	for _, dependency := range item.Dependencies {
		if !normalizedNamePattern.MatchString(dependency.Name) || dependency.Version != "" && !ExactVersion(dependency.Version) || strings.TrimSpace(dependency.Marker) != dependency.Marker || hasLineBreak(dependency.Marker) {
			return errors.New("dependency 引用不规范")
		}
	}
	return nil
}

func hasLineBreak(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}

func validateWheel(item lockPackage, value lockWheel) (Wheel, error) {
	if value.URL != "" || value.Path == "" || path.IsAbs(value.Path) || path.Clean(value.Path) != value.Path || path.Dir(value.Path) != "python-wheels" {
		return Wheel{}, errors.New("wheel 必须使用 pylock.toml 相对目录 python-wheels/ 下的本地 path")
	}
	name := value.Name
	if name == "" {
		name = path.Base(value.Path)
	}
	if path.Base(value.Path) != name || !wheelNamePattern.MatchString(name) {
		return Wheel{}, errors.New("wheel name 必须等于安全的 path 文件名")
	}
	if value.Size <= 0 || value.Size > MaxWheelBytes || !sha256Pattern.MatchString(value.Hashes["sha256"]) {
		return Wheel{}, errors.New("wheel 必须声明受限 size 和小写 SHA-256")
	}
	packagePath := path.Join("supply-chain", value.Path)
	if !strings.HasPrefix(packagePath, WheelPathPrefix) {
		return Wheel{}, errors.New("wheel path 越出固定制品目录")
	}
	return Wheel{PackageName: item.Name, PackageVersion: item.Version, Name: name, Path: value.Path, PackagePath: packagePath, Size: value.Size, SHA256: value.Hashes["sha256"]}, nil
}
