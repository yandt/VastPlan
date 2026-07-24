package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
)

func TestMaterializeDevelopmentDeploymentRevisionAdvancesOnSourceChange(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "platform-management-revision.json")
	profile := developmentRevisionTestProfile(12)
	firstDigest := strings.Repeat("a", 64)
	secondDigest := strings.Repeat("b", 64)

	first, revision, err := materializeDevelopmentDeploymentRevision(profile, firstDigest, stateFile)
	if err != nil || revision != 12 || parsedDevelopmentRevision(t, first) != 12 {
		t.Fatalf("首次物化必须使用模板基线 revision: revision=%d err=%v", revision, err)
	}
	repeated, revision, err := materializeDevelopmentDeploymentRevision(profile, firstDigest, stateFile)
	if err != nil || revision != 12 || parsedDevelopmentRevision(t, repeated) != 12 {
		t.Fatalf("相同源重复启动必须幂等: revision=%d err=%v", revision, err)
	}
	changed, revision, err := materializeDevelopmentDeploymentRevision(profile, secondDigest, stateFile)
	if err != nil || revision != 13 || parsedDevelopmentRevision(t, changed) != 13 {
		t.Fatalf("源内容变化必须分配新 revision: revision=%d err=%v", revision, err)
	}
}

func TestPrepareDevelopmentPlatformProfileDoesNotAllocateRevisionDuringStartup(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "platform-management-revision.json")
	profile := developmentRevisionTestProfile(12)
	prepared, err := prepareDevelopmentPlatformProfile(profile, strings.Repeat("a", 64), stateFile, false)
	if err != nil || parsedDevelopmentRevision(t, prepared) != 12 {
		t.Fatalf("零发布启动不得改写 Profile revision: err=%v profile=%s", err, prepared)
	}
	if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
		t.Fatalf("零发布启动不得创建 revision 状态文件: %v", err)
	}
}

func TestMaterializeDevelopmentDeploymentRevisionHonorsNewTemplateFloor(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "platform-management-revision.json")
	if _, _, err := materializeDevelopmentDeploymentRevision(developmentRevisionTestProfile(12), strings.Repeat("a", 64), stateFile); err != nil {
		t.Fatal(err)
	}
	materialized, revision, err := materializeDevelopmentDeploymentRevision(developmentRevisionTestProfile(20), strings.Repeat("b", 64), stateFile)
	if err != nil || revision != 20 || parsedDevelopmentRevision(t, materialized) != 20 {
		t.Fatalf("更高模板 revision 必须成为新下限: revision=%d err=%v", revision, err)
	}
}

func TestMaterializeDevelopmentDeploymentRevisionRejectsCorruptState(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "platform-management-revision.json")
	if err := os.WriteFile(stateFile, []byte(`{"version":1,"sourceDigest":"bad","revision":12}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := materializeDevelopmentDeploymentRevision(developmentRevisionTestProfile(12), strings.Repeat("a", 64), stateFile); err == nil {
		t.Fatal("损坏的 revision 状态必须 fail-closed")
	}
}

func TestPlatformManagementSourceDigestIncludesPortalCatalog(t *testing.T) {
	template, err := os.ReadFile(filepath.Join("..", "..", "deploy", "platform-management-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := os.ReadFile(filepath.Join("..", "..", "deploy", "portal-platform-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	application, err := os.ReadFile(filepath.Join("..", "..", "deploy", "platform-management-application.json"))
	if err != nil {
		t.Fatal(err)
	}
	backendDigest := strings.Repeat("a", 64)
	profile := artifactrepositoryv1.Profile{Version: 1, ID: "local-testing", Protocol: artifactrepositoryv1.ProtocolLocalTest, Endpoint: "unix:///tmp/vastplan/repository.sock", Channels: []string{"testing"}, DevelopmentOnly: true}
	first, err := platformManagementSourceDigest(template, application, catalog, profile, backendDigest)
	if err != nil {
		t.Fatal(err)
	}
	var changed map[string]any
	if err := json.Unmarshal(catalog, &changed); err != nil {
		t.Fatal(err)
	}
	changed["revision"] = changed["revision"].(float64) + 1
	changedRaw, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	second, err := platformManagementSourceDigest(template, application, changedRaw, profile, backendDigest)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("嵌入的 Portal Catalog 变化必须改变开发平台部署指纹")
	}
	third, err := platformManagementSourceDigest(template, application, catalog, profile, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	if first == third {
		t.Fatal("Backend 制品构建输入变化必须改变开发平台部署指纹")
	}
	var changedApplication map[string]any
	if err := json.Unmarshal(application, &changedApplication); err != nil {
		t.Fatal(err)
	}
	changedApplication["revision"] = changedApplication["revision"].(float64) + 1
	changedApplicationRaw, err := json.Marshal(changedApplication)
	if err != nil {
		t.Fatal(err)
	}
	fourth, err := platformManagementSourceDigest(template, changedApplicationRaw, catalog, profile, backendDigest)
	if err != nil {
		t.Fatal(err)
	}
	if first == fourth {
		t.Fatal("种子 Application Composition 变化必须改变开发平台部署指纹")
	}
}

func developmentRevisionTestProfile(revision uint64) []byte {
	return []byte(`{
  "version": 1,
  "revision": ` + jsonNumber(revision) + `,
  "id": "test-profile",
  "target": {"kernel": "backend"},
  "serviceClasses": ["application.backend"],
  "serviceBaselines": [],
  "services": [{"id":"enabled","kind":"service","enabled":true,"service_role":"backend","logical_service":"test.enabled","instance_policy":"per-kernel","state_model":"local-ephemeral","visibility":"local","routing":"direct","replicas":1,"plugins":[{"id":"cn.vastplan.test.enabled","version":"1.0.0","channel":"stable"}]}]
}`)
}

func parsedDevelopmentRevision(t *testing.T, raw []byte) uint64 {
	t.Helper()
	profile, err := backendcompositionv1.ParsePlatformProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	return profile.Revision
}

func jsonNumber(value uint64) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
