package main

import (
	"encoding/json"

	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type runtimeConfiguration struct {
	PlatformCatalog string                     `json:"platform.portal-composer.platformCatalog"`
	VersionControl  json.RawMessage            `json:"platform.portal-composer.versionControl,omitempty"`
	ApprovalPolicy  approvalv2.ProviderBinding `json:"platform.portal-composer.approvalPolicy"`
}

func loadApprovalPolicy() (approvalv2.ProviderBinding, error) {
	var configuration runtimeConfiguration
	if err := sdk.DecodeStartupConfiguration(&configuration); err != nil {
		return approvalv2.ProviderBinding{}, err
	}
	return approvalPolicyFromConfiguration(configuration)
}

func approvalPolicyFromConfiguration(configuration runtimeConfiguration) (approvalv2.ProviderBinding, error) {
	if err := approvalv2.ValidateBinding(configuration.ApprovalPolicy); err != nil {
		return approvalv2.ProviderBinding{}, err
	}
	return configuration.ApprovalPolicy, nil
}
