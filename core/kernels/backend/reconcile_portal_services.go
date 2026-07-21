package main

import (
	"context"
	"errors"
	"fmt"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/edge"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

type portalArtifactVerifierAdapter struct{ verifier nodeagent.ArtifactVerifier }

func (a portalArtifactVerifierAdapter) Verify(_ context.Context, ref pluginv1.ArtifactRef, envelope artifacttrust.Envelope) (pluginv1.Artifact, error) {
	verified, err := a.verifier.Verify(ref, envelope)
	if err != nil {
		return pluginv1.Artifact{}, err
	}
	return verified.Artifact(), nil
}

func configurePortalHostServices(options reconcileOptions, artifacts artifactResolution, plane *nodeControlPlane, runtime *nodeagent.ProtocolRuntime, logf func(string, ...any)) error {
	if options.frontendDeliveryOrigin == "" {
		return nil
	}
	sources := make([]edge.ArtifactSource, 0, len(artifacts.sources))
	catalogOptions := []edge.TrustedCatalogOption{edge.WithFrontendDeliveryRoot(options.frontendDeliveryOrigin)}
	for _, source := range artifacts.sources {
		sources = append(sources, source)
		if remote, ok := source.(*pluginservice.RemoteRepository); ok {
			catalogOptions = append(catalogOptions, edge.WithTestArtifactIndex(edge.RemoteTestArtifactIndex{
				BaseURL: remote.BaseURL, Token: remote.Token, Client: remote.Client,
			}))
		}
	}
	if len(sources) == 0 {
		return errors.New("Portal Catalog 没有可用制品源")
	}
	catalog, err := edge.NewTrustedCatalog(sources, portalArtifactVerifierAdapter{verifier: artifacts.verifier}, catalogOptions...)
	if err != nil {
		return fmt.Errorf("创建 Node Agent Portal Catalog: %w", err)
	}
	var publisher edge.ArtifactReferencePublisher
	if plane.router != nil {
		publisher, err = edge.NewAddressingArtifactReferencePublisher(plane.router)
	} else if options.allowDevelopmentPlugins {
		logf("警告：Portal Composer 未接入集群仓库，制品引用仅做开发态契约校验")
		publisher = edge.DevelopmentArtifactReferencePublisher{}
	} else {
		return errors.New("生产 Portal Composer 必须接入 Addressing 制品引用发布器")
	}
	if err != nil {
		return err
	}
	runtime.HostServices = map[string]protocolbus.HostService{
		portalapi.KernelCatalogValidationCapability:            edge.CatalogValidationService(catalog),
		portalapi.KernelCatalogMaterializationCapability:       edge.CatalogMaterializationService(catalog),
		portalapi.KernelArtifactReferencePublicationCapability: edge.ArtifactReferencePublicationService(publisher),
		portalapi.KernelTestArtifactValidationCapability:       edge.CatalogTestArtifactValidationService(catalog),
	}
	return nil
}
