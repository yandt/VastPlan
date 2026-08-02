package main

import (
	"testing"
	"time"

	policy "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.authorization-policy/authorizationpolicy"
)

func TestLoadBootstrapReconciliation(t *testing.T) {
	if selected, err := loadBootstrapReconciliation(""); err != nil {
		t.Fatal(err)
	} else if _, ok := selected.(policy.DisabledBootstrapReconciliation); !ok {
		t.Fatalf("普通启动必须选择禁用策略: %T", selected)
	}
	if selected, err := loadBootstrapReconciliation("seed-owned"); err != nil {
		t.Fatal(err)
	} else if _, ok := selected.(policy.SeedOwnedBootstrapReconciliation); !ok {
		t.Fatalf("显式 bootstrap 必须选择 Seed-owned 策略: %T", selected)
	}
	if _, err := loadBootstrapReconciliation("unsafe"); err == nil {
		t.Fatal("未知协调策略必须被拒绝")
	}
}

func TestLeasePolicyComesOnlyFromStartupConfiguration(t *testing.T) {
	configuration := runtimeConfiguration{
		TenantID:        "local",
		SnapshotLease:   snapshotLeaseConfig{Audiences: []string{"development:local", "portal:local:operations"}, TTLSeconds: 86400, RenewalLeadSeconds: 21600},
		ManagedBindings: managedBindingConfig{Creators: []string{"seed-authority"}, TTLSeconds: 86400, RenewalLeadSeconds: 21600},
	}
	lease, err := leasePolicyFromConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if lease.SnapshotTTL() != 24*time.Hour || lease.RenewalLead() != 6*time.Hour || len(lease.Audiences()) != 2 {
		t.Fatalf("启动配置未完整注入 Lease Policy: ttl=%s lead=%s audiences=%v", lease.SnapshotTTL(), lease.RenewalLead(), lease.Audiences())
	}
	configuration.SnapshotLease.RenewalLeadSeconds = configuration.SnapshotLease.TTLSeconds
	if _, err := leasePolicyFromConfiguration(configuration); err == nil {
		t.Fatal("无效租约配置必须在组合根注入时失败")
	}
}
