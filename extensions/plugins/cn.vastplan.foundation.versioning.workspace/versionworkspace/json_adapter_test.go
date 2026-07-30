package versionworkspace

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
)

func TestJSONAdapterCanonicalizesRejectsMaterialAndDiffs(t *testing.T) {
	adapter := NewJSONAdapter()
	request := func(raw string) resourcev1.AdapterNormalizeRequest {
		return resourcev1.AdapterNormalizeRequest{
			Resource: resourcev1.ResourceKey{Type: "portal.configuration", ID: "portal-main"}, Mode: resourcev1.ModeSnapshot,
			Snapshot: resourcev1.Snapshot{Kind: resourcev1.ContentJSON, MediaType: "application/json", JSON: json.RawMessage(raw)},
		}
	}
	left, err := adapter.Normalize(context.Background(), request(`{"b":2,"a":{"enabled":true}}`))
	if err != nil || string(left.Snapshot.JSON) != `{"a":{"enabled":true},"b":2}` {
		t.Fatalf("JSON 未规范化: result=%s err=%v", left.Snapshot.JSON, err)
	}
	if _, err := adapter.Normalize(context.Background(), request(`{"password":"plain"}`)); err == nil || !strings.Contains(err.Error(), "ManagedCredentialRef") {
		t.Fatalf("疑似秘密明文必须拒绝: %v", err)
	}
	credential := `{"token":{"handle":"credential://managed/opaque","scope":"tenant","owner":"cn.vastplan.demo","purpose":"demo.token","version":1}}`
	if _, err := adapter.Normalize(context.Background(), request(credential)); err != nil {
		t.Fatalf("合法 CredentialRef 不应被拒绝: %v", err)
	}
	right, err := adapter.Normalize(context.Background(), request(`{"a":{"enabled":false,"name":"main"},"c":3}`))
	if err != nil {
		t.Fatal(err)
	}
	diff, err := adapter.Diff(context.Background(), resourcev1.AdapterDiffRequest{Resource: request(`{}`).Resource, Mode: resourcev1.ModeSnapshot, Left: left.Snapshot, Right: right.Snapshot})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/a/enabled", "/a/name", "/b", "/c"}
	if !reflect.DeepEqual(diff.ChangedPaths, want) || diff.Summary.Added != 2 || diff.Summary.Modified != 1 || diff.Summary.Removed != 1 {
		t.Fatalf("diff 不符合预期: %+v", diff)
	}
}
