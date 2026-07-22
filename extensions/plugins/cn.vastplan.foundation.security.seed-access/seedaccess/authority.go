package seedaccess

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

type Authority struct {
	store    Store
	recovery LocalRecoveryVerifier
	now      func() time.Time
}

func NewAuthority(store Store, recovery LocalRecoveryVerifier) (*Authority, error) {
	if store == nil {
		return nil, errors.New("Seed Access Store 不能为空")
	}
	return &Authority{store: store, recovery: recovery, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (a *Authority) Initialize(operatorID string, password []byte) (State, error) {
	current, err := a.store.Load()
	if err != nil {
		return State{}, err
	}
	if current.Phase != Uninitialized || current.Generation != 0 {
		return State{}, errors.New("Seed Access 已初始化")
	}
	if strings.TrimSpace(operatorID) == "" {
		return State{}, errors.New("Seed Operator ID 不能为空")
	}
	verifier, err := NewPasswordVerifier(password)
	if err != nil {
		return State{}, err
	}
	next := State{Version: StateVersion, Generation: 1, Phase: SeedActive, Operator: &Operator{ID: operatorID, Verifier: verifier}, UpdatedAt: a.now()}
	return a.store.Update(0, next)
}

func (a *Authority) Authenticate(operatorID string, password, recoveryToken []byte) error {
	state, err := a.store.Load()
	if err != nil {
		return err
	}
	if state.Operator == nil || state.Operator.ID != operatorID {
		return errors.New("Seed 凭据无效")
	}
	if state.Phase == EnterpriseActive || state.Phase == Uninitialized {
		return errors.New("Seed 登录当前不可用")
	}
	if state.Phase == RecoveryLease {
		if state.Recovery == nil || !a.now().Before(state.Recovery.ExpiresAt) || digest(recoveryToken) != state.Recovery.Digest {
			return errors.New("Recovery Lease 无效或已过期")
		}
	}
	if !state.Operator.Verifier.Verify(password) {
		return errors.New("Seed 凭据无效")
	}
	return nil
}

func (a *Authority) ConfigureProvider(expected uint64, profile compositioncommonv1.Ref) (State, error) {
	current, err := a.store.Load()
	if err != nil {
		return State{}, err
	}
	if current.Generation != expected || (current.Phase != SeedActive && current.Phase != ProviderConfigured) {
		return State{}, errors.New("Seed Access 不允许在当前状态配置 Provider")
	}
	if err := validRef(profile); err != nil {
		return State{}, err
	}
	next := current
	next.Generation++
	next.Phase = ProviderConfigured
	next.ProviderProfile = &profile
	next.ProviderSubject, next.Handoff = nil, nil
	next.UpdatedAt = a.now()
	return a.store.Update(expected, next)
}

func (a *Authority) VerifyProvider(expected uint64, profile compositioncommonv1.Ref, subject authenticationv1.SubjectIdentity) (State, error) {
	current, err := a.store.Load()
	if err != nil {
		return State{}, err
	}
	if current.Generation != expected || current.Phase != ProviderConfigured || current.ProviderProfile == nil || *current.ProviderProfile != profile {
		return State{}, errors.New("Provider 验证结果与当前配置不匹配")
	}
	if strings.TrimSpace(subject.ID) == "" || strings.TrimSpace(subject.Issuer) == "" {
		return State{}, errors.New("Provider 必须返回稳定 subject 和 issuer")
	}
	next := current
	next.Generation++
	next.Phase = ProviderVerified
	next.ProviderSubject = &subject
	next.UpdatedAt = a.now()
	return a.store.Update(expected, next)
}

func (a *Authority) PrepareHandoff(expected uint64, seal HandoffSeal) (State, error) {
	current, err := a.store.Load()
	if err != nil {
		return State{}, err
	}
	if current.Generation != expected || current.Phase != ProviderVerified || current.ProviderProfile == nil || current.ProviderSubject == nil {
		return State{}, errors.New("Seed Access 尚未满足交接前置状态")
	}
	if seal.ProviderProfile != *current.ProviderProfile || seal.Subject != *current.ProviderSubject {
		return State{}, errors.New("交接身份与已验证 Provider 不一致")
	}
	if err := validRef(seal.PolicySnapshot); err != nil {
		return State{}, fmt.Errorf("授权快照无效: %w", err)
	}
	if strings.TrimSpace(seal.SessionID) == "" || !seal.RecoveryReady || seal.AuthenticatedAt.IsZero() || !seal.ExpiresAt.After(a.now()) {
		return State{}, errors.New("交接必须包含有效普通 Session、授权快照和恢复通道")
	}
	seal.Digest = handoffDigest(seal)
	next := current
	next.Generation++
	next.Phase = HandoffReady
	next.Handoff = &seal
	next.UpdatedAt = a.now()
	return a.store.Update(expected, next)
}

func (a *Authority) CompleteHandoff(expected uint64, sealDigest string) (State, error) {
	current, err := a.store.Load()
	if err != nil {
		return State{}, err
	}
	if current.Generation != expected || current.Phase != HandoffReady || current.Handoff == nil || current.Handoff.Digest != sealDigest || !current.Handoff.ExpiresAt.After(a.now()) {
		return State{}, errors.New("交接 Seal 无效、过期或已被并发修改")
	}
	next := current
	next.Generation++
	next.Phase = EnterpriseActive
	next.Recovery = nil
	next.UpdatedAt = a.now()
	return a.store.Update(expected, next)
}

func (a *Authority) OpenRecovery(expected uint64, localProof []byte, ttl time.Duration) (State, []byte, error) {
	current, err := a.store.Load()
	if err != nil {
		return State{}, nil, err
	}
	if current.Generation != expected || current.Phase != EnterpriseActive || a.recovery == nil {
		return State{}, nil, errors.New("Seed Recovery 当前不可开启")
	}
	if ttl <= 0 || ttl > 15*time.Minute {
		return State{}, nil, errors.New("Recovery Lease TTL 必须在 15 分钟以内")
	}
	if err := a.recovery.VerifyLocalRecoveryProof(localProof); err != nil {
		return State{}, nil, errors.New("本机恢复证明无效")
	}
	token := make([]byte, 24)
	if _, err := rand.Read(token); err != nil {
		return State{}, nil, err
	}
	encodedToken := []byte(base64.RawURLEncoding.EncodeToString(token))
	next := current
	next.Generation++
	next.Phase = RecoveryLease
	next.Recovery = &Lease{Digest: digest(encodedToken), ExpiresAt: a.now().Add(ttl)}
	next.UpdatedAt = a.now()
	updated, err := a.store.Update(expected, next)
	if err != nil {
		return State{}, nil, err
	}
	return updated, encodedToken, nil
}

func (a *Authority) CloseRecovery(expected uint64) (State, error) {
	current, err := a.store.Load()
	if err != nil {
		return State{}, err
	}
	if current.Generation != expected || current.Phase != RecoveryLease {
		return State{}, errors.New("Recovery Lease 当前不可关闭")
	}
	next := current
	next.Generation++
	next.Phase = EnterpriseActive
	next.Recovery = nil
	next.UpdatedAt = a.now()
	return a.store.Update(expected, next)
}

func validRef(ref compositioncommonv1.Ref) error {
	if strings.TrimSpace(ref.ID) == "" || ref.Revision == 0 || len(ref.Digest) != 64 {
		return errors.New("引用必须精确包含 id、revision 和 digest")
	}
	return nil
}

func handoffDigest(seal HandoffSeal) string {
	seal.Digest = ""
	raw, _ := json.Marshal(seal)
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum)
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return fmt.Sprintf("%x", sum)
}
