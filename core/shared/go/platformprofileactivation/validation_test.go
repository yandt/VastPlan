package platformprofileactivation

import (
	"encoding/json"
	"testing"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
)

func TestPrepareDigestNormalizesJSONAndRejectsCredentialMaterial(t *testing.T) {
	request := testPrepareRequest()
	first, err := DigestPrepareRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Values = json.RawMessage("{\n  \"enabled\": true\n}")
	second, err := DigestPrepareRequest(request)
	if err != nil || first != second {
		t.Fatalf("等价 JSON 必须得到稳定请求摘要: first=%s second=%s err=%v", first, second, err)
	}
	request.Credentials["token"] = pluginconfig.ManagedCredentialRef{Handle: "plaintext", Scope: "tenant", Owner: "plugin.a", Purpose: "api.token", Version: 1}
	if _, err := DigestPrepareRequest(request); err == nil {
		t.Fatal("候选契约不得接受明文或非托管凭证引用")
	}
}

func TestPublishRequestBindsFullPrepareRequest(t *testing.T) {
	request := testPrepareRequest()
	digest, err := DigestPrepareRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	publish := PublishRequest{Prepare: request, RequestDigest: digest, ExpectedDigest: hexValue("d")}
	if _, err := publish.Normalize(); err != nil {
		t.Fatal(err)
	}
	publish.Prepare.DeploymentRevision++
	if _, err := publish.Normalize(); err == nil {
		t.Fatal("改变 Deployment revision 后必须拒绝旧请求摘要")
	}
}

func TestActivationValidationEnforcesApprovalAndMonotonicRollback(t *testing.T) {
	activation := Activation{
		CandidateID: "pcfg_" + hexValue("a")[:32], ConfigurationID: "cfg_" + hexValue("b")[:24],
		Deployment: "services-a", DeploymentRevision: 2, PreviousServiceRevision: 1,
		Status: ActivationApproved, RequestedBy: "alice", ApprovedBy: "bob",
	}
	if err := activation.Validate(); err != nil {
		t.Fatal(err)
	}
	activation.ApprovedBy = "alice"
	if err := activation.Validate(); err == nil {
		t.Fatal("Profile 激活不得由提交人自批")
	}
	activation.ApprovedBy, activation.Status, activation.RollbackDeploymentRevision = "bob", ActivationRolledBack, 2
	if err := activation.Validate(); err == nil {
		t.Fatal("Profile 回滚修订必须严格大于候选部署修订")
	}
	activation.RollbackDeploymentRevision = 3
	if err := activation.Validate(); err != nil {
		t.Fatal(err)
	}
	activation.Status, activation.RollbackDeploymentRevision = ActivationAborted, 0
	if err := activation.Validate(); err != nil {
		t.Fatalf("已审批候选仍应允许安全放弃: %v", err)
	}
}

func testPrepareRequest() PrepareRequest {
	return PrepareRequest{
		CandidateID: "pcfg_" + hexValue("a")[:32], ConfigurationID: "cfg_" + hexValue("b")[:24],
		ConfigCatalogDigest: hexValue("c"), SchemaDigest: hexValue("d"), ArtifactSHA256: hexValue("e"),
		Values:      json.RawMessage(`{"enabled":true}`),
		Credentials: map[string]pluginconfig.ManagedCredentialRef{"token": {Handle: "credential://managed/opaque", Scope: "tenant", Owner: "plugin.a", Purpose: "api.token", Version: 1}},
		Composition: backendcompositionv1.ApplicationComposition{
			Document: compositioncommonv1.Document{Version: 1, ID: "platform-management", Revision: 1},
			Target:   compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend},
			Metadata: deploymentv1.Metadata{Name: "platform-management", Tenant: "system"},
			Units:    []backendcompositionv1.ApplicationUnit{},
		},
		DeploymentRevision: 2,
	}
}

func hexValue(character string) string {
	value := ""
	for len(value) < 64 {
		value += character
	}
	return value
}
