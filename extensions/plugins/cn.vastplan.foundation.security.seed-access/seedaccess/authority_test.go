package seedaccess

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

type localProof struct{ valid string }

func (v localProof) VerifyLocalRecoveryProof(proof []byte) error {
	if string(proof) != v.valid {
		return errors.New("invalid")
	}
	return nil
}

func testRef(id, fill string) compositioncommonv1.Ref {
	return compositioncommonv1.Ref{ID: id, Revision: 1, Digest: strings.Repeat(fill, 64)}
}

func TestSeedAuthorityHandoffAndRecoveryRemainDatabaseIndependent(t *testing.T) {
	store := FileStore{Path: filepath.Join(t.TempDir(), "seed-access.json")}
	authority, err := NewAuthority(store, localProof{valid: "console-proof"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	authority.now = func() time.Time { return now }

	state, err := authority.Initialize("seed-owner", []byte("correct horse battery staple"))
	if err != nil || state.Phase != SeedActive {
		t.Fatalf("初始化失败: %+v %v", state, err)
	}
	if err := authority.Authenticate("seed-owner", []byte("correct horse battery staple"), nil); err != nil {
		t.Fatal(err)
	}

	profile := testRef("corporate-oidc", "a")
	state, err = authority.ConfigureProvider(state.Generation, profile)
	if err != nil || state.Phase != ProviderConfigured {
		t.Fatalf("配置 Provider 失败: %+v %v", state, err)
	}
	subject := authenticationv1.SubjectIdentity{ID: "alice-immutable-id", Issuer: "https://identity.example.test"}
	state, err = authority.VerifyProvider(state.Generation, profile, subject)
	if err != nil || state.Phase != ProviderVerified {
		t.Fatalf("验证 Provider 失败: %+v %v", state, err)
	}

	seal := HandoffSeal{ProviderProfile: profile, Subject: subject, PolicySnapshot: testRef("root-policy", "b"), SessionID: "session.1", AuthenticatedAt: now, ExpiresAt: now.Add(5 * time.Minute), RecoveryReady: true}
	state, err = authority.PrepareHandoff(state.Generation, seal)
	if err != nil || state.Phase != HandoffReady || state.Handoff == nil {
		t.Fatalf("准备交接失败: %+v %v", state, err)
	}
	state, err = authority.CompleteHandoff(state.Generation, state.Handoff.Digest)
	if err != nil || state.Phase != EnterpriseActive {
		t.Fatalf("完成交接失败: %+v %v", state, err)
	}
	if err := authority.Authenticate("seed-owner", []byte("correct horse battery staple"), nil); err == nil {
		t.Fatal("企业身份启用后 Seed 登录必须关闭")
	}

	state, token, err := authority.OpenRecovery(state.Generation, []byte("console-proof"), 5*time.Minute)
	if err != nil || state.Phase != RecoveryLease || len(token) < 32 {
		t.Fatalf("开启恢复失败: %+v %v", state, err)
	}
	if err := authority.Authenticate("seed-owner", []byte("correct horse battery staple"), token); err != nil {
		t.Fatalf("恢复租约内应允许 Seed 验证: %v", err)
	}
	state, err = authority.CloseRecovery(state.Generation)
	if err != nil || state.Phase != EnterpriseActive {
		t.Fatalf("关闭恢复失败: %+v %v", state, err)
	}
}

func TestSeedAuthorityRejectsWeakOrIncompleteHandoff(t *testing.T) {
	store := FileStore{Path: filepath.Join(t.TempDir(), "seed-access.json")}
	authority, _ := NewAuthority(store, nil)
	if _, err := authority.Initialize("owner", []byte("short")); err == nil {
		t.Fatal("弱 Seed 密码必须拒绝")
	}
	state, err := authority.Initialize("owner", []byte("a sufficiently long password"))
	if err != nil {
		t.Fatal(err)
	}
	profile := testRef("corporate-oidc", "c")
	state, _ = authority.ConfigureProvider(state.Generation, profile)
	state, _ = authority.VerifyProvider(state.Generation, profile, authenticationv1.SubjectIdentity{ID: "alice", Issuer: "issuer"})
	if _, err := authority.PrepareHandoff(state.Generation, HandoffSeal{ProviderProfile: profile, Subject: *state.ProviderSubject, PolicySnapshot: testRef("policy", "d")}); err == nil {
		t.Fatal("缺少正常 Session、恢复通道和有效期时不得交接")
	}
}

func TestFileStoreCASAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seed-access.json")
	store := FileStore{Path: path}
	authority, _ := NewAuthority(store, nil)
	state, err := authority.Initialize("owner", []byte("a sufficiently long password"))
	if err != nil {
		t.Fatal(err)
	}
	conflict := state
	conflict.Generation++
	conflict.Phase = ProviderConfigured
	if _, err := store.Update(0, conflict); err == nil {
		t.Fatal("过期 generation 必须 CAS 冲突")
	}
	loaded, err := store.Load()
	if err != nil || loaded.Generation != state.Generation || !loaded.Operator.Verifier.Verify([]byte("a sufficiently long password")) {
		t.Fatalf("安全状态回读失败: %+v %v", loaded, err)
	}
}
