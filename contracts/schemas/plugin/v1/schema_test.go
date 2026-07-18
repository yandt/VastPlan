package pluginv1

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
)

func TestParseManifest_ExistingPluginsConform(t *testing.T) {
	pluginsDir := filepath.Join("..", "..", "..", "..", "extensions", "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		t.Fatalf("读取示例插件目录失败: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(pluginsDir, entry.Name(), "vastplan.plugin.json"))
		if err != nil {
			t.Fatalf("读取 %s 清单失败: %v", entry.Name(), err)
		}
		manifest, err := ParseManifest(raw)
		if err != nil {
			t.Errorf("%s 清单应符合 Schema: %v", entry.Name(), err)
			continue
		}
		if manifest.ID != entry.Name() {
			t.Errorf("清单 ID=%q，应与目录名 %q 一致", manifest.ID, entry.Name())
		}
	}
}

func TestParseManifest_RejectsUnknownField(t *testing.T) {
	raw := []byte(`{"id":"com.example.demo","name":"demo","description":"demo","version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}},"unexpected":true}`)
	if _, err := ParseManifest(raw); err == nil {
		t.Fatal("未知字段必须被 Schema 拒绝")
	}
}

func TestParseManifest_BindsFirstPartyNamespaceToPublisher(t *testing.T) {
	base := `{"id":%q,"name":"demo","description":"demo","version":"1.0.0","publisher":%q,"engines":{"backend":"^1.0"},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}}`
	for name, values := range map[string][2]string{
		"第三方抢占首方命名空间": {"com.vastplan.platform.security.fake", "example"},
		"首方使用外部命名空间":  {"com.example.platform.security.fake", "vastplan"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(fmt.Sprintf(base, values[0], values[1]))); err == nil {
				t.Fatal("插件 ID 命名空间必须与发布者绑定")
			}
		})
	}
}

func TestParseManifest_LicenseFieldsArePaired(t *testing.T) {
	base := `{"id":"com.example.demo","name":"demo","description":"demo","version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}%s}`
	for name, fields := range map[string]string{
		"仅 license":     `,"license":"Apache-2.0"`,
		"仅 licenseFile": `,"licenseFile":"LICENSE"`,
		"仅 noticeFile":  `,"noticeFile":"NOTICE"`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(fmt.Sprintf(base, fields))); err == nil {
				t.Fatal("许可证标识与文本路径必须成对声明")
			}
		})
	}
	manifest, err := ParseManifest([]byte(fmt.Sprintf(base, `,"license":"Apache-2.0","licenseFile":"LICENSE","noticeFile":"NOTICE"`)))
	if err != nil {
		t.Fatalf("成对的许可证字段应通过校验: %v", err)
	}
	if manifest.License != "Apache-2.0" || manifest.LicenseFile != "LICENSE" || manifest.NoticeFile != "NOTICE" {
		t.Fatalf("许可证字段解析错误: %+v", manifest)
	}
}

func TestParseManifest_DesignSystemContributionIsClosedAndComplete(t *testing.T) {
	base := `{
  "id":"com.vastplan.foundation.frontend.design-system.test","name":"test","description":"test","version":"1.0.0","publisher":"vastplan",
  "engines":{"frontend":"^1.0"},"activation":["onStartup"],"entry":{"frontend":"frontend/remoteEntry.js"},
  "contributes":{"frontend":{"designSystems":[%s]}}
}`
	valid := `{"id":"ui.design-system","uiContract":"^1.0.0","framework":"test-ui","capabilities":["layout","menu","overlay","form","data","feedback","theme"]}`
	if _, err := ParseManifest([]byte(fmt.Sprintf(base, valid))); err != nil {
		t.Fatalf("完整的设计系统贡献应通过校验: %v", err)
	}
	missing := `{"id":"ui.design-system","uiContract":"^1.0.0","framework":"test-ui","capabilities":["layout","menu","overlay","form","data","feedback"]}`
	if _, err := ParseManifest([]byte(fmt.Sprintf(base, missing))); err == nil {
		t.Fatal("缺少基础 UI 能力的设计系统贡献必须被拒绝")
	}
	unknown := `{"id":"ui.design-system","uiContract":"^1.0.0","framework":"test-ui","capabilities":["layout","menu","overlay","form","data","feedback","theme"],"rawFrameworkToken":true}`
	if _, err := ParseManifest([]byte(fmt.Sprintf(base, unknown))); err == nil {
		t.Fatal("设计系统 descriptor 的未知字段必须被拒绝")
	}
}

func TestParseManifest_CrossPlatformInteractionContributions(t *testing.T) {
	base := `{
  "id":"com.vastplan.foundation.mobile.native-shell","name":"native shell","description":"cross-platform interaction fixture","version":"1.0.0","publisher":"vastplan",
  "engines":{"runner":"^1.0","mobile":"^1.0"},"activation":["onStartup"],"entry":{"runner":"runner/main","mobile":"mobile/main"},
  "contributes":{
    "runner":{"interactions":[{"id":"foundation.runner.interaction","interactionContract":"^1.0.0","kinds":["approval","form"],"allowedSurfaces":["frontend","mobile"]}]},
    "mobile":{"uiAdapters":[%s]}
  }
}`
	valid := `{"id":"mobile.ui-adapter","uiContract":"^1.0.0","framework":"swiftui","capabilities":["form","approval","feedback"]}`
	if _, err := ParseManifest([]byte(fmt.Sprintf(base, valid))); err != nil {
		t.Fatalf("跨端交互贡献应通过校验: %v", err)
	}
	invalid := `{"id":"mobile.other","uiContract":"^1.0.0","framework":"swiftui","capabilities":["form"]}`
	if _, err := ParseManifest([]byte(fmt.Sprintf(base, invalid))); err == nil {
		t.Fatal("移动 UI 适配器必须使用保留的 mobile.ui-adapter ID")
	}
}

func TestValidateDescriptor_RejectsInvalidHookPhase(t *testing.T) {
	err := ValidateDescriptor("hook", []byte(`{"point":"invoke","phase":"later"}`))
	if err == nil {
		t.Fatal("非法 hook phase 必须被 Schema 拒绝")
	}
}

func TestValidateDescriptor_BackendPublicCatalog(t *testing.T) {
	valid := map[string]string{
		"tool.package":       `{"title":"工具","subcommands":[{"name":"run","description":"运行"}]}`,
		"agent":              `{"systemPrompt":"你是助手","tools":[{"extensionPoint":"tool.package","capability":"demo.tool","operation":"run"}]}`,
		"api.route":          `{"service_role":"backend","method":"POST","path":"/v1/demo","auth":"session"}`,
		"permission.checker": `{"applies":{"caller":"CALLER_KIND_*"}}`,
		"event.sink":         `{"subscribe":["task.*"]}`,
		"hook":               `{"point":"invoke","phase":"before"}`,
		"runner.capability":  `{"service_role":"rs","kind":"process.exec","params":{"sandbox":true}}`,
	}
	for point, descriptor := range valid {
		t.Run(point, func(t *testing.T) {
			if err := ValidateDescriptor(point, []byte(descriptor)); err != nil {
				t.Fatalf("公开扩展点 descriptor 应通过校验: %v", err)
			}
		})
	}
}

func TestDescriptorSchema_PublicCatalogMatchesKernelConstants(t *testing.T) {
	var schema struct {
		Properties struct {
			ExtensionPoint struct {
				Enum []string `json:"enum"`
			} `json:"extensionPoint"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(descriptorSchemaJSON, &schema); err != nil {
		t.Fatalf("解析内置 descriptor Schema 失败: %v", err)
	}
	want := extpoint.BackendPluginPoints()
	got := append([]string(nil), schema.Properties.ExtensionPoint.Enum...)
	sort.Strings(want)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Schema 与内核公开扩展点目录漂移: got=%v want=%v", got, want)
	}
}

func TestValidateDescriptor_RejectsUnpublishedOrInvalidPoint(t *testing.T) {
	tests := map[string]struct {
		point      string
		descriptor string
	}{
		"未知扩展点":       {point: "future.point", descriptor: `{"vendorField":"kept"}`},
		"内核内部能力":      {point: "kernel.service", descriptor: `{"title":"伪造服务"}`},
		"agent 缺系统提示": {point: "agent", descriptor: `{}`},
		"API 路径非法":    {point: "api.route", descriptor: `{"service_role":"backend","method":"GET","path":"relative","auth":"session"}`},
		"Runner 角色非法": {point: "runner.capability", descriptor: `{"service_role":"backend","kind":"process.exec"}`},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDescriptor(test.point, []byte(test.descriptor)); err == nil {
				t.Fatal("未发布或非法 descriptor 必须 fail-closed")
			}
		})
	}
}

func TestParseManifest_BackendDescriptorsAreClosed(t *testing.T) {
	valid := []byte(`{
		"id":"com.example.closed-contract","name":"closed","description":"closed contract",
		"version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},
		"contributes":{"backend":{
			"agents":[{"id":"demo.agent","service_role":"backend","systemPrompt":"你是助手","tools":[{"extensionPoint":"tool.package","capability":"demo.tool"}]}],
			"apiRoutes":[{"id":"demo.api","service_role":"backend","method":"GET","path":"/v1/demo","auth":"session"}],
			"runnerCapabilities":[{"id":"demo.exec","service_role":"rs","kind":"process.exec"}]
		}}
	}`)
	if _, err := ParseManifest(valid); err != nil {
		t.Fatalf("完整 Backend descriptor 清单应通过: %v", err)
	}

	missingContract := []byte(`{
		"id":"com.example.open-contract","name":"open","description":"open contract",
		"version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},
		"contributes":{"backend":{"agents":[{"id":"demo.agent","service_role":"backend"}]}}
	}`)
	if _, err := ParseManifest(missingContract); err == nil {
		t.Fatal("缺少 agent 核心契约字段的清单必须被拒绝")
	}
}

func TestParseManifest_BackendExecutionIsLanguageNeutralAndBackwardCompatible(t *testing.T) {
	legacy, err := ParseManifest([]byte(`{
  "id":"com.example.legacy","name":"legacy","description":"legacy","version":"1.0.0","publisher":"example",
  "engines":{"backend":"^1.0"},"activation":["onStartup"],"entry":{"backend":"backend/main"},
  "contributes":{"backend":{"tools":[{"id":"example.tool","service_role":"backend"}]}}
}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := BackendExecutionContract(legacy); got.Driver != "native" || got.MinimumIsolation != "trusted-process" {
		t.Fatalf("旧清单执行契约应保持 native 兼容: %+v", got)
	}

	python, err := ParseManifest([]byte(`{
  "id":"com.example.python","name":"python","description":"python","version":"1.0.0","publisher":"example",
  "engines":{"backend":"^1.0"},
  "execution":{"backend":{"driver":"python","args":["--mode","worker"],"requirements":{"python":">=3.11"},
    "platforms":["linux/amd64","darwin/arm64"],"minimumIsolation":"process-sandbox","features":["channel.cancel.v1"],
		"dynamicGo":{"entry":"backend/plugin.so","abi":"vastplan.dynamic-go.v1","fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","required":true}}},
  "activation":["onStartup"],"entry":{"backend":"backend/main.py"},
  "contributes":{"backend":{"tools":[{"id":"example.python","service_role":"backend"}]}}
}`))
	if err != nil {
		t.Fatal(err)
	}
	got := BackendExecutionContract(python)
	if got.Driver != "python" || got.MinimumIsolation != "process-sandbox" || got.Requirements["python"] != ">=3.11" || len(got.Args) != 2 {
		t.Fatalf("python 执行契约解析错误: %+v", got)
	}
	if got.DynamicGo == nil || got.DynamicGo.Entry != "backend/plugin.so" || got.DynamicGo.ABI != "vastplan.dynamic-go.v1" || len(got.DynamicGo.Fingerprint) != 64 || !got.DynamicGo.Required {
		t.Fatalf("dynamic-go 执行契约解析错误: %+v", got.DynamicGo)
	}

	invalid := []byte(`{
  "id":"com.example.invalid","name":"invalid","description":"invalid","version":"1.0.0","publisher":"example",
  "engines":{"backend":"^1.0"},"execution":{"backend":{"driver":"bad driver"}},
  "activation":["onStartup"],"entry":{"backend":"backend/main"},
  "contributes":{"backend":{"tools":[{"id":"example.invalid","service_role":"backend"}]}}
}`)
	if _, err := ParseManifest(invalid); err == nil {
		t.Fatal("非法运行驱动标识必须被 Schema 拒绝")
	}
}

func TestParseManifest_RuntimePolicies(t *testing.T) {
	local := []byte(`{
		"id":"com.example.local","name":"local","description":"local",
		"version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},
		"runtime":{"instancePolicy":"per-kernel","stateModel":"local-ephemeral","visibility":"local","routing":"direct"},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},
		"contributes":{"backend":{"tools":[{"id":"local.tool","service_role":"backend","title":"local","subcommands":[]}]}}
	}`)
	manifest, err := ParseManifest(local)
	if err != nil {
		t.Fatalf("本地 runtime 策略应通过: %v", err)
	}
	contributions, err := BackendRuntimeContributions(manifest)
	if err != nil || len(contributions) != 1 {
		t.Fatalf("本地贡献规范化失败: %v %+v", err, contributions)
	}
	if contributions[0].Visibility != "local" || contributions[0].Routing != "direct" {
		t.Fatalf("本地贡献策略错误: %+v", contributions[0])
	}

	invalid := []byte(`{
		"id":"com.example.invalid","name":"invalid","description":"invalid",
		"version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},
		"runtime":{"instancePolicy":"per-kernel","stateModel":"local-ephemeral","visibility":"cluster","routing":"queue"},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}
	}`)
	if _, err := ParseManifest(invalid); err == nil {
		t.Fatal("per-kernel 与 cluster/queue 冲突时必须拒绝")
	}
}

func TestParseManifest_RuntimeRequirements(t *testing.T) {
	raw := []byte(`{
		"id":"com.example.consumer","name":"consumer","description":"consumer",
		"version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},
		"runtime":{"instancePolicy":"active-active","stateModel":"external-shared","visibility":"cluster","routing":"queue",
			"requires":[{"capability":"platform.database","version":"^1.0.0","scope":"remote","kind":"strong","ready":"readiness","failurePolicy":"retry","logicalService":"platform.database","routingDomain":"core"}]},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},
		"contributes":{"backend":{"tools":[{"id":"consumer.tool","service_role":"backend","title":"consumer","subcommands":[]}]}}
	}`)
	manifest, err := ParseManifest(raw)
	if err != nil || manifest.Runtime == nil || len(manifest.Runtime.Requires) != 1 {
		t.Fatalf("runtime requires 应通过并被解析: manifest=%+v err=%v", manifest, err)
	}
	invalid := []byte(`{
		"id":"com.example.invalid-requirement","name":"invalid","description":"invalid",
		"version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},
		"runtime":{"instancePolicy":"active-active","stateModel":"external-shared","visibility":"cluster","routing":"queue",
			"requires":[{"capability":"platform.database","scope":"remote","kind":"strong","ready":"readiness","failurePolicy":"unknown"}]},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}
	}`)
	if _, err := ParseManifest(invalid); err == nil {
		t.Fatal("非法 runtime failurePolicy 必须拒绝")
	}
}

func TestParseManifest_BackendStateMigrationContract(t *testing.T) {
	valid := []byte(`{
		"id":"com.example.stateful","name":"stateful","description":"stateful plugin",
		"version":"2.0.0","publisher":"example","engines":{"backend":"^1.0"},
		"state":{"backend":{"format":"com.example.stateful.data","formatVersion":2,
			"migration":{"protocol":"lifecycle.v1","from":[{"format":"com.example.stateful.data","formatVersion":1}]}}},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},
		"contributes":{"backend":{"tools":[{"id":"demo.tool","service_role":"backend"}]}}
	}`)
	manifest, err := ParseManifest(valid)
	if err != nil {
		t.Fatalf("合法状态迁移契约应通过: %v", err)
	}
	if manifest.State == nil || manifest.State.Backend == nil || manifest.State.Backend.FormatVersion != 2 {
		t.Fatalf("状态契约未解析: %+v", manifest.State)
	}

	invalid := []string{
		`"state":{"backend":{"format":"com.example.stateful.data","formatVersion":0}}`,
		`"state":{"backend":{"format":"invalid","formatVersion":1}}`,
		`"state":{"backend":{"format":"com.example.stateful.data","formatVersion":2,"migration":{"protocol":"future.v2","from":[{"format":"com.example.stateful.data","formatVersion":1}]}}}`,
		`"state":{"backend":{"format":"com.example.stateful.data","formatVersion":2,"migration":{"protocol":"lifecycle.v1","from":[]}}}`,
	}
	base := `{"id":"com.example.stateful","name":"stateful","description":"stateful plugin","version":"2.0.0","publisher":"example","engines":{"backend":"^1.0"},%s,"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[{"id":"demo.tool","service_role":"backend"}]}}}`
	for _, state := range invalid {
		if _, err := ParseManifest([]byte(fmt.Sprintf(base, state))); err == nil {
			t.Errorf("非法状态契约必须被拒绝: %s", state)
		}
	}
}
