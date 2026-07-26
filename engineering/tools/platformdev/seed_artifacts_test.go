package main

import (
	"context"
	"path/filepath"
	"testing"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
)

func TestSeedArtifactSelectionIsExactConfigurationClosure(t *testing.T) {
	root := repositoryRoot(t)
	selection, err := loadSeedArtifactSelection(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.refs) != 26 {
		t.Fatalf("当前平台 Seed 应只包含 26 个精确插件引用，实际为 %d", len(selection.refs))
	}
	for _, required := range []string{
		"cn.vastplan.foundation.security.bootstrap-policy",
		"cn.vastplan.foundation.frontend.runtime.engine.react",
		"cn.vastplan.foundation.frontend.render.adapter.antd",
		"cn.vastplan.platform.artifacts.repository",
	} {
		if !selection.contains(required) {
			t.Fatalf("Seed 缺少必要插件 %s", required)
		}
	}
	for _, ordinary := range []string{
		"cn.vastplan.demo-audit",
		"cn.vastplan.hello-world",
		"cn.vastplan.product.developer.workbench-gallery",
		"cn.vastplan.test.runtime.node-worker-hello",
		"cn.vastplan.python-hello",
	} {
		if selection.contains(ordinary) {
			t.Fatalf("普通开发插件不得进入 Seed: %s", ordinary)
		}
	}
}

func TestPlatformManagementPortalApplicationDoesNotEmbedExamples(t *testing.T) {
	application, err := frontendcompositionv1.ParseApplicationCompositionFile(
		filepath.Join(repositoryRoot(t), "engineering", "deploy", "portal-application-composition.json"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(application.Plugins) != 0 {
		t.Fatalf("平台管理中心最小 Application 不得隐式启用普通插件: %+v", application.Plugins)
	}
	if application.Branding["title"] != "VastPlan 平台管理中心" {
		t.Fatalf("平台管理中心品牌标题缺失: %+v", application.Branding)
	}
}

func TestSeedPackageSpecsExcludeOrdinaryPlugins(t *testing.T) {
	root := repositoryRoot(t)
	r := &runtime{options: options{root: root, stateRoot: t.TempDir()}}
	specs, err := r.seedPackageSpecs()
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range specs {
		if spec.id == "cn.vastplan.demo-audit" || spec.id == "cn.vastplan.hello-world" {
			t.Fatalf("普通插件进入 Seed 打包计划: %s", spec.id)
		}
	}
}

func TestGoBuildPlanOnlyContainsSeedPlugins(t *testing.T) {
	root := repositoryRoot(t)
	r := &runtime{options: options{root: root, stateRoot: t.TempDir()}}
	goCache := filepath.Join(r.options.stateRoot, "go-cache")
	plan, err := r.computeGoBuildPlan(context.Background(), "test-go", goCache)
	if err != nil {
		t.Fatal(err)
	}
	for _, plugin := range plan.Plugins {
		if !r.seedArtifacts.contains(plugin.ID) {
			t.Fatalf("Go 构建计划包含非 Seed 插件: %s", plugin.ID)
		}
		if plugin.ID == "cn.vastplan.demo-audit" {
			t.Fatal("demo-audit 不得随平台 Seed 构建")
		}
	}
}

func TestValidateExactSeedRefsRejectsMissingAndExtraArtifacts(t *testing.T) {
	expected := []artifactrepository.Ref{{PluginID: "cn.vastplan.a", Version: "1.0.0", Channel: "stable"}}
	if err := validateExactSeedRefs("test", expected, expected); err != nil {
		t.Fatal(err)
	}
	if err := validateExactSeedRefs("test", expected, nil); err == nil {
		t.Fatal("缺少 Seed 制品必须失败")
	}
	extra := append(append([]artifactrepository.Ref{}, expected...), artifactrepository.Ref{PluginID: "cn.vastplan.demo", Version: "1.0.0", Channel: "stable"})
	if err := validateExactSeedRefs("test", expected, extra); err == nil {
		t.Fatal("额外普通插件进入 Seed 仓库必须失败")
	}
}
