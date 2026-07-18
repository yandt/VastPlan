package compositioncore

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/schemas/composition/common/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
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
	return pluginv1.Artifact{PluginID: id, Version: "1.0.0", Channel: "stable", Manifest: manifest}
}

func TestVerifyRefEnforcesSharedOriginPolicy(t *testing.T) {
	platformID := "com.vastplan.foundation.security.policy"
	reader := artifactReader{platformID + "@1.0.0/stable": testArtifact(platformID, "vastplan")}
	ref := Selection{ID: platformID, Version: "1.0.0"}
	if err := VerifyRef(ref, compositioncommonv1.OriginApplication, map[string]Selection{}, reader, Options{}); err == nil {
		t.Fatal("应用来源不得选择平台管理插件")
	}
	if err := VerifyRef(ref, compositioncommonv1.OriginPlatformProfile, map[string]Selection{}, reader, Options{}); err != nil {
		t.Fatalf("平台来源应允许已验证的平台插件: %v", err)
	}
	if err := VerifyRef(ref, "unknown", map[string]Selection{}, reader, Options{}); err == nil {
		t.Fatal("未知来源必须拒绝")
	}
}
