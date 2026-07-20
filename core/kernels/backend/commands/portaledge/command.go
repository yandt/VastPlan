// Package portaledgecommand assembles the production Portal BFF from verified
// artifacts, a trusted session boundary, and the Backend protocol host.
package portaledgecommand

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/edge"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/hostfactory"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

type verifierAdapter struct{ verifier nodeagent.ArtifactVerifier }

func (a verifierAdapter) Verify(_ context.Context, ref pluginv1.ArtifactRef, envelope artifacttrust.Envelope) (pluginv1.Artifact, error) {
	verified, err := a.verifier.Verify(ref, envelope)
	if err != nil {
		return pluginv1.Artifact{}, err
	}
	return verified.Artifact(), nil
}

// Run starts the Portal BFF. It deliberately accepts a Composer artifact ref,
// never a raw executable path: the package is verified and installed before
// its signed runtime contract becomes a LaunchPolicy.
func Run(ctx context.Context, args []string, version string, logf func(string, ...any)) error {
	flags := flag.NewFlagSet("portal-edge", flag.ContinueOnError)
	listen := flags.String("listen", "127.0.0.1:8443", "HTTPS listen address")
	cert := flags.String("tls-cert", "", "HTTPS certificate PEM")
	key := flags.String("tls-key", "", "HTTPS private key PEM")
	sessions := flags.String("session-file", "", "0600 session digest JSON")
	repositoryRoot := flags.String("repository", ".vastplan/repository", "local immutable artifact repository")
	repositoryURL := flags.String("repository-url", "", "HTTPS managed artifact repository used when an exact Seed ref is absent")
	repositoryTrust := flags.String("repository-trust", "", "managed repository publisher trust document")
	repositoryToken := flags.String("repository-token", "", "managed repository read token; defaults to VASTPLAN_ARTIFACT_READ_TOKEN")
	repositoryCA := flags.String("repository-ca", "", "managed repository custom CA PEM")
	installRoot := flags.String("install-root", ".vastplan/portal-edge/plugins", "content-addressed plugin install root")
	deliveryOrigin := flags.String("frontend-delivery-origin", "", "shared trusted Portal frontend delivery origin")
	deliveryCache := flags.String("frontend-delivery-cache", ".vastplan/portal-edge/frontend-cache", "this Portal Edge node's private frontend cache")
	prefetchInterval := flags.Duration("frontend-prefetch-interval", 2*time.Second, "active Portal snapshot prefetch interval")
	trustFile := flags.String("trust-store", "", "artifact publisher trust document")
	allowUnsigned := flags.Bool("allow-unsigned-local", false, "local development only: permit unsigned artifacts")
	pluginID := flags.String("composer-plugin", "cn.vastplan.platform.configuration.portal-composer", "Composer plugin ID")
	pluginVersion := flags.String("composer-version", "", "Composer artifact version")
	channel := flags.String("composer-channel", "stable", "Composer artifact channel")
	policyID := flags.String("policy-plugin", "cn.vastplan.foundation.security.portal-access-policy", "Portal authorization policy plugin ID")
	policyVersion := flags.String("policy-version", "0.1.0", "Portal authorization policy artifact version")
	policyChannel := flags.String("policy-channel", "stable", "Portal authorization policy artifact channel")
	interactionPolicyID := flags.String("interaction-policy-plugin", "cn.vastplan.foundation.security.interaction-access-policy", "Interaction authorization policy plugin ID")
	interactionPolicyVersion := flags.String("interaction-policy-version", "0.1.0", "Interaction authorization policy artifact version")
	interactionPolicyChannel := flags.String("interaction-policy-channel", "stable", "Interaction authorization policy artifact channel")
	brokerID := flags.String("interaction-broker-plugin", "cn.vastplan.platform.interaction.broker", "Interaction Broker plugin ID")
	brokerVersion := flags.String("interaction-broker-version", "", "Interaction Broker artifact version")
	brokerChannel := flags.String("interaction-broker-channel", "stable", "Interaction Broker artifact channel")
	stateFile := flags.String("composer-state-file", "", "Composer governed-state file")
	platformCatalogFile := flags.String("portal-platform-catalog", "", "Portal Platform Catalog v1 JSON")
	brokerStateFile := flags.String("interaction-broker-state-file", "", "Interaction Broker governed-state file")
	portalAssetsDir := flags.String("portal-assets", "", "Portal Shell 静态产物目录")
	natsURL := flags.String("nats-url", "", "平台管理远端 capability NATS URL；留空关闭平台管理 API")
	natsCA := flags.String("nats-ca", "", "NATS CA PEM")
	natsCert := flags.String("nats-cert", "", "NATS mTLS 客户端证书 PEM")
	natsKey := flags.String("nats-key", "", "NATS mTLS 客户端私钥 PEM")
	natsSeed := flags.String("nats-seed", "", "Portal Edge NKey seed（0600）")
	natsAllowInsecure := flags.Bool("nats-allow-insecure", false, "仅本地开发：允许明文匿名 NATS")
	transportSeed := flags.String("transport-seed", "", "Portal Edge addressing 签名 NKey seed（0600）")
	transportTrust := flags.String("transport-trust", "", "addressing 传输身份信任文档")
	nodeID := flags.String("node-id", "portal-edge", "Portal Edge addressing 节点身份")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *cert == "" || *key == "" || *sessions == "" || *pluginVersion == "" || *stateFile == "" || *platformCatalogFile == "" || *brokerVersion == "" || *brokerStateFile == "" || *portalAssetsDir == "" || *deliveryOrigin == "" || *deliveryCache == "" || *prefetchInterval <= 0 {
		return errors.New("portal-edge 必须配置 TLS、session、Portal Platform Catalog、Portal 静态产物、中央交付 origin、本地缓存、Composer 与 Interaction Broker 制品版本及状态文件")
	}
	if *allowUnsigned && *trustFile != "" {
		return errors.New("allow-unsigned-local 与 trust-store 不能同时使用")
	}
	platformOptions := platformRouterOptions{URL: *natsURL, CA: *natsCA, Cert: *natsCert, Key: *natsKey, NKeySeed: *natsSeed, TransportSeed: *transportSeed, TransportTrust: *transportTrust, NodeID: *nodeID, AllowInsecure: *natsAllowInsecure}
	if err := platformOptions.validate(); err != nil {
		return err
	}
	if *repositoryToken == "" {
		*repositoryToken = os.Getenv("VASTPLAN_ARTIFACT_READ_TOKEN")
	}
	remoteSource, remoteTrust, err := buildPortalRemoteArtifactSource(*repositoryURL, *repositoryTrust, *repositoryToken, *repositoryCA)
	if err != nil {
		return err
	}
	var verifier nodeagent.ArtifactVerifier
	if *allowUnsigned {
		if remoteSource == nil {
			verifier = nodeagent.NewLocalDevelopmentArtifactVerifier()
		} else {
			verifier, err = nodeagent.NewLocalDevelopmentArtifactVerifierWithTrust(remoteTrust)
			if err != nil {
				return err
			}
		}
	} else {
		if *trustFile == "" {
			return errors.New("生产 portal-edge 必须配置 trust-store")
		}
		trust, err := pluginservice.LoadTrustStore(*trustFile)
		if err != nil {
			return err
		}
		verifier, err = nodeagent.NewSignedArtifactVerifier(trust)
		if err != nil {
			return err
		}
	}
	repository, err := pluginservice.NewRepository(*repositoryRoot)
	if err != nil {
		return err
	}
	ref := pluginv1.ArtifactRef{PluginID: *pluginID, Version: *pluginVersion, Channel: *channel}
	envelope, err := repository.Fetch(ctx, ref)
	if err != nil {
		return fmt.Errorf("读取 Composer 制品: %w", err)
	}
	verified, err := verifier.Verify(ref, envelope)
	if err != nil {
		return fmt.Errorf("验证 Composer 制品: %w", err)
	}
	installed, err := (nodeagent.LocalInstaller{Root: *installRoot}).Install(verified)
	if err != nil {
		return fmt.Errorf("安装 Composer 制品: %w", err)
	}
	identity, err := edge.NewFileIdentityProvider(*sessions)
	if err != nil {
		return err
	}
	portalAssets, err := edge.NewPortalAssets(*portalAssetsDir)
	if err != nil {
		return fmt.Errorf("加载 Portal 静态产物: %w", err)
	}
	artifactSources := []edge.ArtifactSource{repository}
	if remoteSource != nil {
		artifactSources = append(artifactSources, remoteSource)
	}
	catalog, err := edge.NewTrustedCatalog(artifactSources, verifierAdapter{verifier}, edge.WithFrontendDeliveryDistribution(*deliveryOrigin, *deliveryCache))
	if err != nil {
		return err
	}
	catalogRaw, err := os.ReadFile(*platformCatalogFile)
	if err != nil {
		return fmt.Errorf("读取 Portal Platform Catalog: %w", err)
	}
	platformCatalog, err := frontendcompositionv1.ParsePortalPlatformCatalog(catalogRaw)
	if err != nil {
		return err
	}
	canonicalCatalog, err := json.Marshal(platformCatalog)
	if err != nil {
		return err
	}
	config, err := kernelspi.NewMapConfig(map[string]any{
		"platform.portal-composer.stateFile":       *stateFile,
		"platform.portal-composer.platformCatalog": string(canonicalCatalog),
		"platform.interaction-broker.stateFile":    *brokerStateFile,
	})
	if err != nil {
		return err
	}
	host, err := hostfactory.NewWithDependencies(version, logf, kernelspi.Dependencies{Config: config})
	if err != nil {
		return err
	}
	if err := host.RegisterHostService(extpoint.KernelService, portalapi.KernelCatalogValidationCapability, edge.CatalogValidationService(catalog)); err != nil {
		return err
	}
	if err := host.RegisterHostService(extpoint.KernelService, portalapi.KernelCatalogMaterializationCapability, edge.CatalogMaterializationService(catalog)); err != nil {
		return err
	}
	if err := host.Start(); err != nil {
		return err
	}
	defer host.Stop()
	policyRef := pluginv1.ArtifactRef{PluginID: *policyID, Version: *policyVersion, Channel: *policyChannel}
	policyEnvelope, err := repository.Fetch(ctx, policyRef)
	if err != nil {
		return fmt.Errorf("读取门户访问策略制品: %w", err)
	}
	verifiedPolicy, err := verifier.Verify(policyRef, policyEnvelope)
	if err != nil {
		return fmt.Errorf("验证门户访问策略制品: %w", err)
	}
	installedPolicy, err := (nodeagent.LocalInstaller{Root: *installRoot}).Install(verifiedPolicy)
	if err != nil {
		return fmt.Errorf("安装门户访问策略制品: %w", err)
	}
	interactionPolicyRef := pluginv1.ArtifactRef{PluginID: *interactionPolicyID, Version: *interactionPolicyVersion, Channel: *interactionPolicyChannel}
	interactionPolicyEnvelope, err := repository.Fetch(ctx, interactionPolicyRef)
	if err != nil {
		return fmt.Errorf("读取交互访问策略制品: %w", err)
	}
	verifiedInteractionPolicy, err := verifier.Verify(interactionPolicyRef, interactionPolicyEnvelope)
	if err != nil {
		return fmt.Errorf("验证交互访问策略制品: %w", err)
	}
	installedInteractionPolicy, err := (nodeagent.LocalInstaller{Root: *installRoot}).Install(verifiedInteractionPolicy)
	if err != nil {
		return fmt.Errorf("安装交互访问策略制品: %w", err)
	}
	brokerRef := pluginv1.ArtifactRef{PluginID: *brokerID, Version: *brokerVersion, Channel: *brokerChannel}
	brokerEnvelope, err := repository.Fetch(ctx, brokerRef)
	if err != nil {
		return fmt.Errorf("读取 Interaction Broker 制品: %w", err)
	}
	verifiedBroker, err := verifier.Verify(brokerRef, brokerEnvelope)
	if err != nil {
		return fmt.Errorf("验证 Interaction Broker 制品: %w", err)
	}
	installedBroker, err := (nodeagent.LocalInstaller{Root: *installRoot}).Install(verifiedBroker)
	if err != nil {
		return fmt.Errorf("安装 Interaction Broker 制品: %w", err)
	}
	if _, err := host.LaunchWithPolicy(ctx, installedPolicy.EntryPath, launchPolicy(installedPolicy)); err != nil {
		return fmt.Errorf("启动门户访问策略: %w", err)
	}
	if _, err := host.LaunchWithPolicy(ctx, installedInteractionPolicy.EntryPath, launchPolicy(installedInteractionPolicy)); err != nil {
		return fmt.Errorf("启动交互访问策略: %w", err)
	}
	if _, err := host.LaunchWithPolicy(ctx, installed.EntryPath, launchPolicy(installed)); err != nil {
		return fmt.Errorf("启动 Composer: %w", err)
	}
	if _, err := host.LaunchWithPolicy(ctx, installedBroker.EntryPath, launchPolicy(installedBroker)); err != nil {
		return fmt.Errorf("启动 Interaction Broker: %w", err)
	}
	client, err := edge.NewProtocolBusCapabilityClient(host)
	if err != nil {
		return err
	}
	service, err := edge.NewCapabilityService(client)
	if err != nil {
		return err
	}
	tenantIDs := make([]string, 0, len(platformCatalog.Bindings))
	for _, binding := range platformCatalog.Bindings {
		tenantIDs = append(tenantIDs, binding.TenantID)
	}
	go edge.RunFrontendPrefetcher(ctx, service, catalog, tenantIDs, *prefetchInterval, logf)
	interactionClient, err := edge.NewProtocolBusInteractionClient(host)
	if err != nil {
		return err
	}
	interactionService, err := edge.NewCapabilityInteractionService(interactionClient)
	if err != nil {
		return err
	}
	cluster, err := newPlatformRouter(ctx, platformOptions, logf)
	if err != nil {
		return err
	}
	if cluster != nil {
		defer cluster.Close()
	}
	var platformService *edge.CapabilityPlatformAdminService
	if cluster != nil {
		platformCaller, err := edge.NewAddressingPlatformCapabilityClient(cluster.router)
		if err != nil {
			return err
		}
		platformService, err = edge.NewCapabilityPlatformAdminService(platformCaller)
		if err != nil {
			return err
		}
	}
	server := &http.Server{Addr: *listen, Handler: edge.NewPlatformPortal(identity, service, interactionService, platformService, catalog, portalAssets), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	go func() { <-ctx.Done(); _ = server.Shutdown(context.Background()) }()
	err = server.ListenAndServeTLS(*cert, *key)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func launchPolicy(installed nodeagent.InstalledPlugin) protocolbus.LaunchPolicy {
	return protocolbus.LaunchPolicy{
		PluginID: installed.ID, Publisher: installed.Publisher, Version: installed.Version,
		Contributions: installed.Contract.Contributions, KernelServices: installed.Contract.KernelServices,
		ContextAccess: installed.Contract.ContextAccess,
	}
}
