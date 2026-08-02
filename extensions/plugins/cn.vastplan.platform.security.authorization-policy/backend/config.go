package main

import (
	"time"

	policy "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.authorization-policy/authorizationpolicy"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type runtimeConfiguration struct {
	TenantID        string               `json:"tenantId"`
	SnapshotLease   snapshotLeaseConfig  `json:"snapshotLease"`
	ManagedBindings managedBindingConfig `json:"managedBindings"`
}

type snapshotLeaseConfig struct {
	Audiences          []string `json:"audiences"`
	TTLSeconds         int      `json:"ttlSeconds"`
	RenewalLeadSeconds int      `json:"renewalLeadSeconds"`
}

type managedBindingConfig struct {
	Creators           []string `json:"creators"`
	TTLSeconds         int      `json:"ttlSeconds"`
	RenewalLeadSeconds int      `json:"renewalLeadSeconds"`
}

func loadRuntimeConfiguration() (runtimeConfiguration, policy.FixedSnapshotLeasePolicy, error) {
	var configuration runtimeConfiguration
	if err := sdk.DecodeStartupConfiguration(&configuration); err != nil {
		return runtimeConfiguration{}, policy.FixedSnapshotLeasePolicy{}, err
	}
	lease, err := leasePolicyFromConfiguration(configuration)
	return configuration, lease, err
}

func leasePolicyFromConfiguration(configuration runtimeConfiguration) (policy.FixedSnapshotLeasePolicy, error) {
	lease, err := policy.NewFixedSnapshotLeasePolicy(policy.SnapshotLeasePolicyOptions{
		Audiences:                 configuration.SnapshotLease.Audiences,
		SnapshotTTL:               time.Duration(configuration.SnapshotLease.TTLSeconds) * time.Second,
		RenewalLead:               time.Duration(configuration.SnapshotLease.RenewalLeadSeconds) * time.Second,
		ManagedBindingCreators:    configuration.ManagedBindings.Creators,
		ManagedBindingTTL:         time.Duration(configuration.ManagedBindings.TTLSeconds) * time.Second,
		ManagedBindingRenewalLead: time.Duration(configuration.ManagedBindings.RenewalLeadSeconds) * time.Second,
	})
	return lease, err
}
