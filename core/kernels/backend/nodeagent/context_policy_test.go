package nodeagent

import (
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/callcontext"
)

func TestContextPolicyPublisherOverrideTakesPrecedence(t *testing.T) {
	policy, err := NewContextPolicy(
		[]string{string(callcontext.FieldScopeTenant)},
		map[string][]string{"partner": {string(callcontext.FieldScopeTenant), string(callcontext.FieldTrace)}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Ceiling("unknown").Has(callcontext.FieldTrace) {
		t.Fatal("未知发布者不得继承覆盖字段")
	}
	if !policy.Ceiling("partner").Has(callcontext.FieldTrace) {
		t.Fatal("发布者精确覆盖应优先")
	}
}

func TestParseContextPolicySupportsPublisherOverrideAndRejectsUnknownField(t *testing.T) {
	policy, err := ParseContextPolicy("scope.tenant,caller", "partner=scope.tenant,trace;vastplan=*")
	if err != nil {
		t.Fatal(err)
	}
	if !policy.Ceiling("partner").Has(callcontext.FieldTrace) || !policy.Ceiling("vastplan").Has(callcontext.FieldGrantCredentials) {
		t.Fatalf("发布者上下文覆盖解析错误: %+v", policy)
	}
	if _, err := ParseContextPolicy("unknown", ""); err == nil {
		t.Fatal("未知上下文字段必须在启动前拒绝")
	}
}
