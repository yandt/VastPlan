package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
	policy "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.authorization-policy/authorizationpolicy"
)

const developmentAuthorizationAudience = "development:local"

var developmentAuthorizationAudiences = []string{developmentAuthorizationAudience, "portal:local:operations"}

func (r *runtime) writeSessionsFromPublishedAuthorization() error {
	root := filepath.Join(r.persistentStateRoot(), "authorization")
	catalogPath := filepath.Join(root, "permission-catalog.json")
	raw, err := os.ReadFile(catalogPath)
	if errors.Is(err, os.ErrNotExist) {
		// A fresh zero-publication startup intentionally has no platform roles yet.
		return nil
	}
	if err != nil {
		return err
	}
	var catalog pluginv1.PermissionCatalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return fmt.Errorf("解析已发布权限目录: %w", err)
	}
	ownerPermissions := make([]string, 0, len(catalog.Permissions))
	for _, permission := range catalog.Permissions {
		if permission.Assignable {
			ownerPermissions = append(ownerPermissions, permission.Code)
		}
	}
	if err := writeSessions(filepath.Join(r.runDir, "secrets", "portal-sessions.json"), ownerPermissions); err != nil {
		return err
	}
	return nil
}

func (r *runtime) writeAuthorizationBootstrap(repository *artifactrepository.Repository, refs []artifactrepository.Ref) error {
	root := filepath.Join(r.persistentStateRoot(), "authorization")
	if err := ensurePrivateDirectory(root); err != nil {
		return err
	}
	directoryPath := filepath.Join(root, "directory-groups.json")
	if _, err := os.Stat(directoryPath); errors.Is(err, os.ErrNotExist) {
		projection := struct {
			Version  int                          `json:"version"`
			Revision uint64                       `json:"revision"`
			Subjects map[string][]json.RawMessage `json:"subjects"`
		}{Version: 1, Revision: 1, Subjects: map[string][]json.RawMessage{}}
		if err := writeOwnerJSON(directoryPath, projection); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	sources := make([]pluginv1.PermissionCatalogSource, 0, len(refs))
	for _, ref := range refs {
		artifact, _, err := repository.Read(ref)
		if err != nil {
			return err
		}
		manifest, err := pluginv1.ParseManifest(artifact.Manifest)
		if err != nil {
			return err
		}
		if manifest.Authorization != nil {
			sources = append(sources, pluginv1.PermissionCatalogSource{Manifest: manifest, ArtifactSHA256: artifact.SHA256})
		}
	}
	catalog, err := pluginv1.BuildPermissionCatalog(sources)
	if err != nil {
		return err
	}
	catalogPath := filepath.Join(root, "permission-catalog.json")
	if err := writeOwnerJSON(catalogPath, catalog); err != nil {
		return err
	}
	seedSubjectID, err := r.developmentSeedSubjectID()
	if err != nil {
		return err
	}
	grants := developmentGrants(catalog, seedSubjectID)
	ownerPermissions := make([]string, 0, len(catalog.Permissions))
	for _, permission := range catalog.Permissions {
		if permission.Assignable {
			ownerPermissions = append(ownerPermissions, permission.Code)
		}
	}
	if err := writeSessions(filepath.Join(r.runDir, "secrets", "portal-sessions.json"), ownerPermissions); err != nil {
		return err
	}
	signer, err := ensureAuthorizationSigner(root)
	if err != nil {
		return err
	}
	profile := policy.NativeProviderProfile(catalog)
	domain, err := policy.RootDomain(catalog, profile)
	if err != nil {
		return err
	}
	statePath, snapshotPath := filepath.Join(root, "policy-state.json"), filepath.Join(root, "policy-snapshot.json")
	store := &policy.FileStore{Path: statePath}
	if _, err := os.Stat(statePath); errors.Is(err, os.ErrNotExist) {
		state, buildErr := policy.BuildBootstrapState(catalog, profile, []authorizationv1.PolicyDomain{domain}, grants, time.Now().UTC())
		if buildErr != nil {
			return buildErr
		}
		snapshot, compileErr := policy.CompileSnapshot(state, developmentAuthorizationAudiences, time.Now().UTC(), 24*time.Hour)
		if compileErr != nil {
			return compileErr
		}
		publication, signErr := signer.Sign(snapshot)
		if signErr != nil {
			return signErr
		}
		state.CurrentSnapshot = &snapshot
		if _, err := store.CompareAndSwap(0, state); err != nil {
			return err
		}
		if err := policy.WriteSignedSnapshot(snapshotPath, publication.Snapshot); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		leasePolicy, leaseErr := policy.NewFixedSnapshotLeasePolicy(policy.SnapshotLeasePolicyOptions{
			Audiences: developmentAuthorizationAudiences, SnapshotTTL: 24 * time.Hour, RenewalLead: 6 * time.Hour,
		})
		if leaseErr != nil {
			return leaseErr
		}
		transitionTime := time.Now().UTC()
		if err := reconcileDevelopmentGrantsBeforeCatalogUpdate(store, catalog, grants, transitionTime); err != nil {
			return err
		}
		service, initErr := policy.NewService(policy.ServiceOptions{Store: store, Signer: signer, SnapshotWriter: policy.FileSnapshotWriter{Path: snapshotPath}, Catalog: catalog, ProviderProfile: profile, Domains: []authorizationv1.PolicyDomain{domain}, LeasePolicy: leasePolicy})
		if initErr != nil {
			return initErr
		}
		_ = service
		state, loadErr := store.Load()
		if loadErr != nil {
			return loadErr
		}
		if err := reconcileDevelopmentGrants(&state, grants, transitionTime); err != nil {
			return err
		}
		state.PolicyRevision++
		snapshot, compileErr := policy.CompileSnapshot(state, developmentAuthorizationAudiences, time.Now().UTC(), 24*time.Hour)
		if compileErr != nil {
			return compileErr
		}
		publication, signErr := signer.Sign(snapshot)
		if signErr != nil {
			return signErr
		}
		if err := policy.WriteSignedSnapshot(snapshotPath, publication.Snapshot); err != nil {
			return err
		}
		state.CurrentSnapshot = &snapshot
		state.Generation++
		state.Audit = append(state.Audit, policy.AuditEvent{ID: fmt.Sprintf("audit.dev.%d", time.Now().UnixNano()), Action: "developmentReconcile", ObjectKind: "policy", ObjectID: snapshot.SnapshotID, Revision: snapshot.Revision, SubjectID: "platformdev", OccurredAt: time.Now().UTC()})
		if _, err := store.CompareAndSwap(state.Generation-1, state); err != nil {
			return err
		}
	}
	return nil
}

// Explicit development bootstrap may legitimately shrink the Seed permission
// catalog after an unselected plugin moves to workspace/testing. Reconcile only
// seed-owned revision-1 roles before the policy service performs its strict
// catalog transition. User-created or subsequently revised roles retain the
// production fail-closed behavior and still block removal of permissions they
// actively use.
func reconcileDevelopmentGrantsBeforeCatalogUpdate(store *policy.FileStore, catalog pluginv1.PermissionCatalog, grants []policy.BootstrapGrant, now time.Time) error {
	state, err := store.Load()
	if err != nil {
		return err
	}
	if state.Generation == 0 || state.Catalog.Digest == catalog.Digest {
		return nil
	}
	previousGeneration := state.Generation
	profile := policy.NativeProviderProfile(catalog)
	domain, err := policy.RootDomain(catalog, profile)
	if err != nil {
		return err
	}
	if err := reconcileDevelopmentGrantsAgainst(&state, catalog, profile, []authorizationv1.PolicyDomain{domain}, grants, now); err != nil {
		return err
	}
	state.Generation++
	state.Audit = append(state.Audit, policy.AuditEvent{
		ID: fmt.Sprintf("audit.dev.catalog-transition.%d", now.UnixNano()), Action: "developmentSeedGrantReconcile",
		ObjectKind: "catalog", ObjectID: catalog.Digest, Revision: state.Generation, SubjectID: "platformdev", OccurredAt: now,
	})
	_, err = store.CompareAndSwap(previousGeneration, state)
	return err
}

func reconcileDevelopmentGrants(state *policy.State, grants []policy.BootstrapGrant, now time.Time) error {
	return reconcileDevelopmentGrantsAgainst(state, state.Catalog, state.ProviderProfile, state.Domains, grants, now)
}

func reconcileDevelopmentGrantsAgainst(state *policy.State, catalog pluginv1.PermissionCatalog, profile authorizationv1.ProviderProfile, domains []authorizationv1.PolicyDomain, grants []policy.BootstrapGrant, now time.Time) error {
	canonical, err := policy.BuildBootstrapState(catalog, profile, domains, grants, now)
	if err != nil {
		return fmt.Errorf("构建开发授权基线: %w", err)
	}
	_, err = (policy.SeedOwnedBootstrapReconciliation{}).Reconcile(state, &canonical, catalog.Digest, now)
	return err
}

func developmentGrants(catalog pluginv1.PermissionCatalog, seedSubjectID string) []policy.BootstrapGrant {
	known := map[string]struct{}{}
	for _, permission := range catalog.Permissions {
		if permission.Assignable {
			known[permission.Code] = struct{}{}
		}
	}
	filter := func(values ...string) []string {
		result := []string{}
		for _, value := range values {
			if _, ok := known[value]; ok {
				result = append(result, value)
			}
		}
		return result
	}
	grants := []policy.BootstrapGrant{{RoleID: "platform.owner", Title: "Development Platform Owner", SubjectID: "local-admin", PermissionSelectors: developmentOwnerPermissionSelectors(catalog)}}
	appendExactGrant := func(roleID, title, subjectID string, permissions ...string) {
		selectors := exactSelectors(filter(permissions...))
		if len(selectors) == 0 {
			return
		}
		grants = append(grants, policy.BootstrapGrant{RoleID: roleID, Title: title, SubjectID: subjectID, PermissionSelectors: selectors})
	}
	appendExactGrant("platform.deployment-author", "Development Deployment Author", "local-author", "platform.deployment.read", "platform.deployment.compose", "platform.portal.read", "platform.portal.compose")
	appendExactGrant("platform.deployment-approver", "Development Deployment Approver", "local-approver", "platform.deployment.read", "platform.deployment.approve", "platform.portal.read", "platform.portal.approve")
	appendExactGrant("platform.deployment-publisher", "Development Deployment Publisher", "local-publisher", "platform.deployment.read", "platform.deployment.publish", "platform.portal.read", "platform.portal.publish")
	if seedSubjectID != "" {
		grants = append(grants, policy.BootstrapGrant{RoleID: "platform.seed-owner", Title: "Development Seed Platform Owner", SubjectID: seedSubjectID, PermissionSelectors: developmentOwnerPermissionSelectors(catalog)})
	}
	return grants
}

func developmentOwnerPermissionSelectors(catalog pluginv1.PermissionCatalog) []policy.PermissionSelector {
	roots := map[string]struct{}{}
	for _, permission := range catalog.Permissions {
		if !permission.Assignable {
			continue
		}
		root, _, found := strings.Cut(permission.Code, ".")
		if found {
			roots[root] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(roots))
	for root := range roots {
		ordered = append(ordered, root)
	}
	sort.Strings(ordered)
	selectors := make([]policy.PermissionSelector, 0, len(ordered))
	for _, root := range ordered {
		selectors = append(selectors, policy.PermissionSelector{Kind: policy.PermissionSelectorGlob, Value: root + ".**"})
	}
	return selectors
}

func exactSelectors(values []string) []policy.PermissionSelector {
	selectors := make([]policy.PermissionSelector, 0, len(values))
	for _, value := range values {
		selectors = append(selectors, policy.PermissionSelector{Kind: policy.PermissionSelectorExact, Value: value})
	}
	return selectors
}

func ensureAuthorizationSigner(root string) (policy.Ed25519Signer, error) {
	path := filepath.Join(root, "policy-key.json")
	if _, err := os.Stat(path); err == nil {
		signer, loadErr := policy.LoadSigner(path)
		if loadErr != nil {
			return policy.Ed25519Signer{}, loadErr
		}
		if _, trustErr := os.Stat(filepath.Join(root, "policy-trust.json")); trustErr != nil {
			return policy.Ed25519Signer{}, errors.New("Authorization Policy 私钥存在但 trust 文件缺失")
		}
		return signer, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return policy.Ed25519Signer{}, err
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return policy.Ed25519Signer{}, err
	}
	document := map[string]string{"keyId": "development.policy.1", "privateKey": base64.RawStdEncoding.EncodeToString(private)}
	if err := writeOwnerJSON(path, document); err != nil {
		return policy.Ed25519Signer{}, err
	}
	trust := struct {
		Version int `json:"version"`
		Keys    []struct {
			KeyID     string `json:"keyId"`
			PublicKey string `json:"publicKey"`
		} `json:"keys"`
	}{Version: 1}
	trust.Keys = append(trust.Keys, struct {
		KeyID     string `json:"keyId"`
		PublicKey string `json:"publicKey"`
	}{"development.policy.1", base64.RawStdEncoding.EncodeToString(public)})
	if err := writeOwnerJSON(filepath.Join(root, "policy-trust.json"), trust); err != nil {
		return policy.Ed25519Signer{}, err
	}
	return policy.Ed25519Signer{KeyID: "development.policy.1", Private: private}, nil
}

func writeOwnerJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}
