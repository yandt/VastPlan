package deploymentmanager

import (
	"context"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
)

func TestInstallationRolloutIsObservedWithoutBecomingCandidateState(t *testing.T) {
	host := &fakeHost{deploymentReadiness: map[uint64]deploymentpublication.ReadinessObservation{
		9: {
			SchemaVersion: 1, Tenant: "tenant-a", Deployment: "agent-services", Revision: 9, Generation: 4,
			Status: deploymentpublication.ReadinessPending, UpdatedAt: time.Now().UTC(),
			Units: []deploymentpublication.ReadinessUnit{{ID: "api", Status: deploymentpublication.ReadinessPending, DesiredReplicas: 3, Replicas: 2, ReadyReplicas: 1}},
		},
	}}
	candidate := plugininstallation.Candidate{
		ID: "installation-test", Status: plugininstallation.CandidateReady, ServiceRevisionID: 9,
		Preview: plugininstallation.Preview{Target: plugininstallation.Target{Kernel: "backend", Deployment: "agent-services", UnitID: "api"}},
	}
	observed := observeInstallationRollout(context.Background(), host, userCall("tenant-a", "alice"), candidate)
	if observed.Status != plugininstallation.CandidateReady || observed.Rollout == nil || observed.Rollout.Status != deploymentpublication.ReadinessPending || observed.Rollout.Units[0].ReadyReplicas != 1 {
		t.Fatalf("候选状态与只读滚动观察没有正确分离: %+v", observed)
	}
	planned := candidate
	planned.Status = plugininstallation.CandidatePlanned
	if observeInstallationRollout(context.Background(), host, userCall("tenant-a", "alice"), planned).Rollout != nil {
		t.Fatal("未激活候选不应请求部署滚动状态")
	}
}
