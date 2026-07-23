package configurationv1_test

import (
	"testing"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
)

func TestManagedCredentialMergeRetainsOmittedAndSelectsRetirement(t *testing.T) {
	ref := func(handle string, version int64) commonv1.ManagedCredentialRef {
		return commonv1.ManagedCredentialRef{Handle: "credential://managed/" + handle, Scope: "tenant", Owner: "cn.vastplan.example", Purpose: "example.token", Version: version}
	}
	active := map[string]commonv1.ManagedCredentialRef{"retained": ref("retained", 1), "replaced": ref("old", 1)}
	merged := configurationv1.MergeManagedCredentials(active, map[string]commonv1.ManagedCredentialRef{"replaced": ref("new", 2)})
	if merged["retained"].Version != 1 || merged["replaced"].Version != 2 {
		t.Fatalf("托管凭证合并结果无效: %+v", merged)
	}
	retired := configurationv1.ReplacedManagedCredentials(active, merged)
	if len(retired) != 1 || retired[0].Handle != "credential://managed/old" {
		t.Fatalf("替换引用退役集合无效: %+v", retired)
	}
}
