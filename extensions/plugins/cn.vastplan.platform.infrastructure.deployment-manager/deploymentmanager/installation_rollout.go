package deploymentmanager

import (
	"context"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

// Rollout is an observation, not installation workflow state. Read paths add
// it from the kernel readiness service without persisting a second source of
// truth or turning UI refresh into a background polling loop.
func observeInstallationRollouts(ctx context.Context, host sdk.Host, call *contractv1.CallContext, candidates []plugininstallation.Candidate) []plugininstallation.Candidate {
	for index := range candidates {
		candidates[index] = observeInstallationRollout(ctx, host, call, candidates[index])
	}
	return candidates
}

func observeInstallationRollout(ctx context.Context, host sdk.Host, call *contractv1.CallContext, candidate plugininstallation.Candidate) plugininstallation.Candidate {
	if candidate.Status != plugininstallation.CandidateActivating && candidate.Status != plugininstallation.CandidateReady {
		return candidate
	}
	observation, err := observeServiceReadiness(ctx, host, call, candidate.Preview.Target.Deployment, candidate.ServiceRevisionID)
	if err == nil {
		candidate.Rollout = &observation
	}
	return candidate
}
