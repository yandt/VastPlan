package compositioncore

import "testing"

func TestMergeProtectedConfigAddsServiceBranchesWithoutMutatingInputs(t *testing.T) {
	baseline := map[string]any{"environment_allowlist": map[string]any{"platform.guard": []any{"A"}}}
	service := map[string]any{"environment_allowlist": map[string]any{"app.worker": []any{"B"}}, "plugins": map[string]any{"app.worker": map[string]any{"limit": float64(3)}}}
	merged, err := MergeProtectedConfig(baseline, service)
	if err != nil {
		t.Fatal(err)
	}
	allowlist := merged["environment_allowlist"].(map[string]any)
	if len(allowlist) != 2 || merged["plugins"] == nil {
		t.Fatalf("公共与服务配置没有完整合并: %+v", merged)
	}
	allowlist["platform.guard"] = []any{"changed"}
	if baseline["environment_allowlist"].(map[string]any)["platform.guard"].([]any)[0] != "A" {
		t.Fatal("合并结果不得修改公共基线输入")
	}
}

func TestMergeProtectedConfigRejectsServiceOverride(t *testing.T) {
	baseline := map[string]any{"security": map[string]any{"mode": "enforced"}}
	service := map[string]any{"security": map[string]any{"mode": "disabled"}}
	if _, err := MergeProtectedConfig(baseline, service); err == nil {
		t.Fatal("服务配置不得覆盖公共基线")
	}
}
