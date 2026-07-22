// Package main starts the local Authorization Enforcer.
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"

	"cdsoft.com.cn/VastPlan/core/shared/go/authorizationdirectory"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.authorization-enforcer/enforcer"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	directory := enforcer.GroupDirectory(enforcer.EmptyGroupDirectory{})
	if path := strings.TrimSpace(os.Getenv("VASTPLAN_AUTHORIZATION_DIRECTORY_GROUPS")); path != "" {
		directory = authorizationdirectory.FileDirectory{Path: path}
	}
	checker, err := enforcer.New(enforcer.FilePolicySource{SnapshotPath: os.Getenv("VASTPLAN_AUTHORIZATION_POLICY_SNAPSHOT"), TrustPath: os.Getenv("VASTPLAN_AUTHORIZATION_POLICY_TRUST"), CatalogPath: os.Getenv("VASTPLAN_AUTHORIZATION_PERMISSION_CATALOG")}, directory, split(os.Getenv("VASTPLAN_AUTHORIZATION_POLICY_AUDIENCE")))
	if err != nil {
		log.Fatalf("初始化 Authorization Enforcer: %v", err)
	}
	plugin := sdk.New(enforcer.PluginID, enforcer.PluginVersion, map[string]string{"backend": "^0.1"})
	descriptor, _ := json.Marshal(extpoint.CheckerDescriptor{Title: "签名策略授权执行器", Applies: &extpoint.Applies{}})
	plugin.Contribute(sdk.Contribution{ExtensionPoint: extpoint.PermissionChecker, ID: enforcer.Capability, Priority: 2000, Descriptor: descriptor, Handlers: map[string]sdk.Handler{"check": func(ctx context.Context, _ sdk.Host, callCtx *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
		response, err := checker.Check(ctx, callCtx, raw)
		if err != nil {
			response = enforcer.ErrorResponse(err)
		}
		encoded, encodeErr := enforcer.EncodeResponse(response)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, encoded, encodeErr
	}}})
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Authorization Enforcer 退出: %v", err)
	}
}

func split(value string) []string {
	result := []string{}
	for _, item := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
