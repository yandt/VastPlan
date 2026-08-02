package deploymentmanager

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

type portalInstallationHost struct {
	base         *intentWorkflowHost
	calls        []string
	failCommitOn string
}

func (h *portalInstallationHost) Call(ctx context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if target.Capability != portalapi.ComposerCapability {
		return h.base.Call(ctx, target, call, payload)
	}
	var lookup portalapi.PluginInstallationLookup
	_ = json.Unmarshal(payload, &lookup)
	if lookup.PortalID == "" {
		var request portalapi.PluginInstallationRequest
		_ = json.Unmarshal(payload, &request)
		lookup.CandidateID, lookup.PortalID = request.CandidateID, request.PortalID
	}
	h.calls = append(h.calls, target.GetOperation()+":"+lookup.PortalID)
	if target.GetOperation() == portalapi.CommitPluginInstallationOperation && lookup.PortalID == h.failCommitOn {
		return nil, nil, errors.New("portal commit failed")
	}
	status := portalapi.PluginInstallationPrepared
	if target.GetOperation() == portalapi.CommitPluginInstallationOperation {
		status = portalapi.PluginInstallationCommitted
	}
	return okJSON(portalapi.PluginInstallationPreparation{CandidateID: lookup.CandidateID, PortalID: lookup.PortalID, Status: status})
}

func TestInstallationActivationPreparesAllPortalsBeforeBackendAndCommitsDeterministically(t *testing.T) {
	service, base, requester := publishedIntentService(t)
	host := &portalInstallationHost{base: base}
	request := upgradeInstallationRequest()
	request.PortalTargets = []string{"portal-b", "portal-a"}
	requester.Scene = "portal.bff"
	candidate, err := service.CreatePluginInstallationCandidate(context.Background(), host, requester, plugininstallation.SourceController, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.SubmitPluginInstallationCandidate(context.Background(), host, requester, candidate.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ApprovePluginInstallationCandidate(context.Background(), host, userCall("tenant-a", "bob"), candidate.ID); err != nil {
		t.Fatal(err)
	}
	ready, err := service.ActivatePluginInstallationCandidate(context.Background(), host, userCall("tenant-a", "alice"), candidate.ID)
	if err != nil || ready.Status != plugininstallation.CandidateReady {
		t.Fatalf("全栈候选未就绪: %+v err=%v", ready, err)
	}
	want := []string{
		"preparePluginInstallation:portal-a", "preparePluginInstallation:portal-b",
		"commitPluginInstallation:portal-a", "commitPluginInstallation:portal-b",
	}
	if !equalStrings(host.calls, want) {
		t.Fatalf("Portal 两阶段顺序错误: got=%v want=%v", host.calls, want)
	}
	host.calls = nil
	ready, err = service.ActivatePluginInstallationCandidate(context.Background(), host, userCall("tenant-a", "alice"), candidate.ID)
	if err != nil || ready.Status != plugininstallation.CandidateReady {
		t.Fatalf("全栈候选重复激活应幂等: %+v err=%v", ready, err)
	}
}

func TestInstallationCommitFailureRollsBackCommittedPortalAndBackend(t *testing.T) {
	service, base, requester := publishedIntentService(t)
	host := &portalInstallationHost{base: base, failCommitOn: "portal-b"}
	request := upgradeInstallationRequest()
	request.PortalTargets = []string{"portal-a", "portal-b"}
	requester.Scene = "portal.bff"
	candidate, err := service.CreatePluginInstallationCandidate(context.Background(), host, requester, plugininstallation.SourceController, request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = service.SubmitPluginInstallationCandidate(context.Background(), host, requester, candidate.ID)
	_, _ = service.ApprovePluginInstallationCandidate(context.Background(), host, userCall("tenant-a", "bob"), candidate.ID)
	if _, err = service.ActivatePluginInstallationCandidate(context.Background(), host, userCall("tenant-a", "alice"), candidate.ID); err == nil {
		t.Fatal("Portal 提交失败时全栈候选不得报告成功")
	}
	revisions, listErr := service.ListServiceRevisions(requester)
	if listErr != nil || len(revisions) < 3 || !revisions[0].Active || revisions[0].Intent.Services[0].RootPlugins[0].Constraint != "=1.0.0" {
		t.Fatalf("Portal 提交失败后 Backend 未恢复上一修订: %+v err=%v", revisions, listErr)
	}
	if !containsString(host.calls, "rollbackPluginInstallation:portal-a") || !containsString(host.calls, "abortPluginInstallation:portal-b") {
		t.Fatalf("失败补偿不完整: %v", host.calls)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
