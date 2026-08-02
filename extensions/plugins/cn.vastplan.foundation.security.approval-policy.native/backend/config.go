package main

import (
	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type runtimeConfiguration struct {
	Profiles []approvalv2.PolicyProfile `json:"foundation.approval-policy.native.profiles"`
}

func loadProfiles() ([]approvalv2.PolicyProfile, error) {
	var configuration runtimeConfiguration
	if err := sdk.DecodeStartupConfiguration(&configuration); err != nil {
		return nil, err
	}
	return configuration.Profiles, nil
}
