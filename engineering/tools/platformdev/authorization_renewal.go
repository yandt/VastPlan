package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	policy "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.authorization-policy/authorizationpolicy"
)

const (
	developmentAuthorizationTTL         = 24 * time.Hour
	developmentAuthorizationRenewalLead = 6 * time.Hour
)

// renewPublishedDevelopmentAuthorization renews only the already-published
// local development policy material. It cannot introduce a permission catalog,
// change a catalog digest, create a signing identity, or publish any platform
// composition. That keeps zero-publication startup distinct from bootstrap while
// preventing a restored developer session from being paired with an expired LKG.
func renewPublishedDevelopmentAuthorization(root string, catalog pluginv1.PermissionCatalog, now time.Time) (bool, error) {
	statePath := filepath.Join(root, "policy-state.json")
	snapshotPath := filepath.Join(root, "policy-snapshot.json")
	store := &policy.FileStore{Path: statePath}
	state, err := store.Load()
	if err != nil {
		return false, fmt.Errorf("读取已发布开发授权状态: %w", err)
	}
	if state.Generation == 0 || state.CurrentSnapshot == nil {
		return false, errors.New("已发布开发授权状态缺失；请显式执行 bootstrap")
	}
	if catalog.Digest == "" || state.Catalog.Digest != catalog.Digest {
		return false, errors.New("已发布开发授权目录与策略不一致；请显式执行 bootstrap")
	}
	publishedRaw, err := os.ReadFile(snapshotPath)
	if err != nil {
		return false, fmt.Errorf("读取已发布开发授权 Snapshot: %w", err)
	}
	published, err := authorizationv1.ParseSignedPolicySnapshot(publishedRaw)
	if err != nil {
		return false, fmt.Errorf("解析已发布开发授权 Snapshot: %w", err)
	}
	if published.Payload.Revision != state.CurrentSnapshot.Revision || published.Payload.Policy.CatalogDigest != state.Catalog.Digest {
		return false, errors.New("运行期授权 Snapshot 已超出本地 Bootstrap 状态；零发布启动拒绝覆盖")
	}
	now = now.UTC()
	if developmentAuthorizationStillValid(state, now.Add(developmentAuthorizationRenewalLead)) {
		return false, nil
	}
	signer, err := loadPublishedDevelopmentAuthorizationSigner(root)
	if err != nil {
		return false, err
	}
	previousGeneration := state.Generation
	seedSubjectID := ""
	for _, binding := range state.Bindings {
		if binding.RoleID == "platform.seed-owner" {
			seedSubjectID = binding.Subject.ID
			break
		}
	}
	if err := reconcileDevelopmentGrants(&state, developmentGrants(catalog, seedSubjectID), now); err != nil {
		return false, err
	}
	state.PolicyRevision++
	snapshot, err := policy.CompileSnapshot(state, developmentAuthorizationAudiences, now, developmentAuthorizationTTL)
	if err != nil {
		return false, fmt.Errorf("编译开发授权续签 Snapshot: %w", err)
	}
	publication, err := signer.Sign(snapshot)
	if err != nil {
		return false, fmt.Errorf("签署开发授权续签 Snapshot: %w", err)
	}
	if err := policy.WriteSignedSnapshot(snapshotPath, publication.Snapshot); err != nil {
		return false, err
	}
	state.CurrentSnapshot = &snapshot
	state.Generation++
	state.Audit = append(state.Audit, policy.AuditEvent{
		ID: fmt.Sprintf("audit.dev.renew.%d", now.UnixNano()), Action: "developmentLeaseRenewed",
		ObjectKind: "policy-snapshot", ObjectID: snapshot.SnapshotID, Revision: snapshot.Revision,
		SubjectID: "platformdev", Reason: "zero-publication runtime renewal", OccurredAt: now,
	})
	if _, err := store.CompareAndSwap(previousGeneration, state); err != nil {
		return false, err
	}
	return true, nil
}

func developmentAuthorizationStillValid(state policy.State, threshold time.Time) bool {
	if state.CurrentSnapshot == nil || !state.CurrentSnapshot.ExpiresAt.After(threshold) {
		return false
	}
	foundSeedBinding := false
	for _, binding := range state.Bindings {
		if binding.Revision != 1 || binding.CreatedBy != "seed-authority" {
			continue
		}
		foundSeedBinding = true
		if !binding.ExpiresAt.After(threshold) {
			return false
		}
	}
	return foundSeedBinding
}

func loadPublishedDevelopmentAuthorizationSigner(root string) (policy.Ed25519Signer, error) {
	keyPath := filepath.Join(root, "policy-key.json")
	trustPath := filepath.Join(root, "policy-trust.json")
	for _, path := range []string{keyPath, trustPath} {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
			return policy.Ed25519Signer{}, errors.New("已发布开发授权签名材料缺失或权限无效；请显式执行 bootstrap")
		}
	}
	signer, err := policy.LoadSigner(keyPath)
	if err != nil {
		return policy.Ed25519Signer{}, fmt.Errorf("加载已发布开发授权签名身份: %w", err)
	}
	return signer, nil
}
