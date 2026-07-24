package main

import (
	"context"
	"errors"
	"fmt"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/portaltrust"
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
	sources := make([]portaltrust.ArtifactSource, 0, len(artifacts.sources))
	catalogOptions := []portaltrust.TrustedCatalogOption{portaltrust.WithFrontendDeliveryRoot(options.frontendDeliveryOrigin)}
	for _, source := range artifacts.sources {
		sources = append(sources, source)
		if local, ok := source.(protocolArtifactSource); ok {
			catalogOptions = append(catalogOptions, portaltrust.WithTestArtifactIndex(portaltrust.LocalTestArtifactIndex{Adapter: local.adapter}))
		}
	}
	if len(sources) == 0 {
		return errors.New("Portal Catalog 没有可用制品源")
	}
	catalog, err := portaltrust.NewTrustedCatalog(sources, portalArtifactVerifierAdapter{verifier: artifacts.verifier}, catalogOptions...)
	if err != nil {
		return fmt.Errorf("创建 Node Agent Portal Catalog: %w", err)
	}
	var publisher portaltrust.ArtifactReferencePublisher
	if plane.router != nil {
		publisher, err = portaltrust.NewAddressingArtifactReferencePublisher(plane.router)
	} else if options.allowDevelopmentPlugins {
		logf("警告：Portal Composer 未接入集群仓库，制品引用仅做开发态契约校验")
		publisher = portaltrust.DevelopmentArtifactReferencePublisher{}
	} else {
		return errors.New("生产 Portal Composer 必须接入 Addressing 制品引用发布器")
	}
	if err != nil {
		return err
	}
	runtime.HostServices = map[string]protocolbus.HostService{
		portalapi.KernelCatalogValidationCapability:            portaltrust.CatalogValidationService(catalog),
		portalapi.KernelCatalogMaterializationCapability:       portaltrust.CatalogMaterializationService(catalog),
		portalapi.KernelArtifactReferencePublicationCapability: portaltrust.ArtifactReferencePublicationService(publisher),
		portalapi.KernelTestArtifactValidationCapability:       portaltrust.CatalogTestArtifactValidationService(catalog),
	}
	return nil
}
