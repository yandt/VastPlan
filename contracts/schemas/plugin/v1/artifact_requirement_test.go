package pluginv1

import (
	"reflect"
	"testing"
)

func TestNormalizeArtifactRequirement(t *testing.T) {
	t.Parallel()
	for _, constraint := range []string{"=1.4.0", "^1.4.0", "^0.4.0", "^0.0.5", ">=1.4.0, <2.0.0"} {
		t.Run(constraint, func(t *testing.T) {
			input := ArtifactRequirement{PluginID: "cn.example.app", Constraint: constraint, Channel: "stable", Features: []string{"trace", "audit", "trace"}}
			original := append([]string(nil), input.Features...)
			got, err := NormalizeArtifactRequirement(input)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Features, []string{"audit", "trace"}) {
				t.Fatalf("features 未规范化: %v", got.Features)
			}
			if !reflect.DeepEqual(input.Features, original) {
				t.Fatalf("规范化修改了调用方输入: got=%v want=%v", input.Features, original)
			}
			again, err := NormalizeArtifactRequirement(got)
			if err != nil || !reflect.DeepEqual(again, got) {
				t.Fatalf("规范化不幂等: first=%+v again=%+v err=%v", got, again, err)
			}
		})
	}
}

func TestNormalizeArtifactRequirementRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]ArtifactRequirement{
		"plugin":   {PluginID: "invalid", Constraint: "^1.0.0"},
		"empty":    {PluginID: "cn.example.app", Constraint: ""},
		"wildcard": {PluginID: "cn.example.app", Constraint: "*"},
		"syntax":   {PluginID: "cn.example.app", Constraint: "not-semver"},
		"channel":  {PluginID: "cn.example.app", Constraint: "^1.0.0", Channel: "Stable"},
		"feature":  {PluginID: "cn.example.app", Constraint: "^1.0.0", Features: []string{"Bad Feature"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeArtifactRequirement(value); err == nil {
				t.Fatalf("非法 Requirement 必须拒绝: %+v", value)
			}
		})
	}
}
