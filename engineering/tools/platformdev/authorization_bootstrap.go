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
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	policy "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.authorization-policy/authorizationpolicy"
)

const developmentAuthorizationAudience = "development:local"

func (r *runtime) writeSessionsFromPublishedAuthorization() error {
	catalogPath := filepath.Join(r.persistentStateRoot(), "authorization", "permission-catalog.json")
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
	return writeSessions(filepath.Join(r.runDir, "secrets", "portal-sessions.json"), ownerPermissions)
}

func (r *runtime) writeAuthorizationBootstrap(repository *pluginservice.Repository, refs []pluginservice.Ref) error {
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
		state, buildErr := policy.BuildBootstrapState(catalog, profile, []authorizationv1.PolicyDomain{domain}, developmentGrants(catalog), time.Now().UTC())
		if buildErr != nil {
			return buildErr
		}
		snapshot, compileErr := policy.CompileSnapshot(state, []string{developmentAuthorizationAudience}, time.Now().UTC(), 24*time.Hour)
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
		service, initErr := policy.NewService(policy.ServiceOptions{Store: store, Signer: signer, SnapshotWriter: policy.FileSnapshotWriter{Path: snapshotPath}, Catalog: catalog, ProviderProfile: profile, Domains: []authorizationv1.PolicyDomain{domain}, DefaultAudience: []string{developmentAuthorizationAudience}, DefaultTTL: 24 * time.Hour})
		if initErr != nil {
			return initErr
		}
		_ = service
		state, loadErr := store.Load()
		if loadErr != nil {
			return loadErr
		}
		reconcileDevelopmentGrants(&state, catalog, time.Now().UTC())
		state.PolicyRevision++
		snapshot, compileErr := policy.CompileSnapshot(state, []string{developmentAuthorizationAudience}, time.Now().UTC(), 24*time.Hour)
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

func reconcileDevelopmentGrants(state *policy.State, catalog pluginv1.PermissionCatalog, now time.Time) {
	grants := developmentGrants(catalog)
	byRole := make(map[string]policy.BootstrapGrant, len(grants))
	for _, grant := range grants {
		byRole[grant.RoleID] = grant
	}
	for index := range state.Roles {
		grant, ok := byRole[state.Roles[index].ID]
		if !ok || state.Roles[index].Revision != 1 || state.Roles[index].CreatedBy != "seed-authority" {
			continue
		}
		state.Roles[index].Statements = []authorizationv1.PolicyStatement{{ID: "bootstrap-allow", Effect: authorizationv1.EffectAllow, Permissions: append([]string(nil), grant.Permissions...), Constraints: []authorizationv1.AttributeConstraint{}}}
		state.Roles[index].UpdatedAt = now
	}
	for index := range state.Bindings {
		if _, ok := byRole[state.Bindings[index].RoleID]; !ok || state.Bindings[index].Revision != 1 || state.Bindings[index].CreatedBy != "seed-authority" {
			continue
		}
		state.Bindings[index].NotBefore = now.Add(-time.Minute)
		state.Bindings[index].ExpiresAt = now.Add(24 * time.Hour)
		state.Bindings[index].UpdatedAt = now
	}
}

func developmentGrants(catalog pluginv1.PermissionCatalog) []policy.BootstrapGrant {
	all := []string{}
	known := map[string]struct{}{}
	for _, permission := range catalog.Permissions {
		if permission.Assignable {
			all = append(all, permission.Code)
			known[permission.Code] = struct{}{}
		}
	}
	sort.Strings(all)
	filter := func(values ...string) []string {
		result := []string{}
		for _, value := range values {
			if _, ok := known[value]; ok {
				result = append(result, value)
			}
		}
		return result
	}
	return []policy.BootstrapGrant{
		{RoleID: "platform.owner", Title: "Development Platform Owner", SubjectID: "local-admin", Permissions: all},
		{RoleID: "platform.deployment-author", Title: "Development Deployment Author", SubjectID: "local-author", Permissions: filter("platform.deployment.read", "platform.deployment.compose")},
		{RoleID: "platform.deployment-approver", Title: "Development Deployment Approver", SubjectID: "local-approver", Permissions: filter("platform.deployment.read", "platform.deployment.approve")},
		{RoleID: "platform.deployment-publisher", Title: "Development Deployment Publisher", SubjectID: "local-publisher", Permissions: filter("platform.deployment.read", "platform.deployment.publish")},
	}
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
