package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func (r *runtime) writeFixtures(ctx context.Context) error {
	certFile, keyFile := filepath.Join(r.runDir, "secrets", "tls-cert.pem"), filepath.Join(r.runDir, "secrets", "tls-key.pem")
	if err := writeTLS(certFile, keyFile); err != nil {
		return err
	}
	seedTrust, err := ensureSigningIdentity(filepath.Join(r.runDir, "secrets", "artifact-signing.pem"), "vastplan", "local-development")
	if err != nil {
		return fmt.Errorf("创建本次 Seed 签名身份: %w", err)
	}
	testingTrust, err := ensureSigningIdentity(r.testingRepositorySigningKey(), "vastplan", "local-testing")
	if err != nil {
		return fmt.Errorf("准备持久化测试签名身份: %w", err)
	}
	if err := writeTrustDocument(filepath.Join(r.runDir, "secrets", "artifact-trust.json"), seedTrust, testingTrust); err != nil {
		return err
	}
	if err := writeTrustDocument(filepath.Join(r.runDir, "secrets", "seed-artifact-trust.json"), seedTrust); err != nil {
		return err
	}
	if err := writeTrustDocument(r.testingRepositoryTrust(), testingTrust); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.runDir, "secrets", "artifact-read.token"), []byte("vastplan-local-artifact-reader\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.runDir, "secrets", "artifact-publish.token"), []byte("vastplan-local-artifact-publisher\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.runDir, "secrets", "artifact-bundle.token"), []byte("vastplan-local-artifact-bundle\n"), 0o600); err != nil {
		return err
	}
	repositoryProfile, err := r.prepareTestingRepositoryProtocol()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.runDir, "secrets", "vault-token"), []byte("vastplan-local-vault-token\n"), 0o600); err != nil {
		return err
	}
	if err := writeSessions(filepath.Join(r.runDir, "secrets", "portal-sessions.json"), nil); err != nil {
		return err
	}
	if err := writeDevelopmentTransportIdentities(filepath.Join(r.runDir, "secrets")); err != nil {
		return err
	}
	template, err := os.ReadFile(filepath.Join(r.options.root, "engineering", "deploy", "platform-management-profile.json"))
	if err != nil {
		return err
	}
	application, err := os.ReadFile(filepath.Join(r.options.root, "engineering", "deploy", "platform-management-application.json"))
	if err != nil {
		return err
	}
	portalCatalog, err := os.ReadFile(filepath.Join(r.options.root, "engineering", "deploy", "portal-platform-catalog.json"))
	if err != nil {
		return err
	}
	compiledPortalCatalog, err := r.compilePortalPlatformCatalog(portalCatalog)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.runDir, "portal-platform-catalog.json"), compiledPortalCatalog, 0o600); err != nil {
		return err
	}
	rendered, err := renderPlatformProfile(template, portalCatalog, r.runDir, r.persistentStateRoot(), r.options.artifactListen, repositoryProfile)
	if err != nil {
		return err
	}
	backendDigest, err := r.computeBackendBuildDigest(ctx)
	if err != nil {
		return err
	}
	r.backendInputDigest = backendDigest
	dynamicFingerprint := digestStrings("dynamic-go-not-selected-v1")
	selection, err := r.seedSelection()
	if err != nil {
		return err
	}
	if selection.contains("cn.vastplan.foundation.security.bootstrap-policy") {
		dynamicFingerprint, err = r.dynamicGoFingerprint(ctx, filepath.Join(r.options.stateRoot, "go-cache"))
		if err != nil {
			return fmt.Errorf("计算开发平台 dynamic-go 制品指纹: %w", err)
		}
	}
	sourceDigest, err := platformManagementSourceDigest(template, application, portalCatalog, repositoryProfile, backendDigest, dynamicFingerprint)
	if err != nil {
		return err
	}
	rendered, err = prepareDevelopmentPlatformProfile(
		rendered, sourceDigest,
		filepath.Join(r.persistentStateRoot(), "platform-management-revision.json"),
		r.options.applyPlatform,
	)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.runDir, "platform-management-profile.json"), rendered, 0o600); err != nil {
		return err
	}
	managedProfile, err := backendcompositionv1.ParsePlatformProfileFile(filepath.Join(r.options.root, "engineering", "deploy", "managed-services-profile.json"))
	if err != nil {
		return err
	}
	catalog := backendcompositionv1.BackendPlatformCatalog{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "local-backend-platform"},
		Profiles: []backendcompositionv1.PlatformProfile{managedProfile},
		Bindings: []backendcompositionv1.BackendPlatformBinding{{TenantID: "local", DeploymentName: "managed-services", PlatformProfile: compositioncommonv1.Ref{ID: managedProfile.ID, Revision: managedProfile.Revision, Digest: managedProfile.Digest()}}},
	}
	catalogRaw, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.runDir, "backend-platform-catalog.json"), catalogRaw, 0o600); err != nil {
		return err
	}
	if err := materializeAccessProfileCatalog(
		filepath.Join(r.options.root, "engineering", "deploy", "portal-access-profile-catalog.json"),
		filepath.Join(r.options.root, "engineering", "deploy", "portal-platform-catalog.json"),
		filepath.Join(r.runDir, "access-profile-catalog.json"),
	); err != nil {
		return err
	}
	return r.writeSeedRepositoryProfile()
}

func (r *runtime) writeSeedRepositoryProfile() error {
	profile := fmt.Sprintf("version: 1\nid: seed-repository\nlisten: %s\nrepositoryRoot: %s\ntrustFile: %s\ntlsCertFile: %s\ntlsKeyFile: %s\nreadTokenFile: %s\npublishTokenFile: %s\n",
		yamlString(r.options.seedArtifactListen), yamlString(filepath.Join(r.runDir, "repository")),
		yamlString(filepath.Join(r.runDir, "secrets", "seed-artifact-trust.json")), yamlString(filepath.Join(r.runDir, "secrets", "tls-cert.pem")),
		yamlString(filepath.Join(r.runDir, "secrets", "tls-key.pem")), yamlString(filepath.Join(r.runDir, "secrets", "artifact-read.token")),
		yamlString(filepath.Join(r.runDir, "secrets", "artifact-publish.token")))
	return os.WriteFile(filepath.Join(r.runDir, "seed-repository.yaml"), []byte(profile), 0o600)
}

func yamlString(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

// compilePortalPlatformCatalog is the development composition root for the
// browser-management surface. It reads only manifests selected by the Profile,
// derives exact operation grants, and writes the resulting immutable catalog
// into the run directory. The source catalog never carries an operator-authored
// operation allow-list.
func (r *runtime) compilePortalPlatformCatalog(raw []byte) ([]byte, error) {
	catalog, err := frontendcompositionv1.ParsePortalPlatformCatalog(raw)
	if err != nil {
		return nil, fmt.Errorf("解析 Portal Platform Catalog: %w", err)
	}
	compiled, err := frontendcompositionv1.CompilePortalBrowserExposure(catalog, func(ref frontendcompositionv1.PluginRef) (pluginv1.Manifest, error) {
		return readLocalPluginManifest(r.options.root, ref.ID)
	})
	if err != nil {
		return nil, fmt.Errorf("编译 Portal 浏览器暴露: %w", err)
	}
	compiledRaw, err := json.Marshal(compiled)
	if err != nil {
		return nil, fmt.Errorf("序列化已编译 Portal Platform Catalog: %w", err)
	}
	return compiledRaw, nil
}

func renderPlatformProfile(template, portalCatalog []byte, runDir, stateDir, artifactListen string, repositoryProfile artifactrepositoryv1.Profile) ([]byte, error) {
	rendered := bytes.ReplaceAll(template, []byte("__VASTPLAN_DEV_ROOT__"), []byte(filepath.ToSlash(runDir)))
	rendered = bytes.ReplaceAll(rendered, []byte("__VASTPLAN_DEV_STATE__"), []byte(filepath.ToSlash(stateDir)))
	rendered = bytes.ReplaceAll(rendered, []byte("__VASTPLAN_ARTIFACT_LISTEN__"), []byte(artifactListen))
	profileRaw, err := json.Marshal(repositoryProfile)
	if err != nil {
		return nil, err
	}
	rendered = bytes.ReplaceAll(rendered, []byte(`"__VASTPLAN_ARTIFACT_PROFILE__"`), profileRaw)
	var canonicalCatalog any
	if err := json.Unmarshal(portalCatalog, &canonicalCatalog); err != nil {
		return nil, fmt.Errorf("解析 Portal Platform Catalog: %w", err)
	}
	canonicalRaw, err := json.Marshal(canonicalCatalog)
	if err != nil {
		return nil, err
	}
	quotedCatalog, err := json.Marshal(string(canonicalRaw))
	if err != nil {
		return nil, err
	}
	rendered = bytes.ReplaceAll(rendered, []byte(`"__VASTPLAN_PORTAL_PLATFORM_CATALOG__"`), quotedCatalog)
	profile, err := backendcompositionv1.ParsePlatformProfile(rendered)
	if err != nil {
		return nil, fmt.Errorf("解析开发 Platform Profile 模板: %w", err)
	}
	for index := range profile.Services {
		if profile.Services[index].ID == "platform-artifacts" && repositoryProfile.Protocol == artifactrepositoryv1.ProtocolLocalTest {
			plugins, ok := profile.Services[index].Config["plugins"].(map[string]any)
			if !ok {
				return nil, errors.New("开发 Platform Profile 的 platform-artifacts plugins 配置无效")
			}
			repository, ok := plugins["cn.vastplan.platform.artifacts.repository"].(map[string]any)
			if !ok {
				return nil, errors.New("开发 Platform Profile 缺少仓库插件配置")
			}
			delete(repository, "listen")
		}
		if profile.Services[index].ID == "platform-database-runtime" {
			// 开发编排器只有一个 local-platform 节点。生产模板保持两个
			// active-active 副本，开发投影显式缩为一个，避免伪造第二节点。
			profile.Services[index].Replicas = 1
			raw, err := json.MarshalIndent(profile, "", "  ")
			if err != nil {
				return nil, err
			}
			return append(raw, '\n'), nil
		}
	}
	return nil, errors.New("开发 Platform Profile 缺少 platform-database-runtime")
}
