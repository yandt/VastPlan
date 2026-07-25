package main

import (
	"context"
	"fmt"
	"time"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/compositionresolver"
	backendconfigurationauthority "cdsoft.com.cn/VastPlan/core/kernels/backend/configurationauthority"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/configurationcatalog"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/credentialbroker"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/deploymentpublisher"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodebootstrapbroker"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodebootstrapobserver"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/platformcatalog"
	kernelprofileactivation "cdsoft.com.cn/VastPlan/core/kernels/backend/profileactivation"
	"cdsoft.com.cn/VastPlan/core/shared/go/bootstrapinventory"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

type reconcilePreparation struct {
	labels    map[string]string
	artifacts artifactResolution
	inventory *bootstrapinventory.Inventory
	upgrade   nodeagent.BootstrapUpgradeCoordinator
}

func prepareReconcile(options reconcileOptions) (reconcilePreparation, error) {
	labels, err := parseLabels(options.labelsRaw)
	if err != nil {
		return reconcilePreparation{}, err
	}
	artifacts, err := buildArtifactResolution(options)
	if err != nil {
		return reconcilePreparation{}, err
	}
	var inventory *bootstrapinventory.Inventory
	if options.bootstrapInventory != "" {
		parsed, err := bootstrapinventory.ParseFile(options.bootstrapInventory)
		if err != nil {
			return reconcilePreparation{}, fmt.Errorf("读取 Bootstrap Inventory: %w", err)
		}
		if err := artifacts.VerifyBootstrapInventory(context.Background(), parsed); err != nil {
			return reconcilePreparation{}, err
		}
		inventory = &parsed
	}
	upgrade, err := buildBootstrapUpgrade(options, artifacts)
	if err != nil {
		return reconcilePreparation{}, err
	}
	return reconcilePreparation{labels: labels, artifacts: artifacts, inventory: inventory, upgrade: upgrade}, nil
}

func buildReconcileRuntime(options reconcileOptions, artifacts artifactResolution, plane *nodeControlPlane, logf func(string, ...any)) (*nodeagent.ProtocolRuntime, error) {
	runtime := nodeagent.NewProtocolRuntime(version, logf)
	runtime.ExecutionPolicy = options.executionPolicy
	runtime.ContextPolicy = options.contextPolicy
	runtime.GrantPolicy = options.grantPolicy
	runtime.PlacementPolicy = options.placementPolicy
	runtime.HostingPolicy = options.hostingPolicy
	runtime.Identity = options.nodeID
	runtime.LeaderKV = plane.buckets.Controllers
	if plane.buckets.SharedState != nil {
		stateStore, err := sharedstate.NewNATSStore(plane.buckets.SharedState)
		if err != nil {
			return nil, err
		}
		runtime.Dependencies.SharedState = stateStore
	}
	if err := configurePortalHostServices(options, artifacts, plane, runtime, logf); err != nil {
		return nil, err
	}
	if plane.transport != nil && plane.buckets.Nodes != nil {
		readiness, err := nodebootstrapobserver.New(plane.buckets.Nodes, plane.transport)
		if err != nil {
			return nil, err
		}
		runtime.Dependencies.NodeReadiness = readiness
	}
	if err := configureRuntimeCredentials(options, plane, runtime); err != nil {
		return nil, err
	}
	if err := configureDeploymentServices(options, artifacts, plane, runtime); err != nil {
		return nil, err
	}
	return runtime, nil
}

func configureRuntimeCredentials(options reconcileOptions, plane *nodeControlPlane, runtime *nodeagent.ProtocolRuntime) error {
	var named kernelspi.CredentialBroker
	if options.credentialRoot != "" {
		credentials, err := credentialbroker.NewDirectory(options.credentialRoot)
		if err != nil {
			return err
		}
		broker, err := nodebootstrapbroker.NewSSH(credentials, 30*time.Second)
		if err != nil {
			return err
		}
		named = credentials
		runtime.Dependencies.NodeBootstrap = broker
	}
	var managed kernelspi.CredentialBroker
	if plane.router != nil {
		audience := options.nodeID
		if plane.transport != nil && plane.transport.SelfIdentity().Name != "" {
			audience = plane.transport.SelfIdentity().Name
		}
		var err error
		managed, err = credentialbroker.NewManagedLease(audience, plane.router.Invoke)
		if err != nil {
			return err
		}
		runtime.Dependencies.RuntimeMaterialLeases, err = credentialbroker.NewRuntimeLease(plane.router.Invoke)
		if err != nil {
			return err
		}
	}
	if managed == nil && named == nil {
		return nil
	}
	credentials, err := credentialbroker.NewComposite(managed, named)
	if err != nil {
		return err
	}
	runtime.Dependencies.Credentials = credentials
	return nil
}

func configureDeploymentServices(options reconcileOptions, artifacts artifactResolution, plane *nodeControlPlane, runtime *nodeagent.ProtocolRuntime) error {
	if options.backendPlatformCatalog == "" {
		return nil
	}
	catalog, err := backendcompositionv1.ParseBackendPlatformCatalogFile(options.backendPlatformCatalog)
	if err != nil {
		return err
	}
	catalogSource, err := platformcatalog.NewWritableStore(plane.buckets.BackendPlatformCatalogs, plane.catalogPublisherKV, catalog)
	if err != nil {
		return err
	}
	catalogStore := configurationcatalog.Store{KV: plane.buckets.Deployments}
	publisher, err := deploymentpublisher.NewWithCatalogSource(catalogSource, artifacts, deploymentpublisher.KVApplier{KV: plane.buckets.Deployments}, catalogStore, compositionresolver.Options{AllowDevelopmentPlugins: options.allowDevelopmentPlugins}, compositionresolver.Resolve)
	if err != nil {
		return err
	}
	profileActivation, err := kernelprofileactivation.New(catalogSource, catalogStore, publisher)
	if err != nil {
		return err
	}
	runtime.Dependencies.DeploymentPublication = publisher
	runtime.Dependencies.PlatformProfileActivation = profileActivation
	runtime.Dependencies.DeploymentReadiness = natsDeploymentReadiness{KV: plane.buckets.Compositions}
	runtime.Dependencies.ConfigurationCatalogs = catalogStore
	authority := backendconfigurationauthority.Store{KV: plane.buckets.ConfigurationAuthorities, Catalogs: catalogStore}
	runtime.Dependencies.ConfigurationAuthorityIssuer = authority
	runtime.Dependencies.ConfigurationAuthorityConsumer = authority
	return nil
}

func buildNodeReconciler(options reconcileOptions, prepared reconcilePreparation, plane *nodeControlPlane, runtime *nodeagent.ProtocolRuntime) (*nodeagent.Reconciler, error) {
	reconciler := &nodeagent.Reconciler{
		NodeID: options.nodeID, NodeLabels: prepared.labels, Sources: prepared.artifacts.sources, Verifier: prepared.artifacts.verifier,
		Installer: nodeagent.LocalInstaller{Root: options.runtimeRoot}, Runtime: runtime,
		StateStore: plane.stateStore, RequireArtifactReferences: options.repositoryURL != "" || options.repositoryProfile != "",
		BootstrapInventory: prepared.inventory, BootstrapUpgrade: prepared.upgrade,
	}
	if plane.router == nil {
		return reconciler, nil
	}
	var err error
	reconciler.References, err = nodeagent.NewAddressingArtifactReferencePublisher(plane.router, options.nodeID)
	if err != nil {
		return nil, err
	}
	if prepared.inventory != nil && options.publishBootstrapReferences {
		reconciler.BootstrapReferences, err = nodeagent.NewBootstrapArtifactReferencePublisher(plane.router, prepared.inventory.RepositoryID)
		if err != nil {
			return nil, err
		}
	}
	return reconciler, nil
}
