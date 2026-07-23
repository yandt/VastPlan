package repositoryruntime

import (
	"encoding/json"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestSupplyChainPolicySecureDefault(t *testing.T) {
	policy := (SupplyChainPolicy{}).normalized()
	if !policy.requiresSBOM("stable") || policy.requiresSBOM("testing") || policy.Validate() != nil {
		t.Fatalf("默认供应链策略无效: %+v", policy)
	}
	manifest := json.RawMessage(`{"id":"com.example.demo","name":"demo","description":"demo","version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}}`)
	if err := policy.admit(pluginv1.Artifact{Channel: "stable", Manifest: manifest}); err == nil {
		t.Fatal("stable 制品缺少 SBOM 必须拒绝")
	}
	if err := policy.admit(pluginv1.Artifact{Channel: "testing", Manifest: manifest}); err != nil {
		t.Fatalf("默认策略不应阻止 testing: %v", err)
	}
}
