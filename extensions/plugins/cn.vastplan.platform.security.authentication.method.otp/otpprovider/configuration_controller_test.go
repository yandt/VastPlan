package otpprovider

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
)

func TestConfigurationControllerCommitsAtomicallyAndRecoversAfterRestart(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "otp-configuration.json")
	initial := controllerTestConfiguration(stateFile, "urn:issuer:old")
	store := NewMemoryChallengeStore(32)
	provider, err := New(initial, store)
	if err != nil {
		t.Fatal(err)
	}
	configurationID := "cfg_" + strings.Repeat("a", 24)
	status, err := provider.Status(context.Background(), nil, nil, configurationv1.StatusRequest{ConfigurationID: configurationID})
	if err != nil {
		t.Fatal(err)
	}

	before := provider.begin(authenticationv1.BeginRequest{
		TransactionID: "before-hot-commit", ProviderProfileID: "enterprise-email", MethodID: EmailMethodID,
	})
	if before.Result.State != authenticationv1.StateChallenge {
		t.Fatalf("旧配置下无法创建挑战: %+v", before)
	}

	next := controllerTestConfiguration(stateFile, "urn:issuer:new")
	values, _ := json.Marshal(next)
	request := configurationv1.PrepareRequest{
		CandidateID: "pcfg_" + strings.Repeat("b", 32), ConfigurationID: configurationID,
		CatalogDigest: strings.Repeat("c", 64), SchemaDigest: strings.Repeat("d", 64), ArtifactSHA256: strings.Repeat("e", 64),
		ExpectedActive: status.Active, Values: values,
	}
	prepared, err := provider.Prepare(context.Background(), nil, nil, request)
	if err != nil || prepared.Candidate == nil || prepared.Candidate.Status != configurationv1.StatusPrepared || !prepared.Candidate.Ready {
		t.Fatalf("OTP hot candidate 未 Ready: observation=%+v err=%v", prepared, err)
	}
	requestDigest, _ := configurationv1.DigestPrepareRequest(request)
	committed, err := provider.Commit(context.Background(), nil, nil, configurationv1.CandidateRequest{CandidateID: request.CandidateID, RequestDigest: requestDigest})
	if err != nil || committed.Active.Revision != status.Active.Revision+1 || committed.Candidate.Status != configurationv1.StatusCommitted {
		t.Fatalf("OTP hot candidate 未原子提交: observation=%+v err=%v", committed, err)
	}
	if duplicate, err := provider.Prepare(context.Background(), nil, nil, request); err != nil || duplicate.Candidate.Status != configurationv1.StatusCommitted {
		t.Fatalf("提交后的相同 prepare 必须幂等: observation=%+v err=%v", duplicate, err)
	}

	oldChallenge, ok := store.Load("before-hot-commit")
	if !ok || oldChallenge.Profile.Issuer != "urn:issuer:old" {
		t.Fatalf("在途挑战必须固定旧配置 generation: %+v", oldChallenge)
	}
	after := provider.begin(authenticationv1.BeginRequest{
		TransactionID: "after-hot-commit", ProviderProfileID: "enterprise-email", MethodID: EmailMethodID,
	})
	newChallenge, ok := store.Load("after-hot-commit")
	if after.Result.State != authenticationv1.StateChallenge || !ok || newChallenge.Profile.Issuer != "urn:issuer:new" {
		t.Fatalf("新挑战未使用新配置 generation: result=%+v challenge=%+v", after, newChallenge)
	}

	restarted, err := New(initial, NewMemoryChallengeStore(32))
	if err != nil {
		t.Fatalf("重启无法恢复已提交 hot 配置: %v", err)
	}
	recovered, err := restarted.Status(context.Background(), nil, nil, configurationv1.StatusRequest{
		ConfigurationID: configurationID, CandidateID: request.CandidateID, RequestDigest: requestDigest,
	})
	if err != nil || recovered.Active != committed.Active || recovered.Candidate.Status != configurationv1.StatusCommitted {
		t.Fatalf("重启恢复 observation 错误: recovered=%+v err=%v", recovered, err)
	}
}

func TestConfigurationControllerAbortsPreparedCandidateAndRejectsStaleCAS(t *testing.T) {
	configuration := controllerTestConfiguration(filepath.Join(t.TempDir(), "otp.json"), "urn:issuer:old")
	provider, err := New(configuration)
	if err != nil {
		t.Fatal(err)
	}
	values, _ := json.Marshal(controllerTestConfiguration(configuration.StateFile, "urn:issuer:new"))
	request := configurationv1.PrepareRequest{
		CandidateID: "pcfg_" + strings.Repeat("1", 32), ConfigurationID: "cfg_" + strings.Repeat("2", 24),
		CatalogDigest: strings.Repeat("3", 64), SchemaDigest: strings.Repeat("4", 64), ArtifactSHA256: strings.Repeat("5", 64),
		ExpectedActive: configurationv1.ActiveReference{Revision: 99, Digest: strings.Repeat("6", 64)}, Values: values,
	}
	if _, err := provider.Prepare(context.Background(), nil, nil, request); err == nil {
		t.Fatal("过期 Active CAS 必须拒绝")
	}
	status, _ := provider.Status(context.Background(), nil, nil, configurationv1.StatusRequest{ConfigurationID: request.ConfigurationID})
	request.ExpectedActive = status.Active
	prepared, err := provider.Prepare(context.Background(), nil, nil, request)
	if err != nil {
		t.Fatal(err)
	}
	aborted, err := provider.Abort(context.Background(), nil, nil, configurationv1.CandidateRequest{CandidateID: request.CandidateID, RequestDigest: prepared.Candidate.RequestDigest})
	if err != nil || aborted.Candidate.Status != configurationv1.StatusAborted || aborted.Candidate.Ready || aborted.Active != status.Active {
		t.Fatalf("abort 不得改变 Active: observation=%+v err=%v", aborted, err)
	}
}

func controllerTestConfiguration(stateFile, issuer string) Configuration {
	maxResends := 2
	return Configuration{
		StateFile: stateFile, Capacity: 32,
		Profiles: map[string]Profile{"enterprise-email": {
			MethodID: EmailMethodID, DeliveryProfileID: "enterprise-mail", Channel: authenticationv1.DeliveryEmail,
			Issuer: issuer, CodeLength: 6, TTLSeconds: 300, ResendSeconds: 5, MaxAttempts: 5, MaxResends: &maxResends,
		}},
	}
}
