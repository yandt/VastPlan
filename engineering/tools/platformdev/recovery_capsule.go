package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	recoveryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/recovery/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/bootstrapinventory"
)

const recoveryCapsuleFilename = "recovery-capsule.json"

func (r *runtime) recoveryPluginIDs() (map[string]struct{}, error) {
	plan, profile, err := r.loadRecoveryInputs()
	if err != nil {
		return nil, err
	}
	recoveryUnits, err := recoveryv1.RequiredUnits(plan.Stages, recoveryv1.StageRecovery)
	if err != nil {
		return nil, err
	}
	recoverySet := make(map[string]struct{}, len(recoveryUnits))
	for _, unitID := range recoveryUnits {
		recoverySet[unitID] = struct{}{}
	}
	plugins := map[string]struct{}{}
	for _, service := range profile.Services {
		if _, required := recoverySet[service.ID]; !required {
			continue
		}
		for _, plugin := range service.Plugins {
			plugins[plugin.ID] = struct{}{}
		}
	}
	if len(plugins) == 0 {
		return nil, errors.New("Recovery Capsule 没有解析出任何 LKG 插件")
	}
	return plugins, nil
}

func (r *runtime) writeRecoveryCapsule(inventory bootstrapinventory.Inventory) error {
	plan, _, err := r.loadRecoveryInputs()
	if err != nil {
		return err
	}
	artifacts := make([]recoveryv1.Artifact, len(inventory.LastKnownGood))
	for index, item := range inventory.LastKnownGood {
		artifacts[index] = recoveryv1.Artifact{Ref: item.Ref, SHA256: item.SHA256}
	}
	capsule, err := recoveryv1.NormalizeCapsule(recoveryv1.Capsule{
		Version: recoveryv1.Version,
		ID:      plan.ID,
		Inventory: recoveryv1.InventoryBinding{
			RepositoryID: inventory.RepositoryID,
			Generation:   inventory.Generation,
		},
		Artifacts: artifacts,
		Stages:    plan.Stages,
	})
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(capsule, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.runDir, recoveryCapsuleFilename), append(raw, '\n'), 0o600)
}

func (r *runtime) loadRecoveryInputs() (recoveryv1.Plan, backendcompositionv1.PlatformProfile, error) {
	planRaw, err := os.ReadFile(filepath.Join(r.options.root, "engineering", "deploy", "platform-recovery-plan.json"))
	if err != nil {
		return recoveryv1.Plan{}, backendcompositionv1.PlatformProfile{}, err
	}
	plan, err := recoveryv1.ParsePlan(planRaw)
	if err != nil {
		return recoveryv1.Plan{}, backendcompositionv1.PlatformProfile{}, fmt.Errorf("解析平台 Recovery Plan: %w", err)
	}
	profile, err := backendcompositionv1.ParsePlatformProfileFile(filepath.Join(r.runDir, "platform-management-profile.json"))
	if err != nil {
		return recoveryv1.Plan{}, backendcompositionv1.PlatformProfile{}, err
	}
	if err := validateRecoveryPlanCoverage(plan, profile); err != nil {
		return recoveryv1.Plan{}, backendcompositionv1.PlatformProfile{}, err
	}
	return plan, profile, nil
}

func validateRecoveryPlanCoverage(plan recoveryv1.Plan, profile backendcompositionv1.PlatformProfile) error {
	profileUnits := make([]string, 0, len(profile.Services))
	for _, service := range profile.Services {
		if service.Enabled {
			profileUnits = append(profileUnits, service.ID)
		}
	}
	planUnits, err := recoveryv1.RequiredUnits(plan.Stages, recoveryv1.StagePlatform)
	if err != nil {
		return err
	}
	sort.Strings(profileUnits)
	if len(profileUnits) != len(planUnits) {
		return fmt.Errorf("Recovery Plan 必须覆盖全部启用 Seed unit: profile=%v plan=%v", profileUnits, planUnits)
	}
	for index := range profileUnits {
		if profileUnits[index] != planUnits[index] {
			return fmt.Errorf("Recovery Plan 与启用 Seed unit 不一致: profile=%v plan=%v", profileUnits, planUnits)
		}
	}
	return nil
}

func validateRecoveryCapsuleAgainstInventory(raw []byte, inventory bootstrapinventory.Inventory) error {
	capsule, err := recoveryv1.ParseCapsule(raw)
	if err != nil {
		return err
	}
	if capsule.Inventory.RepositoryID != inventory.RepositoryID || capsule.Inventory.Generation != inventory.Generation {
		return errors.New("Recovery Capsule 与 Bootstrap Inventory 身份不一致")
	}
	if len(capsule.Artifacts) != len(inventory.LastKnownGood) {
		return errors.New("Recovery Capsule 与 Bootstrap LKG 制品数量不一致")
	}
	for index := range capsule.Artifacts {
		artifact, item := capsule.Artifacts[index], inventory.LastKnownGood[index]
		if artifact.Ref != item.Ref || artifact.SHA256 != item.SHA256 {
			return fmt.Errorf("Recovery Capsule 与 Bootstrap LKG 制品不一致: %s", artifact.Ref.PluginID)
		}
	}
	return nil
}
