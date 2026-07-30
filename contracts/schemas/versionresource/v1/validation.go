package versionresourcev1

import (
	"bytes"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"sort"
	"strings"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	"github.com/Masterminds/semver/v3"
)

var (
	resourceTypePattern    = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$`)
	resourceIDPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$`)
	adapterIDPattern       = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$`)
	digestPattern          = regexp.MustCompile(`^[a-f0-9]{64}$`)
	materializationPattern = regexp.MustCompile(`^mat_[A-Za-z0-9_-]{16,96}$`)
)

func ValidateResourceKey(key ResourceKey) error {
	if len(key.Type) > 128 || !resourceTypePattern.MatchString(key.Type) || !resourceIDPattern.MatchString(key.ID) {
		return errors.New("版本资源标识无效")
	}
	return nil
}

func ValidateSnapshot(snapshot Snapshot, maxBytes int64) error {
	if maxBytes < 1 {
		return errors.New("版本资源大小上限无效")
	}
	switch snapshot.Kind {
	case ContentJSON:
		trimmed := bytes.TrimSpace(snapshot.JSON)
		if snapshot.MediaType != "application/json" || len(trimmed) == 0 || trimmed[0] != '{' || int64(len(snapshot.JSON)) > maxBytes || len(snapshot.JSON) > versioningv1.MaxContentBytes || !json.Valid(snapshot.JSON) || len(snapshot.Files) != 0 {
			return errors.New("JSON 版本资源快照无效")
		}
	case ContentFiles:
		if snapshot.MediaType == "" || len(snapshot.JSON) != 0 || len(snapshot.Files) > MaxFileEntries {
			return errors.New("文件版本资源快照无效")
		}
		var total int64
		previous := ""
		for _, entry := range snapshot.Files {
			if !validRelativePath(entry.Path) || !digestPattern.MatchString(entry.Digest) || entry.Size < 0 || entry.Mode&^0o777 != 0 || entry.Path <= previous {
				return errors.New("文件版本资源清单无效或未排序")
			}
			if total > maxBytes-entry.Size {
				return errors.New("文件版本资源超过大小上限")
			}
			total += entry.Size
			previous = entry.Path
		}
	default:
		return errors.New("版本资源内容类型无效")
	}
	return nil
}

func ValidateAdapterDescriptor(descriptor AdapterDescriptor) error {
	if descriptor.Protocol != Protocol || !adapterIDPattern.MatchString(descriptor.ID) || descriptor.MaxSnapshotBytes < 1 || !json.Valid(descriptor.ConfigurationSchema) {
		return errors.New("版本资源 Adapter 描述无效")
	}
	if _, err := semver.StrictNewVersion(descriptor.Version); err != nil {
		return errors.New("版本资源 Adapter 版本无效")
	}
	if descriptor.ContentKind != ContentJSON && descriptor.ContentKind != ContentFiles || !validSecretPolicy(descriptor.SecretPolicy) {
		return errors.New("版本资源 Adapter 内容或秘密策略无效")
	}
	if !descriptor.Capabilities.Normalize {
		return errors.New("版本资源 Adapter 必须支持 normalize")
	}
	if err := validateModes(descriptor.SupportedModes, descriptor.DefaultMode); err != nil {
		return err
	}
	if descriptor.ContentKind == ContentJSON && contains(descriptor.SupportedModes, ModeGit) {
		return errors.New("JSON Adapter 不得声明 Git 工作区")
	}
	return nil
}

func ValidateEnvironmentProfile(profile EnvironmentProfile) error {
	if profile.Protocol != Protocol || !resourceIDPattern.MatchString(profile.ID) || profile.Revision == 0 || len(profile.Bindings) == 0 || len(profile.Bindings) > 256 {
		return errors.New("版本环境 Profile 无效")
	}
	if profile.Limits.MaxSessionsPerTenant < 1 || profile.Limits.MaxSessionsPerTenant > 10000 || profile.Limits.MaxLeaseSeconds < 30 || profile.Limits.MaxLeaseSeconds > 86400 || profile.Limits.MaxSnapshotBytes < 1 || profile.Limits.MaxSnapshotBytes > versioningv1.MaxContentBytes || profile.Limits.MaxOverlayBytes < profile.Limits.MaxSnapshotBytes {
		return errors.New("版本环境限制无效")
	}
	seen := map[string]struct{}{}
	for _, binding := range profile.Bindings {
		if !resourceTypePattern.MatchString(binding.ResourceType) || !resourceTypePattern.MatchString(binding.Namespace) || !adapterIDPattern.MatchString(binding.Adapter) || !validProjection(binding.ProjectionPolicy) || len(binding.AdapterConfig) != 0 && !json.Valid(binding.AdapterConfig) {
			return errors.New("版本资源绑定无效")
		}
		if err := validateModes(binding.AllowedModes, binding.DefaultMode); err != nil {
			return err
		}
		if _, duplicate := seen[binding.ResourceType]; duplicate {
			return errors.New("版本环境 ResourceType 重复")
		}
		seen[binding.ResourceType] = struct{}{}
	}
	return nil
}

func ValidateAdapterNormalizeRequest(request AdapterNormalizeRequest, maxBytes int64) error {
	if ValidateResourceKey(request.Resource) != nil || !validMode(request.Mode) || len(request.Configuration) != 0 && !json.Valid(request.Configuration) {
		return errors.New("版本资源规范化请求无效")
	}
	return ValidateSnapshot(request.Snapshot, maxBytes)
}

func ValidateAdapterNormalizeResult(result AdapterNormalizeResult, maxBytes int64) error {
	digest, err := SnapshotDigest(result.Snapshot, maxBytes)
	if err != nil || digest != result.Digest {
		return errors.New("版本资源规范化结果摘要不匹配")
	}
	return nil
}

func ValidateAdapterDiffRequest(request AdapterDiffRequest, maxBytes int64) error {
	if ValidateResourceKey(request.Resource) != nil || !validMode(request.Mode) || request.Left.Kind != request.Right.Kind {
		return errors.New("版本资源比较请求无效")
	}
	if err := ValidateSnapshot(request.Left, maxBytes); err != nil {
		return err
	}
	return ValidateSnapshot(request.Right, maxBytes)
}

func ValidateAdapterDiffResult(result AdapterDiffResult) error {
	if len(result.ChangedPaths) > MaxChangedPaths || result.Summary.Total < 0 || result.Summary.Total != result.Summary.Added+result.Summary.Modified+result.Summary.Removed {
		return errors.New("版本资源比较统计无效")
	}
	if !sort.StringsAreSorted(result.ChangedPaths) || len(result.ChangedPaths) != result.Summary.Total {
		return errors.New("版本资源比较路径未排序或与统计不一致")
	}
	for index, value := range result.ChangedPaths {
		if strings.TrimSpace(value) == "" || index > 0 && result.ChangedPaths[index-1] == value {
			return errors.New("版本资源比较路径无效或重复")
		}
	}
	return nil
}

func ValidateAdapterMaterializeRequest(request AdapterMaterializeRequest, maxBytes int64) error {
	if ValidateResourceKey(request.Resource) != nil || request.Mode != ModeOverlay && request.Mode != ModeGit || request.Snapshot.Kind != ContentFiles {
		return errors.New("版本资源物化请求无效")
	}
	return ValidateSnapshot(request.Snapshot, maxBytes)
}

func ValidateMaterializationRef(ref MaterializationRef) error {
	if !materializationPattern.MatchString(ref.ID) || !digestPattern.MatchString(ref.Digest) || ref.Size < 0 {
		return errors.New("版本资源物化引用无效")
	}
	return nil
}

func validateModes(modes []string, defaultMode string) error {
	if len(modes) == 0 || len(modes) > 3 || !validMode(defaultMode) {
		return errors.New("版本资源运行模式无效")
	}
	copyModes := append([]string(nil), modes...)
	sort.Strings(copyModes)
	for index, mode := range copyModes {
		if !validMode(mode) || index > 0 && copyModes[index-1] == mode {
			return errors.New("版本资源运行模式无效或重复")
		}
	}
	if !contains(copyModes, defaultMode) {
		return errors.New("默认模式不在允许范围")
	}
	return nil
}

func validRelativePath(value string) bool {
	return value != "" && len(value) <= 1024 && !strings.Contains(value, "\\") && !strings.HasPrefix(value, "/") && path.Clean(value) == value && value != "." && !strings.HasPrefix(value, "../")
}

func validMode(value string) bool {
	return value == ModeSnapshot || value == ModeOverlay || value == ModeGit
}
func validSecretPolicy(value string) bool {
	return value == SecretPolicyForbidden || value == SecretPolicyCredentialRefsOnly
}
func validProjection(value string) bool {
	return value == ProjectionNone || value == ProjectionDomainHot || value == ProjectionCurrentOnly
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
