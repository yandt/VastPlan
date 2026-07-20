package runtimeidentity_test

import (
	"context"
	"strings"
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/runtimeidentity"
)

func validIdentity() runtimeidentity.Identity {
	return runtimeidentity.Identity{
		PluginID: "cn.vastplan.foundation.data.relational.runtime", Publisher: "vastplan", Version: "0.2.0",
		ArtifactSHA256: strings.Repeat("a", 64), NodeID: "node-a", RuntimeScope: "database-a", InstanceID: "runtime-1",
	}
}

func TestAudienceBindsEveryIdentityField(t *testing.T) {
	identity := validIdentity()
	audience, err := identity.Audience()
	if err != nil || !strings.HasPrefix(audience, runtimeidentity.AudiencePrefix) {
		t.Fatalf("runtime audience 无效: %q err=%v", audience, err)
	}
	if err := runtimeidentity.ValidateAudience(audience); err != nil {
		t.Fatalf("自身生成的 audience 必须合法: %v", err)
	}
	if err := runtimeidentity.ValidateAudience("runtime:v1:forged"); err == nil {
		t.Fatal("非 SHA-256 audience 必须拒绝")
	}
	changed := identity
	changed.InstanceID = "runtime-2"
	other, _ := changed.Audience()
	if audience == other {
		t.Fatal("不同启动实例不得共享 audience")
	}
	ctx, err := runtimeidentity.WithIdentity(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if restored, ok := runtimeidentity.FromContext(ctx); !ok || restored != identity {
		t.Fatalf("host-only identity 丢失: %+v", restored)
	}
}

func TestIdentityRejectsNonCanonicalArtifactDigest(t *testing.T) {
	identity := validIdentity()
	identity.ArtifactSHA256 = strings.Repeat("A", 64)
	if err := identity.Validate(); err == nil {
		t.Fatal("大写或非规范制品摘要必须拒绝")
	}
}
