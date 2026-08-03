package releaseorchestrator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type ReleaseMode string

const (
	ReleaseModeDevelopment ReleaseMode = "development"
	ReleaseModeProduction  ReleaseMode = "production"
)

type ReleaseSpec struct {
	SchemaVersion     int                    `yaml:"schemaVersion"`
	Mode              ReleaseMode            `yaml:"mode"`
	BaselineInventory string                 `yaml:"baselineInventory,omitempty"`
	Plugins           []ReleasePluginRequest `yaml:"plugins"`
}

type ReleasePluginRequest struct {
	ID              string             `yaml:"id"`
	Change          ReleaseChangeClass `yaml:"change,omitempty"`
	BackendTarget   string             `yaml:"backendTarget,omitempty"`
	BackendBinding  string             `yaml:"backendBinding,omitempty"`
	FrontendTarget  string             `yaml:"frontendTarget,omitempty"`
	FrontendBinding string             `yaml:"frontendBinding,omitempty"`
	FrontendScope   string             `yaml:"frontendScope,omitempty"`
}

type ReleasePlan struct {
	SchemaVersion       int                         `json:"schemaVersion"`
	Mode                ReleaseMode                 `json:"mode"`
	ContractRegistry    string                      `json:"contractRegistry"`
	Contracts           map[string]string           `json:"contracts"`
	Plugins             []ReleasePluginPlan         `json:"plugins"`
	Impacts             []ReleaseImpact             `json:"impacts"`
	InterfaceBaseline   *InterfaceBaselinePlan      `json:"interfaceBaseline,omitempty"`
	DeploymentChanges   []DeploymentReferenceChange `json:"deploymentChanges,omitempty"`
	GeneratedFiles      []string                    `json:"generatedFiles,omitempty"`
	ExecutionOrder      []string                    `json:"executionOrder"`
	Actions             []string                    `json:"actions"`
	RequiresApproval    bool                        `json:"requiresApproval"`
	ProductionExecution bool                        `json:"productionExecution"`
}

// InterfaceBaselinePlan identifies the immutable Inventory used to verify
// compatibility. The raw Inventory is intentionally not copied into a Release
// Plan because it is already an independently verifiable activation record.
type InterfaceBaselinePlan struct {
	Source          string `json:"source"`
	Generation      uint64 `json:"generation"`
	InventoryDigest string `json:"inventoryDigest"`
}

type ReleasePluginPlan struct {
	ID                  string             `json:"id"`
	Version             string             `json:"version"`
	Change              ReleaseChangeClass `json:"change"`
	SourcePath          string             `json:"sourcePath"`
	Faces               []string           `json:"faces"`
	Dependencies        map[string]string  `json:"dependencies,omitempty"`
	ReverseDependencies []string           `json:"reverseDependencies,omitempty"`
	BackendTarget       string             `json:"backendTarget,omitempty"`
	BackendBinding      string             `json:"backendBinding,omitempty"`
	FrontendTarget      string             `json:"frontendTarget,omitempty"`
	FrontendBinding     string             `json:"frontendBinding,omitempty"`
	FrontendScope       string             `json:"frontendScope,omitempty"`
}

func LoadReleaseSpec(path string) (ReleaseSpec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ReleaseSpec{}, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	var spec ReleaseSpec
	if err := decoder.Decode(&spec); err != nil {
		return ReleaseSpec{}, fmt.Errorf("解析 Release Spec: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ReleaseSpec{}, errors.New("Release Spec 包含多余 YAML 文档")
	}
	if spec.SchemaVersion != 1 || (spec.Mode != ReleaseModeDevelopment && spec.Mode != ReleaseModeProduction) || len(spec.Plugins) == 0 {
		return ReleaseSpec{}, errors.New("Release Spec schemaVersion、mode 或 plugins 无效")
	}
	seen := map[string]struct{}{}
	spec.BaselineInventory = strings.TrimSpace(spec.BaselineInventory)
	for index := range spec.Plugins {
		request := &spec.Plugins[index]
		request.ID = strings.TrimSpace(request.ID)
		if request.ID == "" {
			return ReleaseSpec{}, errors.New("Release Spec 插件 ID 不能为空")
		}
		if _, exists := seen[request.ID]; exists {
			return ReleaseSpec{}, fmt.Errorf("Release Spec 插件重复: %s", request.ID)
		}
		seen[request.ID] = struct{}{}
		if request.Change == "" {
			request.Change = ReleaseChangeImplementation
		}
		if request.Change != ReleaseChangeImplementation && request.Change != ReleaseChangeAdditive && request.Change != ReleaseChangeBreaking {
			return ReleaseSpec{}, fmt.Errorf("插件 %s 的 change 必须是 implementation、additive 或 breaking", request.ID)
		}
		if request.FrontendScope == "" && (request.FrontendTarget != "" || request.FrontendBinding != "") {
			request.FrontendScope = "application-plugin"
		}
		if request.FrontendScope != "" && request.FrontendScope != "application-plugin" && request.FrontendScope != "platform-profile-plugin" {
			return ReleaseSpec{}, fmt.Errorf("插件 %s 的 frontendScope 无效", request.ID)
		}
		if spec.Mode == ReleaseModeProduction && (request.BackendTarget != "" || request.BackendBinding != "" || request.FrontendTarget != "" || request.FrontendBinding != "") {
			return ReleaseSpec{}, fmt.Errorf("生产 Release Spec 不得携带开发 Test Release 目标: %s", request.ID)
		}
	}
	return spec, nil
}

func BuildReleasePlan(repositoryRoot string, spec ReleaseSpec) (ReleasePlan, error) {
	baseline, err := loadInterfaceBaseline(repositoryRoot, spec)
	if err != nil {
		return ReleasePlan{}, err
	}
	return BuildReleasePlanWithBaseline(repositoryRoot, spec, baseline)
}

// BuildReleasePlanWithBaseline lets a production release controller inject the
// active Generation it resolved through its trusted Inventory port. CLI users
// normally call BuildReleasePlan, which adapts the optional baselineInventory
// file from the Release Spec once at its composition boundary.
func BuildReleasePlanWithBaseline(repositoryRoot string, spec ReleaseSpec, baseline *InterfaceBaseline) (ReleasePlan, error) {
	if spec.Mode == ReleaseModeProduction && baseline == nil {
		return ReleasePlan{}, errors.New("生产发布必须注入活动 Plugin Inventory 基线")
	}
	workspace, err := LoadPluginWorkspace(repositoryRoot)
	if err != nil {
		return ReleasePlan{}, err
	}
	registry, err := LoadContractRegistry(repositoryRoot)
	if err != nil {
		return ReleasePlan{}, err
	}
	contractChanges, err := SyncContracts(repositoryRoot, false)
	if err != nil {
		return ReleasePlan{}, err
	}
	versions := map[string]string{}
	requests := map[string]ReleasePluginRequest{}
	for _, request := range spec.Plugins {
		plugin, ok := workspace.Plugins[request.ID]
		if !ok {
			return ReleasePlan{}, fmt.Errorf("Release Spec 引用了不存在的插件 %s", request.ID)
		}
		versions[request.ID] = plugin.Version
		requests[request.ID] = request
	}
	impacts, err := AnalyzeReleaseImpactWithBaseline(workspace, requests, baseline)
	if err != nil {
		return ReleasePlan{}, err
	}
	deploymentChanges, err := DeploymentReferenceChanges(repositoryRoot, versions)
	if err != nil {
		return ReleasePlan{}, err
	}
	capabilityChanges, err := SyncCapabilityContractProjections(repositoryRoot, workspace, false)
	if err != nil {
		return ReleasePlan{}, err
	}
	packageVersionChanges, err := SyncSelectedPluginPackageVersions(repositoryRoot, workspace, versions, false)
	if err != nil {
		return ReleasePlan{}, err
	}
	runtimeVersionChanges, err := SyncSelectedPluginRuntimeVersions(repositoryRoot, workspace, versions, false)
	if err != nil {
		return ReleasePlan{}, err
	}
	plan := ReleasePlan{
		SchemaVersion: 1, Mode: spec.Mode, ContractRegistry: ContractRegistryPath,
		Contracts: map[string]string{}, DeploymentChanges: deploymentChanges, Impacts: impacts,
		ExecutionOrder: releaseExecutionOrder(workspace, versions), RequiresApproval: spec.Mode == ReleaseModeProduction,
		ProductionExecution: false,
	}
	if baseline != nil {
		plan.InterfaceBaseline = &InterfaceBaselinePlan{Source: baseline.Source, Generation: baseline.Inventory.Generation, InventoryDigest: baseline.Inventory.Digest}
	}
	contractIDs := make([]string, 0, len(registry.Contracts))
	for id := range registry.Contracts {
		contractIDs = append(contractIDs, id)
	}
	sort.Strings(contractIDs)
	for _, id := range contractIDs {
		plan.Contracts[id] = registry.Contracts[id].Version
	}
	for _, change := range capabilityChanges {
		plan.GeneratedFiles = append(plan.GeneratedFiles, change.Path)
	}
	for _, change := range packageVersionChanges {
		plan.GeneratedFiles = append(plan.GeneratedFiles, change.Path)
	}
	for _, change := range runtimeVersionChanges {
		plan.GeneratedFiles = append(plan.GeneratedFiles, change.Path)
	}
	for _, change := range contractChanges {
		plan.GeneratedFiles = append(plan.GeneratedFiles, change.Path)
	}
	sort.Strings(plan.GeneratedFiles)
	for _, id := range plan.ExecutionOrder {
		plugin := workspace.Plugins[id]
		request := requests[id]
		faces := make([]string, 0, len(plugin.Manifest.Entry))
		for _, face := range []string{"backend", "frontend", "runner", "mobile"} {
			if strings.TrimSpace(plugin.Manifest.Entry[face]) != "" {
				faces = append(faces, face)
			}
		}
		dependencies := allDependencies(plugin.Manifest)
		plan.Plugins = append(plan.Plugins, ReleasePluginPlan{
			ID: id, Version: plugin.Version, SourcePath: plugin.Path, Faces: faces,
			Change:       request.Change,
			Dependencies: dependencies, ReverseDependencies: workspace.ReverseDependencies(id),
			BackendTarget: request.BackendTarget, BackendBinding: request.BackendBinding,
			FrontendTarget: request.FrontendTarget, FrontendBinding: request.FrontendBinding, FrontendScope: request.FrontendScope,
		})
	}
	plan.Actions = []string{"sync-contract-registry", "sync-capability-contract-projections", "sync-package-version-projections", "sync-deployment-exact-references", "validate-public-interface-compatibility", "validate-dependency-impact", "build-immutable-candidates"}
	if spec.Mode == ReleaseModeDevelopment {
		plan.Actions = append(plan.Actions, "publish-local-test-workspace", "submit-test-release", "activate-candidate-generation")
	} else {
		plan.Actions = append(plan.Actions, "emit-production-approval-plan")
	}
	return plan, nil
}

func WriteReleasePlan(path string, plan ReleasePlan) error {
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func releaseExecutionOrder(workspace PluginWorkspace, selected map[string]string) []string {
	visited := map[string]bool{}
	order := make([]string, 0, len(selected))
	var visit func(string)
	visit = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true
		dependencies := make([]string, 0)
		for dependencyID := range allDependencies(workspace.Plugins[id].Manifest) {
			if _, ok := selected[dependencyID]; ok {
				dependencies = append(dependencies, dependencyID)
			}
		}
		sort.Strings(dependencies)
		for _, dependencyID := range dependencies {
			visit(dependencyID)
		}
		order = append(order, id)
	}
	ids := make([]string, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		visit(id)
	}
	return order
}
