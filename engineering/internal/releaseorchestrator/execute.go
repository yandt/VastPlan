package releaseorchestrator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"cdsoft.com.cn/VastPlan/engineering/internal/plugindev"
)

type DevelopmentExecutionOptions struct {
	StateRoot string
	StatusURL string
	GoCache   string
	Logf      func(string, ...any)
}

type DevelopmentReleaseResult struct {
	PluginID     string `json:"pluginId"`
	Version      string `json:"version"`
	SourceDigest string `json:"sourceDigest"`
	PackageFile  string `json:"packageFile"`
}

func ExecuteDevelopmentRelease(ctx context.Context, repositoryRoot string, spec ReleaseSpec, options DevelopmentExecutionOptions) (ReleasePlan, []DevelopmentReleaseResult, error) {
	if spec.Mode != ReleaseModeDevelopment {
		return ReleasePlan{}, nil, errors.New("生产 Release Spec 只能生成审批计划，不能执行发布")
	}
	plan, err := PrepareRelease(repositoryRoot, spec)
	if err != nil {
		return ReleasePlan{}, nil, err
	}
	if options.StateRoot == "" {
		options.StateRoot = filepath.Join(repositoryRoot, ".vastplan", "dev-platform")
	}
	if !filepath.IsAbs(options.StateRoot) {
		options.StateRoot = filepath.Join(repositoryRoot, options.StateRoot)
	}
	if options.StatusURL == "" {
		options.StatusURL = "http://127.0.0.1:18080/__vastplan_dev/status"
	}
	if options.GoCache == "" {
		options.GoCache = filepath.Join(options.StateRoot, "go-cache")
	}
	if options.Logf == nil {
		options.Logf = func(string, ...any) {}
	}
	if err := os.MkdirAll(options.StateRoot, 0o700); err != nil {
		return ReleasePlan{}, nil, err
	}
	requests := make(map[string]ReleasePluginRequest, len(spec.Plugins))
	for _, request := range spec.Plugins {
		requests[request.ID] = request
	}
	results := make([]DevelopmentReleaseResult, 0, len(plan.ExecutionOrder))
	for index, pluginID := range plan.ExecutionOrder {
		request := requests[pluginID]
		plugin, err := plugindev.Discover(repositoryRoot, pluginID)
		if err != nil {
			return plan, results, err
		}
		if err := validateDevelopmentTargets(plugin, request); err != nil {
			return plan, results, err
		}
		digest, err := plugindev.SourceDigest(repositoryRoot, plugin)
		if err != nil {
			return plan, results, err
		}
		generation := uint64(index + 1)
		options.Logf("构建发布候选 plugin=%s generation=%d digest=%s", pluginID, generation, digest[:16])
		candidate, err := (plugindev.CommandBuilder{
			RepositoryRoot: repositoryRoot, StateRoot: options.StateRoot, GoCache: options.GoCache,
		}).Build(ctx, plugin, digest, generation)
		if err != nil {
			return plan, results, err
		}
		if err := publishDevelopmentCandidate(ctx, repositoryRoot, candidate, request, options); err != nil {
			return plan, results, err
		}
		results = append(results, DevelopmentReleaseResult{
			PluginID: candidate.PluginID, Version: candidate.Version,
			SourceDigest: candidate.SourceDigest, PackageFile: candidate.PackageFile,
		})
	}
	return plan, results, nil
}

func validateDevelopmentTargets(plugin plugindev.Spec, request ReleasePluginRequest) error {
	if plugin.Entry != "" && strings.TrimSpace(request.BackendTarget) == "" && strings.TrimSpace(request.BackendBinding) == "" {
		return fmt.Errorf("插件 %s 包含 Backend，Release Spec 必须提供 backendTarget 或 backendBinding", plugin.ID)
	}
	if plugin.FrontendEntry != "" && strings.TrimSpace(request.FrontendTarget) == "" && strings.TrimSpace(request.FrontendBinding) == "" {
		return fmt.Errorf("插件 %s 包含 Frontend，Release Spec 必须提供 frontendTarget 或 frontendBinding", plugin.ID)
	}
	for _, face := range []string{"desktop", "mobile"} {
		if strings.TrimSpace(plugin.Manifest.Entry[face]) != "" {
			return fmt.Errorf("插件 %s 的 %s 候选执行尚未接入统一 Test Release", plugin.ID, face)
		}
	}
	return nil
}

func publishDevelopmentCandidate(ctx context.Context, repositoryRoot string, candidate plugindev.Candidate, request ReleasePluginRequest, options DevelopmentExecutionOptions) error {
	arguments := []string{
		"run", "./engineering/tools/testpublish", "-package", candidate.PackageFile,
		"-channel", "workspace", "-state-root", options.StateRoot, "-status-url", options.StatusURL,
	}
	if request.BackendTarget != "" {
		arguments = append(arguments, "-backend-target", request.BackendTarget)
	}
	if request.BackendBinding != "" {
		arguments = append(arguments, "-backend-binding", request.BackendBinding)
	}
	if request.FrontendTarget != "" {
		arguments = append(arguments, "-frontend-target", request.FrontendTarget)
	}
	if request.FrontendBinding != "" {
		arguments = append(arguments, "-frontend-binding", request.FrontendBinding)
	}
	if request.FrontendScope != "" {
		arguments = append(arguments, "-frontend-scope", request.FrontendScope)
	}
	command := exec.CommandContext(ctx, "go", arguments...)
	command.Dir = repositoryRoot
	command.Env = append([]string(nil), os.Environ()...)
	if options.GoCache != "" {
		command.Env = append(command.Env, "GOCACHE="+options.GoCache)
	}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Run(); err != nil {
		return fmt.Errorf("发布并激活 %s: %w\n%s", candidate.PluginID, err, strings.TrimSpace(output.String()))
	}
	if strings.TrimSpace(output.String()) != "" {
		options.Logf("%s", strings.TrimSpace(output.String()))
	}
	return nil
}
