package backendcompositionv1

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func ParseResolutionReport(raw []byte) (ResolutionReport, error) {
	_, schema, err := planningSchemas()
	if err != nil {
		return ResolutionReport{}, err
	}
	if err := validateJSON(schema, raw, "Backend Resolution Report"); err != nil {
		return ResolutionReport{}, err
	}
	var report ResolutionReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return ResolutionReport{}, fmt.Errorf("解析 Backend Resolution Report 字段: %w", err)
	}
	report, err = NormalizeResolutionReport(report)
	if err != nil {
		return ResolutionReport{}, err
	}
	if report.ConfigurationPlan.Digest != report.ConfigurationPlan.ComputedDigest() {
		return ResolutionReport{}, fmt.Errorf("Configuration Plan digest 与规范内容不一致")
	}
	if report.PlanDigest != report.ComputedPlanDigest() {
		return ResolutionReport{}, fmt.Errorf("Resolution Report planDigest 与规范内容不一致")
	}
	return report, nil
}

func ValidateResolutionReport(report ResolutionReport) (ResolutionReport, error) {
	raw, err := json.Marshal(report)
	if err != nil {
		return ResolutionReport{}, fmt.Errorf("编码 Backend Resolution Report: %w", err)
	}
	return ParseResolutionReport(raw)
}

func NormalizeResolutionReport(report ResolutionReport) (ResolutionReport, error) {
	if report.ApplicationComposition != nil {
		composition, err := ValidateApplicationComposition(*report.ApplicationComposition)
		if err != nil {
			return ResolutionReport{}, fmt.Errorf("Resolution Report Application Composition 无效: %w", err)
		}
		report.ApplicationComposition = &composition
		if report.ApplicationCompositionDigest != composition.Digest() {
			return ResolutionReport{}, fmt.Errorf("Resolution Report Application Composition digest 不一致")
		}
	}
	if report.ArtifactLock != nil {
		raw, err := json.Marshal(report.ArtifactLock)
		if err != nil {
			return ResolutionReport{}, fmt.Errorf("编码 Resolution Report Artifact Lock: %w", err)
		}
		if err := pluginv1.ValidateArtifactLock(raw); err != nil {
			return ResolutionReport{}, err
		}
		digest, err := pluginv1.ArtifactLockDigest(*report.ArtifactLock)
		if err != nil {
			return ResolutionReport{}, fmt.Errorf("计算 Resolution Report Artifact Lock digest: %w", err)
		}
		if digest != report.ArtifactLock.Digest {
			return ResolutionReport{}, fmt.Errorf("Resolution Report Artifact Lock digest 与规范内容不一致")
		}
		if report.ArtifactLock.Target != compositioncommonv1.KernelBackend {
			return ResolutionReport{}, fmt.Errorf("Resolution Report Artifact Lock target 必须为 backend")
		}
	}
	sort.Slice(report.Features, func(i, j int) bool {
		left, right := report.Features[i], report.Features[j]
		if left.UnitID != right.UnitID {
			return left.UnitID < right.UnitID
		}
		if left.PluginID != right.PluginID {
			return left.PluginID < right.PluginID
		}
		return left.FeatureID < right.FeatureID
	})
	if err := rejectDuplicateFeatures(report.Features); err != nil {
		return ResolutionReport{}, err
	}
	sort.Slice(report.ProviderBindings, func(i, j int) bool {
		left, right := report.ProviderBindings[i], report.ProviderBindings[j]
		if left.ConsumerUnitID != right.ConsumerUnitID {
			return left.ConsumerUnitID < right.ConsumerUnitID
		}
		return left.Capability < right.Capability
	})
	if err := rejectDuplicateProviderBindings(report.ProviderBindings); err != nil {
		return ResolutionReport{}, err
	}
	if err := normalizeServiceGraph(&report.ServiceGraph); err != nil {
		return ResolutionReport{}, err
	}
	for index := range report.ConfigurationPlan.Items {
		item := &report.ConfigurationPlan.Items[index]
		sort.Slice(item.Missing, func(i, j int) bool {
			if item.Missing[i].Kind != item.Missing[j].Kind {
				return item.Missing[i].Kind < item.Missing[j].Kind
			}
			return item.Missing[i].Field < item.Missing[j].Field
		})
	}
	sort.Slice(report.ConfigurationPlan.Items, func(i, j int) bool {
		left, right := report.ConfigurationPlan.Items[i], report.ConfigurationPlan.Items[j]
		if left.UnitID != right.UnitID {
			return left.UnitID < right.UnitID
		}
		return left.PluginID < right.PluginID
	})
	if err := validateConfigurationPlan(report.ConfigurationPlan); err != nil {
		return ResolutionReport{}, err
	}
	if err := validateReportReferences(report); err != nil {
		return ResolutionReport{}, err
	}
	sort.Slice(report.Diagnostics, func(i, j int) bool {
		left, right := report.Diagnostics[i], report.Diagnostics[j]
		if left.Severity != right.Severity {
			return left.Severity < right.Severity
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		if leftPath, rightPath := strings.Join(left.Path, "\x00"), strings.Join(right.Path, "\x00"); leftPath != rightPath {
			return leftPath < rightPath
		}
		return left.Message < right.Message
	})
	if err := validateResolutionStatus(report); err != nil {
		return ResolutionReport{}, err
	}
	return report, nil
}

func rejectDuplicateFeatures(features []ResolvedFeature) error {
	seen := map[string]struct{}{}
	for _, feature := range features {
		key := feature.UnitID + "\x00" + feature.PluginID + "\x00" + feature.FeatureID
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("Resolution Report Feature 重复: %s/%s/%s", feature.UnitID, feature.PluginID, feature.FeatureID)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func rejectDuplicateProviderBindings(bindings []CapabilityProviderBinding) error {
	seen := map[string]struct{}{}
	for _, binding := range bindings {
		key := binding.ConsumerUnitID + "\x00" + binding.Capability
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("Resolution Report Provider Binding 重复: %s/%s", binding.ConsumerUnitID, binding.Capability)
		}
		seen[key] = struct{}{}
	}
	return nil
}
