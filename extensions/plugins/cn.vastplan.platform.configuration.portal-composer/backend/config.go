package main

import (
	"encoding/json"

	approvalv1 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/approvalpolicy"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type runtimeConfiguration struct {
	PlatformCatalog string             `json:"platform.portal-composer.platformCatalog"`
	VersionControl  json.RawMessage    `json:"platform.portal-composer.versionControl,omitempty"`
	ApprovalPolicy  approvalv1.Profile `json:"platform.portal-composer.approvalPolicy"`
}

func loadApprovalPolicy() (approvalpolicy.Policy, error) {
	var configuration runtimeConfiguration
	if err := sdk.DecodeStartupConfiguration(&configuration); err != nil {
		return nil, err
	}
	return approvalPolicyFromConfiguration(configuration)
}

func approvalPolicyFromConfiguration(configuration runtimeConfiguration) (approvalpolicy.Policy, error) {
	return approvalpolicy.New(configuration.ApprovalPolicy)
}
