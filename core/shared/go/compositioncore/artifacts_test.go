package compositioncore

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type artifactReader map[string]pluginv1.Artifact

func (r artifactReader) Read(ref pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error) {
	artifact, ok := r[ref.PluginID+"@"+ref.Version+"/"+ref.Channel]
	if !ok {
		return pluginv1.Artifact{}, nil, errors.New("not found")
	}
	return artifact, nil, nil
}

func testArtifact(id, publisher string) pluginv1.Artifact {
	manifest := []byte(fmt.Sprintf(`{
		"id":%q,"name":"plugin","description":"plugin","version":"1.0.0","publisher":%q,
		"engines":{"backend":"^0.1"},"activation":["onStartup"],"entry":{"backend":"backend/main"},
		"contributes":{"backend":{"tools":[{"id":"tool.%s","service_role":"backend","title":"tool","subcommands":[]}]}}
	}`, id, publisher, strings.ReplaceAll(id, ".", "-")))
	return pluginv1.Artifact{PluginID: id, Version: "1.0.0", Channel: "stable", SHA256: strings.Repeat("a", 64), Manifest: manifest}
}

func TestResolveRefEnforcesSharedOriginPolicy(t *testing.T) {
	platformID := "cn.vastplan.foundation.security.policy"
	reader := artifactReader{platformID + "@1.0.0/stable": testArtifact(platformID, "vastplan")}
	ref := Selection{ID: platformID, Version: "1.0.0"}
	if _, err := ResolveRef(ref, compositioncommonv1.OriginApplication, map[string]ResolvedArtifact{}, reader, Options{}); err == nil {
		t.Fatal("应用来源不得选择平台管理插件")
	}
	if _, err := ResolveRef(ref, compositioncommonv1.OriginPlatformProfile, map[string]ResolvedArtifact{}, reader, Options{}); err != nil {
		t.Fatalf("平台来源应允许已验证的平台插件: %v", err)
	}
	if _, err := ResolveRef(ref, "unknown", map[string]ResolvedArtifact{}, reader, Options{}); err == nil {
		t.Fatal("未知来源必须拒绝")
	}
}

func TestResolveRefDistinguishesDigestCreationFromLockConsumption(t *testing.T) {
	id := "com.example.agent"
	artifact := testArtifact(id, "example")
	reader := artifactReader{id + "@1.0.0/stable": artifact}

	discovered, err := ResolveRef(Selection{ID: id, Version: "1.0.0"}, compositioncommonv1.OriginApplication, map[string]ResolvedArtifact{}, reader, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if discovered.Selection.SHA256 != artifact.SHA256 {
		t.Fatalf("空摘要应由解析阶段产生: %+v", discovered.Selection)
	}

	locked := Selection{ID: id, Version: "1.0.0", SHA256: strings.Repeat("b", 64)}
	if _, err := ResolveRef(locked, compositioncommonv1.OriginApplication, map[string]ResolvedArtifact{}, reader, Options{}); err == nil || !strings.Contains(err.Error(), "与解析锁") {
		t.Fatalf("已有摘要必须作为精确锁校验: %v", err)
	}
}

func TestResolveRefRejectsConflictingDigestForRepeatedPlugin(t *testing.T) {
	id := "com.example.agent"
	artifact := testArtifact(id, "example")
	reader := artifactReader{id + "@1.0.0/stable": artifact}
	seen := map[string]ResolvedArtifact{}
	if _, err := ResolveRef(Selection{ID: id, Version: "1.0.0", SHA256: artifact.SHA256}, compositioncommonv1.OriginApplication, seen, reader, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveRef(Selection{ID: id, Version: "1.0.0", SHA256: strings.Repeat("b", 64)}, compositioncommonv1.OriginApplication, seen, reader, Options{}); err == nil || !strings.Contains(err.Error(), "SHA-256 冲突") {
		t.Fatalf("重复插件不得携带冲突摘要: %v", err)
	}
}
