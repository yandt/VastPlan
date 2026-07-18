package profile

import (
	"context"
	"testing"
)

type catalog struct{}

func (catalog) SupportsRunner(context.Context, PluginRef) (bool, error) { return true, nil }
func TestValidateAndEligibility(t *testing.T) {
	p := Profile{ID: "collector", Revision: 1, TenantID: "tenant-a", Runtime: "runner", Distribution: "self-update", Targets: []string{"darwin/arm64"}, AssignedTo: []string{"runner-a"}, Plugins: []PluginRef{{ID: "com.vastplan.collector", Version: "1.0.0"}}}
	if err := Validate(context.Background(), p, catalog{}); err != nil {
		t.Fatal(err)
	}
	if !Eligible(p, "runner-a") || Eligible(p, "runner-b") {
		t.Fatal("领取约束错误")
	}
}
