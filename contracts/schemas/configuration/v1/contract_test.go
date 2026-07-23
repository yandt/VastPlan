package configurationv1_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
)

func TestPrepareRequestHasStableCrossLanguageDigest(t *testing.T) {
	request := validPrepareRequest()
	first, err := configurationv1.DigestPrepareRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Values = json.RawMessage("{\n  \"capacity\": 10\n}")
	second, err := configurationv1.DigestPrepareRequest(request)
	if err != nil || first != second {
		t.Fatalf("等价 JSON 必须产生稳定摘要: first=%s second=%s err=%v", first, second, err)
	}
	raw, _ := json.Marshal(request)
	parsed, err := configurationv1.ParseRequest(configurationv1.OperationPrepare, raw)
	if err != nil || parsed.(*configurationv1.PrepareRequest).ConfigurationID != request.ConfigurationID {
		t.Fatalf("有效 prepare wire 被拒绝: parsed=%+v err=%v", parsed, err)
	}
}

func TestControllerWireRejectsSecretMaterialAndUnboundStatus(t *testing.T) {
	request := validPrepareRequest()
	request.ManagedCredentials["token"] = commonv1.ManagedCredentialRef{
		Handle: "plaintext", Scope: "tenant", Owner: "cn.vastplan.demo", Purpose: "demo.token", Version: 1,
	}
	raw, _ := json.Marshal(request)
	if _, err := configurationv1.ParseRequest(configurationv1.OperationPrepare, raw); err == nil {
		t.Fatal("Configuration Controller 不得接受明文或非托管凭证引用")
	}
	status, _ := json.Marshal(configurationv1.StatusRequest{ConfigurationID: validConfigurationID(), CandidateID: validCandidateID()})
	if _, err := configurationv1.ParseRequest(configurationv1.OperationStatus, status); err == nil {
		t.Fatal("candidate status 必须绑定 request digest")
	}
}

func TestObservationContainsOnlyLifecycleFacts(t *testing.T) {
	configurationDigest, err := configurationv1.DigestConfiguration(json.RawMessage(`{"capacity":10}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	observation := configurationv1.Observation{
		Protocol: configurationv1.Protocol, ConfigurationID: validConfigurationID(),
		Active: configurationv1.ActiveReference{Revision: 2, Digest: configurationDigest},
		Candidate: &configurationv1.CandidateObservation{
			CandidateID: validCandidateID(), RequestDigest: strings.Repeat("b", 64), ConfigurationDigest: configurationDigest,
			Status: configurationv1.StatusCommitted, Ready: true,
		},
		ObservedAt: time.Now().UTC(),
	}
	if err := configurationv1.ValidateObservation(observation); err != nil {
		t.Fatal(err)
	}
	observation.Candidate.Ready = false
	if err := configurationv1.ValidateObservation(observation); err == nil {
		t.Fatal("Committed observation 必须证明候选已经 Ready 且成为 Active")
	}
}

func validPrepareRequest() configurationv1.PrepareRequest {
	return configurationv1.PrepareRequest{
		CandidateID: validCandidateID(), ConfigurationID: validConfigurationID(),
		CatalogDigest: strings.Repeat("c", 64), SchemaDigest: strings.Repeat("d", 64), ArtifactSHA256: strings.Repeat("e", 64),
		ExpectedActive: configurationv1.ActiveReference{Revision: 1, Digest: strings.Repeat("f", 64)},
		Values:         json.RawMessage(`{"capacity":10}`),
		ManagedCredentials: map[string]commonv1.ManagedCredentialRef{"token": {
			Handle: "credential://managed/opaque", Scope: "tenant", Owner: "cn.vastplan.demo", Purpose: "demo.token", Version: 1,
		}},
	}
}

func validCandidateID() string     { return "pcfg_" + strings.Repeat("a", 32) }
func validConfigurationID() string { return "cfg_" + strings.Repeat("9", 24) }
