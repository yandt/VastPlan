package pluginv1

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"
)

var (
	requirementPluginIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)+$`)
	requirementChannelPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	requirementFeaturePattern  = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)
)

// NormalizeArtifactRequirement validates the shared desired-version contract
// and returns a deterministic copy. Callers may apply narrower input policies.
func NormalizeArtifactRequirement(value ArtifactRequirement) (ArtifactRequirement, error) {
	if len(value.PluginID) > 160 || !requirementPluginIDPattern.MatchString(value.PluginID) {
		return ArtifactRequirement{}, fmt.Errorf("Artifact Requirement pluginId 无效: %q", value.PluginID)
	}
	if len(value.Constraint) == 0 || len(value.Constraint) > 128 || value.Constraint != strings.TrimSpace(value.Constraint) {
		return ArtifactRequirement{}, errors.New("Artifact Requirement constraint 不能为空、包含首尾空白或超过 128 字符")
	}
	if value.Constraint == "*" || strings.EqualFold(value.Constraint, "x") {
		return ArtifactRequirement{}, errors.New("Artifact Requirement constraint 不允许全版本通配")
	}
	if _, err := semver.NewConstraint(value.Constraint); err != nil {
		return ArtifactRequirement{}, fmt.Errorf("Artifact Requirement constraint 无效: %w", err)
	}
	if value.Channel != "" && !requirementChannelPattern.MatchString(value.Channel) {
		return ArtifactRequirement{}, fmt.Errorf("Artifact Requirement channel 无效: %q", value.Channel)
	}

	if len(value.Features) > 64 {
		return ArtifactRequirement{}, errors.New("Artifact Requirement features 不能超过 64 项")
	}
	features := append([]string(nil), value.Features...)
	sort.Strings(features)
	normalized := features[:0]
	for _, feature := range features {
		if len(feature) > 80 || !requirementFeaturePattern.MatchString(feature) {
			return ArtifactRequirement{}, fmt.Errorf("Artifact Requirement feature 无效: %q", feature)
		}
		if len(normalized) == 0 || normalized[len(normalized)-1] != feature {
			normalized = append(normalized, feature)
		}
	}
	value.Features = normalized
	return value, nil
}
