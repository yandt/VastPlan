// Package portaledgecommand assembles the production Portal BFF from verified
// artifacts, a trusted session boundary, and the Backend protocol host.
package portaledgecommand

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"time"

	"cdsoft.com.cn/VastPlan/kernels/backend/edge"
	"cdsoft.com.cn/VastPlan/kernels/backend/hostfactory"
	"cdsoft.com.cn/VastPlan/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/kernels/backend/pluginservice"
	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/shared/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/shared/go/protocolbus"
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
	installRoot := flags.String("install-root", ".vastplan/portal-edge/plugins", "content-addressed plugin install root")
	trustFile := flags.String("trust-store", "", "artifact publisher trust document")
	allowUnsigned := flags.Bool("allow-unsigned-local", false, "local development only: permit unsigned artifacts")
	pluginID := flags.String("composer-plugin", "com.vastplan.platform.configuration.portal-composer", "Composer plugin ID")
	pluginVersion := flags.String("composer-version", "", "Composer artifact version")
	channel := flags.String("composer-channel", "stable", "Composer artifact channel")
	stateFile := flags.String("composer-state-file", "", "Composer governed-state file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *cert == "" || *key == "" || *sessions == "" || *pluginVersion == "" || *stateFile == "" {
		return errors.New("portal-edge 必须配置 TLS、session、Composer 制品版本及状态文件")
	}
	if *allowUnsigned && *trustFile != "" {
		return errors.New("allow-unsigned-local 与 trust-store 不能同时使用")
	}
	var verifier nodeagent.ArtifactVerifier
	if *allowUnsigned {
		verifier = nodeagent.NewLocalDevelopmentArtifactVerifier()
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
	catalog, err := edge.NewTrustedCatalog([]edge.ArtifactSource{repository}, verifierAdapter{verifier})
	if err != nil {
		return err
	}
	config, err := kernelspi.NewMapConfig(map[string]any{"platform.portal-composer.stateFile": *stateFile})
	if err != nil {
		return err
	}
	host, err := hostfactory.NewWithDependencies(version, logf, kernelspi.Dependencies{Config: config})
	if err != nil {
		return err
	}
	if err := host.RegisterHostService(extpoint.KernelService, "kernel.portal.catalog.validate", edge.CatalogValidationService(catalog)); err != nil {
		return err
	}
	if err := host.Start(); err != nil {
		return err
	}
	defer host.Stop()
	if _, err := host.LaunchWithPolicy(ctx, installed.EntryPath, protocolbus.LaunchPolicy{PluginID: installed.ID, Version: installed.Version, Contributions: installed.Contract.Contributions, KernelServices: installed.Contract.KernelServices}); err != nil {
		return fmt.Errorf("启动 Composer: %w", err)
	}
	client, err := edge.NewProtocolBusCapabilityClient(host)
	if err != nil {
		return err
	}
	service, err := edge.NewCapabilityService(client)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: *listen, Handler: edge.New(identity, service), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	go func() { <-ctx.Done(); _ = server.Shutdown(context.Background()) }()
	err = server.ListenAndServeTLS(*cert, *key)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
