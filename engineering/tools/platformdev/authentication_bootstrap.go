package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	if err := ensurePrivate32ByteKey(r.portalSessionKeyPath(), "Portal session key"); err != nil {
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

func ensurePrivate32ByteKey(path, label string) error {
	if fileExists(path) {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("%s 必须是 owner-only 普通文件", label)
		}
		raw, err := os.ReadFile(path)
		if err != nil || len(raw) != 32 {
			return fmt.Errorf("%s 必须是 32 字节", label)
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
	reader := &broker.FileManagementStateReader{Path: path}
	current, err := reader.LoadState()
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
	if _, err := compareAndSwapDevelopmentManagementState(path, 0, next); err != nil {
		return fmt.Errorf("初始化开发 Authentication Provider Catalog: %w", err)
	}
	return nil
}

// compareAndSwapDevelopmentManagementState owns the development-only file
// mutation that formerly leaked through the Broker runtime adapter. Reads use
// the public Bootstrap reader so the persisted document keeps the plugin's
// validation rules; writes remain limited to platformdev's persistent state.
func compareAndSwapDevelopmentManagementState(path string, expected uint64, next broker.ManagementState) (broker.ManagementState, error) {
	current, err := (&broker.FileManagementStateReader{Path: path}).LoadState()
	if err != nil {
		return broker.ManagementState{}, err
	}
	if current.Generation == next.Generation && reflect.DeepEqual(current, next) {
		return current, nil
	}
	if current.Generation != expected || next.Generation != expected+1 {
		return broker.ManagementState{}, fmt.Errorf("开发 Authentication Provider Catalog CAS 冲突: expected=%d actual=%d next=%d", expected, current.Generation, next.Generation)
	}
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return broker.ManagementState{}, err
	}
	if err := writeDevelopmentManagementState(path, append(raw, '\n')); err != nil {
		return broker.ManagementState{}, err
	}
	return next, nil
}

func writeDevelopmentManagementState(path string, raw []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".authentication-providers-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		return err
	}
	if err := errors.Join(temporary.Sync(), temporary.Close()); err != nil {
		return err
	}
	if _, err := (&broker.FileManagementStateReader{Path: temporaryPath}).LoadState(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	committed = true
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
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
