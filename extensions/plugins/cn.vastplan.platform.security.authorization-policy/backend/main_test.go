package main

import (
	"testing"

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
