// Package approval governs calls to the stable Approval Provider protocol at
// every source workload. It does not select a Provider or interpret policies.
package approval

import (
	"context"
	"encoding/json"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	approvalsdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/approvalpolicy"
)

const Capability = "foundation.security.approval-provider-access-policy"

func Check(_ context.Context, call *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
	var request extpoint.PermissionRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, nil, err
	}
	response := approvalsdk.AccessDecision(call, request)
	encoded, err := json.Marshal(response)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, encoded, err
}
