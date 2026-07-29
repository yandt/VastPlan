package recoveryv1

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

var stageOrder = []string{StageRecovery, StageControlPlane, StagePlatform}

func ParsePlan(raw []byte) (Plan, error) {
	var value Plan
	if err := decodeStrict(raw, &value); err != nil {
		return Plan{}, err
	}
	return NormalizePlan(value)
}

func ParseCapsule(raw []byte) (Capsule, error) {
	var value Capsule
	if err := decodeStrict(raw, &value); err != nil {
		return Capsule{}, err
	}
	return NormalizeCapsule(value)
}

func NormalizePlan(value Plan) (Plan, error) {
	if value.Version != Version || strings.TrimSpace(value.ID) == "" {
		return Plan{}, errors.New("Recovery Plan 版本或身份无效")
	}
	stages, err := normalizeStages(value.Stages)
	if err != nil {
		return Plan{}, err
	}
	value.ID, value.Stages = strings.TrimSpace(value.ID), stages
	return value, nil
}

func NormalizeCapsule(value Capsule) (Capsule, error) {
	if value.Version != Version || strings.TrimSpace(value.ID) == "" {
		return Capsule{}, errors.New("Recovery Capsule 版本或身份无效")
	}
	if strings.TrimSpace(value.Inventory.RepositoryID) == "" || value.Inventory.Generation == 0 {
		return Capsule{}, errors.New("Recovery Capsule 的 Bootstrap Inventory 绑定无效")
	}
	stages, err := normalizeStages(value.Stages)
	if err != nil {
		return Capsule{}, err
	}
	if len(value.Artifacts) == 0 {
		return Capsule{}, errors.New("Recovery Capsule 必须锁定至少一个 LKG 制品")
	}
	artifacts := append([]Artifact(nil), value.Artifacts...)
	for index := range artifacts {
		artifact := &artifacts[index]
		artifact.Ref.PluginID = strings.TrimSpace(artifact.Ref.PluginID)
		artifact.Ref.Version = strings.TrimSpace(artifact.Ref.Version)
		artifact.Ref.Channel = strings.TrimSpace(artifact.Ref.Channel)
		artifact.SHA256 = strings.TrimSpace(artifact.SHA256)
		if artifact.Ref.PluginID == "" || artifact.Ref.Version == "" || artifact.Ref.Channel == "" || len(artifact.SHA256) != 64 {
			return Capsule{}, fmt.Errorf("Recovery Capsule 制品 %d 身份不完整", index)
		}
		if artifact.SHA256 != strings.ToLower(artifact.SHA256) {
			return Capsule{}, fmt.Errorf("Recovery Capsule 制品 %s 摘要必须使用小写", artifact.Ref.PluginID)
		}
		if _, err := hex.DecodeString(artifact.SHA256); err != nil {
			return Capsule{}, fmt.Errorf("Recovery Capsule 制品 %s 摘要无效", artifact.Ref.PluginID)
		}
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifactKey(artifacts[i]) < artifactKey(artifacts[j]) })
	for index := 1; index < len(artifacts); index++ {
		if artifacts[index].Ref == artifacts[index-1].Ref {
			return Capsule{}, fmt.Errorf("Recovery Capsule 包含重复制品 %s", artifacts[index].Ref.PluginID)
		}
	}
	value.ID = strings.TrimSpace(value.ID)
	value.Inventory.RepositoryID = strings.TrimSpace(value.Inventory.RepositoryID)
	value.Artifacts, value.Stages = artifacts, stages
	return value, nil
}

func UnitStage(plan Plan, unitID string) (string, bool) {
	for _, stage := range plan.Stages {
		index := sort.Search(len(stage.Units), func(index int) bool { return stage.Units[index].ID >= unitID })
		if index < len(stage.Units) && stage.Units[index].ID == unitID {
			return stage.ID, true
		}
	}
	return "", false
}

// RequiredUnits returns the cumulative unit set for the requested stage.
func RequiredUnits(stages []Stage, stageID string) ([]string, error) {
	result := make([]string, 0)
	for _, stage := range stages {
		for _, unit := range stage.Units {
			result = append(result, unit.ID)
		}
		if stage.ID == stageID {
			sort.Strings(result)
			return result, nil
		}
	}
	return nil, fmt.Errorf("Recovery Capsule 不包含阶段 %q", stageID)
}

// RequiredUnitRequirements returns the cumulative quorum requirements for a stage.
func RequiredUnitRequirements(stages []Stage, stageID string) ([]UnitRequirement, error) {
	result := make([]UnitRequirement, 0)
	for _, stage := range stages {
		result = append(result, stage.Units...)
		if stage.ID == stageID {
			sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
			return result, nil
		}
	}
	return nil, fmt.Errorf("Recovery Capsule 不包含阶段 %q", stageID)
}

func normalizeStages(stages []Stage) ([]Stage, error) {
	if len(stages) != len(stageOrder) {
		return nil, errors.New("Recovery Capsule 必须依次定义 recovery、control-plane、platform 三个阶段")
	}
	seen := map[string]struct{}{}
	out := make([]Stage, len(stages))
	for index, expected := range stageOrder {
		stage := stages[index]
		if stage.ID != expected || len(stage.Units) == 0 {
			return nil, fmt.Errorf("Recovery Capsule 阶段 %d 必须是非空 %s", index, expected)
		}
		stage.Units = append([]UnitRequirement(nil), stage.Units...)
		for unitIndex := range stage.Units {
			stage.Units[unitIndex].ID = strings.TrimSpace(stage.Units[unitIndex].ID)
			if stage.Units[unitIndex].ID == "" || stage.Units[unitIndex].MinReady == 0 || stage.Units[unitIndex].MinReady > 1024 {
				return nil, fmt.Errorf("Recovery Capsule 阶段 %s 包含空 unit", stage.ID)
			}
			if _, exists := seen[stage.Units[unitIndex].ID]; exists {
				return nil, fmt.Errorf("Recovery Capsule unit %s 被重复分级", stage.Units[unitIndex].ID)
			}
			seen[stage.Units[unitIndex].ID] = struct{}{}
		}
		sort.Slice(stage.Units, func(i, j int) bool { return stage.Units[i].ID < stage.Units[j].ID })
		out[index] = stage
	}
	return out, nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("Recovery Capsule 只能包含一个 JSON 文档")
	}
	return nil
}

func artifactKey(value Artifact) string {
	return value.Ref.PluginID + "@" + value.Ref.Version + "/" + value.Ref.Channel
}
