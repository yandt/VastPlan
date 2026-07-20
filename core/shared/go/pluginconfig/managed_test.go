package pluginconfig_test

import (
	"context"
	"errors"
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
)

type custodian struct {
	staged, activated, aborted int
}

func (c *custodian) Stage(_ context.Context, owner, name, purpose string, value []byte) (pluginconfig.StagedCredential, error) {
	c.staged++
	return pluginconfig.StagedCredential{ID: "stage-1", Ref: pluginconfig.ManagedCredentialRef{Handle: "credential://managed/stage-1", Scope: "service", Owner: owner, Purpose: purpose, Version: 1, Name: name}}, nil
}
func (c *custodian) Activate(context.Context, pluginconfig.StagedCredential) error {
	c.activated++
	return nil
}
func (c *custodian) Abort(context.Context, pluginconfig.StagedCredential) error {
	c.aborted++
	return nil
}

type candidateStore struct {
	active  pluginconfig.Snapshot
	prepare pluginconfig.Snapshot
	fail    bool
}

func (s *candidateStore) Active(context.Context, string) (pluginconfig.Snapshot, bool, error) {
	return s.active, s.active.ID != "", nil
}
func (s *candidateStore) Prepare(_ context.Context, snapshot pluginconfig.Snapshot, _ *uint64) (pluginconfig.Snapshot, error) {
	if s.fail {
		return pluginconfig.Snapshot{}, errors.New("prepare failed")
	}
	snapshot.ID, snapshot.Revision = "candidate-1", s.active.Revision+1
	s.prepare = snapshot
	return snapshot, nil
}
func (s *candidateStore) Activate(_ context.Context, id string) (pluginconfig.Snapshot, error) {
	if s.prepare.ID != id {
		return pluginconfig.Snapshot{}, errors.New("candidate not found")
	}
	s.prepare.State = "Active"
	s.active = s.prepare
	return s.active, nil
}
func (s *candidateStore) Fail(context.Context, string, string) error { return nil }

func TestManagerKeepsSecretsOutOfSnapshotAndActivatesLast(t *testing.T) {
	credentials, store := &custodian{}, &candidateStore{}
	secret := []byte("super-secret")
	snapshot, err := (pluginconfig.Manager{Credentials: credentials, Store: store}).Apply(context.Background(), "plugin.a", []pluginconfig.CredentialSpec{{ID: "api-token", Purpose: "remote.api-token", Required: true}}, pluginconfig.WriteRequest{Values: []byte(`{"endpoint":"https://api.example"}`), Secrets: map[string][]byte{"api-token": secret}})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != "Active" || credentials.staged != 1 || credentials.activated != 1 || credentials.aborted != 0 {
		t.Fatalf("两阶段生效错误: snapshot=%+v custodian=%+v", snapshot, credentials)
	}
	if string(snapshot.Values) != `{"endpoint":"https://api.example"}` || snapshot.Credentials["api-token"].Handle == "" {
		t.Fatalf("快照投影错误: %+v", snapshot)
	}
	for _, value := range secret {
		if value != 0 {
			t.Fatal("Manager 返回前必须清零可变明文缓冲区")
		}
	}
}

func TestManagerAbortsStagedCredentialWhenCandidatePrepareFails(t *testing.T) {
	credentials, store := &custodian{}, &candidateStore{fail: true}
	_, err := (pluginconfig.Manager{Credentials: credentials, Store: store}).Apply(context.Background(), "plugin.a", []pluginconfig.CredentialSpec{{ID: "api-token", Purpose: "remote.api-token", Required: true}}, pluginconfig.WriteRequest{Values: []byte(`{}`), Secrets: map[string][]byte{"api-token": []byte("secret")}})
	if err == nil || credentials.aborted != 1 || credentials.activated != 0 {
		t.Fatalf("prepare 失败必须 abort: err=%v custodian=%+v", err, credentials)
	}
}

func TestManagerClearsSecretsEvenWhenValidationFails(t *testing.T) {
	secret := []byte("must-clear")
	_, err := (pluginconfig.Manager{Credentials: &custodian{}, Store: &candidateStore{}}).Apply(context.Background(), "plugin.a", nil, pluginconfig.WriteRequest{Values: []byte(`{}`), Secrets: map[string][]byte{"undeclared": secret}})
	if err == nil {
		t.Fatal("未声明秘密必须拒绝")
	}
	for _, value := range secret {
		if value != 0 {
			t.Fatal("校验失败也必须清零明文缓冲区")
		}
	}
}
