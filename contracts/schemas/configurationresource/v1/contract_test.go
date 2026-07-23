package configurationresourcev1_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	configurationresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationresource/v1"
)

func TestPrepareRequestUsesIndependentResourceIdentityAndReplacementCredentials(t *testing.T) {
	request := validPrepare(configurationresourcev1.ActionCreate)
	digest, err := configurationresourcev1.DigestPrepareRequest(request)
	if err != nil || len(digest) != 64 {
		t.Fatalf("计算 resource prepare digest: %s err=%v", digest, err)
	}
	raw, _ := json.Marshal(request)
	parsed, err := configurationresourcev1.ParseRequest(configurationresourcev1.OperationPrepare, raw)
	if err != nil || parsed.(*configurationresourcev1.PrepareRequest).ResourceID != request.ResourceID {
		t.Fatalf("有效 resource prepare 被拒绝: %+v err=%v", parsed, err)
	}
	request.ExpectedActive = &configurationresourcev1.ActiveReference{Revision: 1, Digest: strings.Repeat("f", 64)}
	if _, err := configurationresourcev1.DigestPrepareRequest(request); err == nil {
		t.Fatal("create 不得伪装已有 Active")
	}
	deletion := validPrepare(configurationresourcev1.ActionDelete)
	deletion.ExpectedActive = &configurationresourcev1.ActiveReference{Revision: 2, Digest: strings.Repeat("f", 64)}
	deletion.Values, deletion.ManagedCredentials = nil, nil
	if _, err := configurationresourcev1.DigestPrepareRequest(deletion); err != nil {
		t.Fatalf("合法 delete 被拒绝: %v", err)
	}
}

func TestPrepareDigestMatchesNodeSDKGolden(t *testing.T) {
	request := configurationresourcev1.PrepareRequest{
		CandidateID: candidateID(), ConfigurationID: "cfg_" + strings.Repeat("9", 24), CollectionID: collectionID(), ResourceID: resourceID(), Action: configurationresourcev1.ActionCreate,
		CatalogDigest: strings.Repeat("c", 64), SchemaDigest: strings.Repeat("d", 64), ArtifactSHA256: strings.Repeat("e", 64),
		Values: json.RawMessage(`{"endpoint":"https://delivery.example.test","displayName":"Enterprise Mail"}`),
		ManagedCredentials: map[string]commonv1.ManagedCredentialRef{"authorization": {
			Handle: "credential://managed/opaque", Scope: "tenant", Owner: "cn.vastplan.demo", Purpose: "demo.authorization", Version: 1,
		}},
	}
	digest, err := configurationresourcev1.DigestPrepareRequest(request)
	if err != nil || digest != "c00b448b09303ea0d4764706a2e6e5ddae2b01fc17dfee9aed967596b8e5424d" {
		t.Fatalf("Go/Node resource prepare digest 不一致: %s err=%v", digest, err)
	}
	configurationDigest, err := configurationresourcev1.DigestResourceConfiguration(request.Values, request.ManagedCredentials)
	if err != nil || configurationDigest != "13f3e6cda2ac6eb08697aec48ac96c4afeeec471891b6366e7677b93d22eed6d" {
		t.Fatalf("Go/Node resource configuration digest 不一致: %s err=%v", configurationDigest, err)
	}
	deletedDigest, err := configurationresourcev1.DigestDeletedResource(request.ResourceID)
	if err != nil || deletedDigest != "8e4e709267c0aada3b915401047fcf63d7c3ca2a7f46590d80105eb7dbd3e679" {
		t.Fatalf("Go/Node deleted resource digest 不一致: %s err=%v", deletedDigest, err)
	}
}

func TestQueryViewsExposeCredentialStatusButNeverHandles(t *testing.T) {
	response := configurationresourcev1.GetResponse{
		Protocol: configurationresourcev1.Protocol, CollectionID: collectionID(), ObservedAt: time.Now().UTC(),
		Item: configurationresourcev1.ResourceView{
			ResourceID: resourceID(), Active: configurationresourcev1.ActiveReference{Revision: 3, Digest: strings.Repeat("a", 64)},
			Values:           json.RawMessage(`{"displayName":"Enterprise Mail"}`),
			CredentialStates: []configurationresourcev1.CredentialState{{FieldID: "authorization", Configured: true, Version: 2}}, UpdatedAt: time.Now().UTC(),
		},
	}
	if err := configurationresourcev1.ValidateGetResponse(response); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(response)
	if strings.Contains(string(raw), "credential://") {
		t.Fatal("公开 Profile 资源不得包含凭证 handle")
	}
}

func TestCommittedDeleteIsRepresentedByAbsentActive(t *testing.T) {
	observation := configurationresourcev1.Observation{
		Protocol: configurationresourcev1.Protocol, CollectionID: collectionID(), ResourceID: resourceID(), ObservedAt: time.Now().UTC(),
		Candidate: &configurationresourcev1.CandidateObservation{
			CandidateID: candidateID(), RequestDigest: strings.Repeat("b", 64), ResultDigest: strings.Repeat("c", 64),
			Action: configurationresourcev1.ActionDelete, Status: configurationresourcev1.StatusCommitted, Ready: true,
		},
	}
	if err := configurationresourcev1.ValidateObservation(observation); err != nil {
		t.Fatal(err)
	}
	observation.Active = &configurationresourcev1.ActiveReference{Revision: 4, Digest: strings.Repeat("c", 64)}
	if err := configurationresourcev1.ValidateObservation(observation); err == nil {
		t.Fatal("delete commit 后不得继续报告 Active")
	}
}

func validPrepare(action configurationresourcev1.Action) configurationresourcev1.PrepareRequest {
	return configurationresourcev1.PrepareRequest{
		CandidateID: candidateID(), ConfigurationID: "cfg_" + strings.Repeat("9", 24), CollectionID: collectionID(), ResourceID: resourceID(), Action: action,
		CatalogDigest: strings.Repeat("c", 64), SchemaDigest: strings.Repeat("d", 64), ArtifactSHA256: strings.Repeat("e", 64),
		Values: json.RawMessage(`{"displayName":"Enterprise Mail"}`),
		ManagedCredentials: map[string]commonv1.ManagedCredentialRef{"authorization": {
			Handle: "credential://managed/opaque", Scope: "tenant", Owner: "cn.vastplan.demo", Purpose: "demo.authorization", Version: 1,
		}},
	}
}

func candidateID() string  { return "pcfg_" + strings.Repeat("a", 32) }
func collectionID() string { return "cfgc_" + strings.Repeat("1", 24) }
func resourceID() string   { return "cfgp_" + strings.Repeat("2", 32) }
