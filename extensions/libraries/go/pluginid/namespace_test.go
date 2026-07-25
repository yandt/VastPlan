package pluginid

import "testing"

func TestParseFirstPartyClassifiesLayerDomainAndComponent(t *testing.T) {
	namespace, err := ParseFirstParty("cn.vastplan.foundation.security.bootstrap-policy")
	if err != nil {
		t.Fatal(err)
	}
	if namespace.Layer != LayerFoundation || namespace.Domain() != "security" || namespace.CategoryPath() != "security" || namespace.Component != "bootstrap-policy" {
		t.Fatalf("命名空间分类错误: %+v", namespace)
	}
	if !namespace.IsPlatformBootstrapReader() {
		t.Fatal("foundation 插件应属于平台自举读取层")
	}
}

func TestParseFirstPartyPreservesMultiLevelFunctionalCategory(t *testing.T) {
	namespace, err := ParseFirstParty("cn.vastplan.platform.data.relational.connection-manager")
	if err != nil {
		t.Fatal(err)
	}
	if namespace.Domain() != "data" || namespace.CategoryPath() != "data.relational" || namespace.Component != "connection-manager" {
		t.Fatalf("多级功能分类错误: %+v", namespace)
	}
}

func TestParseFirstPartyRejectsFlatUnknownAndForeignNamespaces(t *testing.T) {
	for _, id := range []string{
		"cn.vastplan.bootstrap-policy",
		"cn.vastplan.unknown.security.policy",
		"com.example.foundation.security.policy",
	} {
		if _, err := ParseFirstParty(id); err == nil {
			t.Fatalf("应拒绝未分类命名空间 %q", id)
		}
	}
}

func TestValidatePublisherOwnershipBindsFirstPartyBothWays(t *testing.T) {
	for _, pair := range [][2]string{
		{"cn.vastplan.platform.data.database", "example"},
		{"com.example.platform.data.database", "vastplan"},
	} {
		if err := ValidatePublisherOwnership(pair[0], pair[1]); err == nil {
			t.Fatalf("应拒绝命名空间与发布者不匹配: %q / %q", pair[0], pair[1])
		}
	}
	if err := ValidatePublisherOwnership("cn.vastplan.platform.data.database", "vastplan"); err != nil {
		t.Fatalf("合法首方身份应通过: %v", err)
	}
}

func TestClassifyManagementUsesVerifiedIdentity(t *testing.T) {
	tests := []struct {
		id        string
		publisher string
		want      ManagementClass
	}{
		{"cn.vastplan.foundation.security.bootstrap-policy", "vastplan", ManagementPlatform},
		{"cn.vastplan.platform.data.relational.connection-manager", "vastplan", ManagementPlatform},
		{"cn.vastplan.product.agent.designer", "vastplan", ManagementApplication},
		{"cn.vastplan.integration.database.postgresql", "vastplan", ManagementApplication},
		{"cn.vastplan.example.demo.hello-world", "vastplan", ManagementDevelopment},
		{"cn.vastplan.hello-world", "vastplan", ManagementDevelopment},
		{"com.example.tool", "example", ManagementApplication},
	}
	for _, test := range tests {
		got, err := ClassifyManagement(test.id, test.publisher)
		if err != nil || got != test.want {
			t.Fatalf("ClassifyManagement(%q, %q) = %q, %v; want %q", test.id, test.publisher, got, err, test.want)
		}
	}
}

func TestClassifyManagementRejectsPublisherNamespaceSpoofing(t *testing.T) {
	if _, err := ClassifyManagement("cn.vastplan.platform.security.policy", "attacker"); err == nil {
		t.Fatal("首方命名空间冒用必须拒绝")
	}
	if _, err := ClassifyManagement("com.attacker.tool", "vastplan"); err == nil {
		t.Fatal("首方发布者使用外部命名空间必须拒绝")
	}
}
