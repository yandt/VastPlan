package main

import (
	"context"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/bootstrapupgrade"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/core/shared/go/bootstrapinventory"
)

type bootstrapUpgradeAdapter struct{ controller *bootstrapupgrade.Controller }

func buildBootstrapUpgrade(options reconcileOptions, artifacts artifactResolution) (nodeagent.BootstrapUpgradeCoordinator, error) {
	if !options.bootstrapUpgrade {
		return nil, nil
	}
	controller, err := bootstrapupgrade.New(
		bootstrapupgrade.FileInventoryStore{Path: options.bootstrapInventory},
		artifacts.bootstrap,
	)
	if err != nil {
		return nil, err
	}
	return bootstrapUpgradeAdapter{controller: controller}, nil
}

func (a bootstrapUpgradeAdapter) Begin(installed []bootstrapinventory.Item) (bootstrapinventory.Inventory, error) {
	return a.controller.Begin(installed)
}

func (a bootstrapUpgradeAdapter) Prepare(ctx context.Context, verified []nodeagent.VerifiedArtifact) (bootstrapinventory.Inventory, error) {
	candidates := make([]bootstrapupgrade.Candidate, len(verified))
	for index, artifact := range verified {
		candidates[index] = bootstrapupgrade.Candidate{
			Artifact: artifact.Artifact(), PackageBytes: artifact.PackageBytes(), Proof: artifact.ProofBytes(),
			Provenance: artifact.ProvenanceBytes(), ProvenanceVerification: artifact.ProvenanceVerificationBytes(),
		}
	}
	return a.controller.Prepare(ctx, candidates)
}

func (a bootstrapUpgradeAdapter) Commit(ctx context.Context) (bootstrapinventory.Inventory, error) {
	return a.controller.Commit(ctx)
}
