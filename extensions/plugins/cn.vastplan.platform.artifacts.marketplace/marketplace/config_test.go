package marketplace

import (
	"testing"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
)

func TestConfigAllowsMultipleHTTPSMarketsAndOrdersByPriority(t *testing.T) {
	config := Config{Sources: []SourceConfig{
		{ID: "partner", Label: "合作市场", URL: "https://partner.example", Priority: 20},
		{ID: "enterprise", Label: "企业市场", URL: "https://market.example", Priority: 10},
	}}
	if err := config.Validate(); err != nil {
		t.Fatalf("验证配置: %v", err)
	}
	ordered := config.normalized()
	if ordered[0].ID != "enterprise" || ordered[1].ID != "partner" {
		t.Fatalf("优先级排序错误: %#v", ordered)
	}
}

func TestConfigRejectsUnsafeURLsAndUnboundCredentials(t *testing.T) {
	cases := []SourceConfig{
		{ID: "remote", Label: "远端", URL: "http://market.example"},
		{ID: "remote", Label: "远端", URL: "https://user:secret@market.example"},
		{ID: "remote", Label: "远端", URL: "https://market.example/../admin"},
		{ID: "remote", Label: "远端", URL: "https://market.example", CredentialRef: &commonv1.ManagedCredentialRef{Handle: "credential://managed/market-token", Owner: "other", Purpose: TokenPurpose, Scope: "tenant", Version: 1}},
	}
	for _, source := range cases {
		if err := source.Validate(); err == nil {
			t.Fatalf("应拒绝不安全 source: %#v", source)
		}
	}
	if err := (SourceConfig{ID: "local", Label: "本地", URL: "http://127.0.0.1:18443", AllowInsecureLoopback: true}).Validate(); err != nil {
		t.Fatalf("开发回环地址应允许: %v", err)
	}
	if err := (SourceConfig{ID: "prefixed", Label: "路径市场", URL: "https://market.example/plugins"}).Validate(); err != nil {
		t.Fatalf("规范 URL path 应允许: %v", err)
	}
	if err := (SourceConfig{ID: "platform", Label: "平台目录", URL: "vastplan://platform.artifacts.repository"}).Validate(); err != nil {
		t.Fatalf("内部平台 Catalog URL 应允许: %v", err)
	}
}

func sixtyFourA() string { return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" }
