package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	broker "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.authentication-broker/broker"
	seedaccess "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.seed-access/seedaccess"
)

const (
	developmentSeedProviderProfileID = "local-seed-access"
	developmentSeedIssuer            = "vastplan://seed-access"
)

func (r *runtime) authenticationRoot() string {
	return filepath.Join(r.persistentStateRoot(), "authentication")
}

func (r *runtime) authenticationAssertionKeyPath() string {
	return filepath.Join(r.authenticationRoot(), "assertion-key.json")
}

func (r *runtime) authenticationAssertionTrustPath() string {
	return filepath.Join(r.authenticationRoot(), "assertion-trust.json")
}

func (r *runtime) authenticationProviderStatePath() string {
	return filepath.Join(r.authenticationRoot(), "provider-state.json")
}

func (r *runtime) portalSessionKeyPath() string {
	return filepath.Join(r.authenticationRoot(), "portal-session.key")
}

func (r *runtime) seedAccessStatePath() string {
	return filepath.Join(r.authenticationRoot(), "seed-access.json")
}

func (r *runtime) ensureDevelopmentAuthenticationMaterial() error {
	if err := ensurePrivateDirectory(r.authenticationRoot()); err != nil {
		return fmt.Errorf("准备开发认证目录: %w", err)
	}
	if err := ensureAuthenticationAssertionIdentity(r.authenticationAssertionKeyPath(), r.authenticationAssertionTrustPath()); err != nil {
		return err
	}
	if err := ensurePortalSessionKey(r.portalSessionKeyPath()); err != nil {
		return err
	}
	if err := ensureDevelopmentProviderState(r.authenticationProviderStatePath()); err != nil {
		return err
	}
	return nil
}

func ensureAuthenticationAssertionIdentity(keyPath, trustPath string) error {
	key, err := broker.LoadAssertionKey(keyPath)
	if errors.Is(err, os.ErrNotExist) || err != nil && !fileExists(keyPath) {
		key, err = broker.GenerateAssertionKey("development.authentication.1")
		if err != nil {
			return fmt.Errorf("生成 Authentication Assertion key: %w", err)
		}
		if err := writeOwnerJSON(keyPath, map[string]string{
			"keyId": key.KeyID, "privateKey": base64.RawStdEncoding.EncodeToString(key.Private),
		}); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("加载 Authentication Assertion key: %w", err)
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
	}{key.KeyID, base64.RawStdEncoding.EncodeToString(key.Public)})
	return writeOwnerJSON(trustPath, trust)
}

func ensurePortalSessionKey(path string) error {
	if fileExists(path) {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
			return errors.New("Portal session key 必须是 owner-only 普通文件")
		}
		raw, err := os.ReadFile(path)
		if err != nil || len(raw) != 32 {
			return errors.New("Portal session key 必须是 32 字节")
		}
		return nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func ensureDevelopmentProviderState(path string) error {
	store := &broker.FileManagementStore{Path: path}
	current, err := store.LoadState()
	if err != nil {
		return err
	}
	if current.Generation != 0 {
		return nil
	}
	configDigest := sha256.Sum256([]byte(`{"kind":"seed-local"}`))
	profile := authenticationv1.AuthenticationProviderProfile{
		Document:       compositioncommonv1.Document{Version: 1, Revision: 1, ID: developmentSeedProviderProfileID},
		ContributionID: "seed-local",
		Configuration:  compositioncommonv1.Ref{ID: "local-seed-access-config", Revision: 1, Digest: fmt.Sprintf("%x", configDigest[:])},
		Purposes:       []authenticationv1.ProviderPurpose{authenticationv1.PurposePortalLogin},
		Methods:        []string{"seed-password"}, SubjectNamespace: "foundation.identity.seed", RequiredCapabilities: []string{},
	}
	profileRef := compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()}
	now := time.Now().UTC()
	catalog := authenticationv1.AuthenticationProviderCatalog{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "local-authentication-providers"},
		Providers: []authenticationv1.ProviderCatalogEntry{{
			Profile: profileRef, ContributionID: profile.ContributionID, Purposes: profile.Purposes,
			Methods: profile.Methods, SubjectNamespace: profile.SubjectNamespace, RequiredCapabilities: []string{},
		}},
		Bindings: []authenticationv1.ProviderBinding{{
			TenantID: "local", PortalID: "operations", DefaultProvider: profile.ID, AllowedProviders: []string{profile.ID},
		}},
	}
	next := broker.ManagementState{
		Version: 1, Generation: 1, UpdatedAt: now,
		Providers: []broker.ManagedProvider{{
			Profile: profile,
			Lifecycle: authenticationv1.AuthenticationProviderLifecycle{
				SchemaVersion: "v1", Profile: profileRef, State: authenticationv1.ProviderPublished,
				Readiness: authenticationv1.ProviderReady, UnmetCapabilities: []string{}, UpdatedAt: now,
			},
		}},
		Catalog: &catalog,
	}
	if _, err := store.UpdateState(0, next); err != nil {
		return fmt.Errorf("初始化开发 Authentication Provider Catalog: %w", err)
	}
	return nil
}

func (r *runtime) developmentSeedSubjectID() (string, error) {
	// Seed Access owns exactly one operator. Its provider projects that operator
	// as a fixed non-PII subject, so the authorization baseline can be prepared
	// before the first password is enrolled. The actual account identifier stays
	// inside the protected Seed Authority state and is never copied to a policy
	// snapshot or Portal session.
	return authenticationv1.StableSubjectID(developmentSeedProviderProfileID, developmentSeedIssuer, seedaccess.SeedOperatorSubjectID), nil
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
