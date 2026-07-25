// Package planner 把受限 Application Intent 编译为可解释、可复验的 Backend 组合方案。
package planner

import (
	"context"
	"fmt"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type Service struct {
	config Config
}

func New(config Config) (*Service, error) {
	normalized, err := config.Normalize()
	if err != nil {
		return nil, err
	}
	return &Service{config: normalized}, nil
}

func (s *Service) Plan(ctx context.Context, repository Repository, request backendcompositionv1.PlanningRequest) (backendcompositionv1.ResolutionReport, error) {
	if s == nil || repository == nil || ctx == nil {
		return backendcompositionv1.ResolutionReport{}, fmt.Errorf("Composition Planner 未初始化")
	}
	intent, err := backendcompositionv1.ValidateApplicationIntent(request.Intent)
	if err != nil {
		return backendcompositionv1.ResolutionReport{}, err
	}
	profile, err := backendcompositionv1.ValidatePlatformProfile(request.PlatformProfile)
	if err != nil {
		return backendcompositionv1.ResolutionReport{}, err
	}
	credentials, err := normalizeConfigurationSnapshot(request.ConfigurationSnapshot)
	if err != nil {
		return backendcompositionv1.ResolutionReport{}, err
	}
	base := backendcompositionv1.ResolutionReport{
		Version:         1,
		Intent:          compositioncommonv1.Ref{ID: intent.ID, Revision: intent.Revision, Digest: intent.Digest()},
		PlatformProfile: compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()},
		Planner: backendcompositionv1.PlannerIdentity{
			Ref:        pluginv1.ArtifactRef{PluginID: PluginID, Version: PluginVersion, Channel: s.config.Channel},
			Capability: backendcompositionv1.PlanningCapability, ConfigurationDigest: s.config.Digest(),
		},
		Features: []backendcompositionv1.ResolvedFeature{}, ProviderBindings: []backendcompositionv1.CapabilityProviderBinding{},
		ServiceGraph:      backendcompositionv1.ServiceDependencyGraph{Nodes: []backendcompositionv1.ServiceDependencyNode{}, Edges: []backendcompositionv1.ServiceDependencyEdge{}},
		ConfigurationPlan: backendcompositionv1.ConfigurationPlan{Items: []backendcompositionv1.ConfigurationPlanItem{}},
		Diagnostics:       []backendcompositionv1.ResolutionDiagnostic{},
	}
	artifacts, err := s.resolveArtifacts(ctx, repository, intent, profile)
	if err != nil {
		return backendcompositionv1.ResolutionReport{}, err
	}
	result, err := s.compile(intent, profile, artifacts, credentials)
	if err != nil {
		base.Status = backendcompositionv1.ResolutionInvalid
		base.Diagnostics = []backendcompositionv1.ResolutionDiagnostic{{Severity: "error", Code: "composition.plan.invalid", Message: err.Error()}}
		return backendcompositionv1.FinalizeResolutionReport(base)
	}
	base.Status = backendcompositionv1.ResolutionResolved
	base.ApplicationComposition = &result.composition
	base.ArtifactLock = &artifacts.lock
	base.Features = result.features
	base.ProviderBindings = result.bindings
	base.ServiceGraph = result.graph
	base.ConfigurationPlan.Items = result.configuration
	base.Diagnostics = result.diagnostics
	if base.Features == nil {
		base.Features = []backendcompositionv1.ResolvedFeature{}
	}
	if base.ProviderBindings == nil {
		base.ProviderBindings = []backendcompositionv1.CapabilityProviderBinding{}
	}
	if base.ConfigurationPlan.Items == nil {
		base.ConfigurationPlan.Items = []backendcompositionv1.ConfigurationPlanItem{}
	}
	if base.Diagnostics == nil {
		base.Diagnostics = []backendcompositionv1.ResolutionDiagnostic{}
	}
	for _, item := range result.configuration {
		if len(item.Missing) > 0 {
			base.Status = backendcompositionv1.ResolutionNeedsConfiguration
			break
		}
	}
	return backendcompositionv1.FinalizeResolutionReport(base)
}

type compileResult struct {
	composition   backendcompositionv1.ApplicationComposition
	features      []backendcompositionv1.ResolvedFeature
	bindings      []backendcompositionv1.CapabilityProviderBinding
	graph         backendcompositionv1.ServiceDependencyGraph
	configuration []backendcompositionv1.ConfigurationPlanItem
	diagnostics   []backendcompositionv1.ResolutionDiagnostic
}
