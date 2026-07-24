package repositoryruntime

import (
	"context"
	"errors"
	"fmt"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

// LocalTestAdapter exposes the managed repository's existing trust, catalog
// and immutable storage through artifact.repository.local-test.v1. It does not
// create another repository or copy artifacts into a development side store.
type LocalTestAdapter struct {
	profile artifactrepositoryv1.Profile
	manager *Manager
}

func NewLocalTestAdapter(profile artifactrepositoryv1.Profile, manager *Manager) (*LocalTestAdapter, error) {
	profile, err := artifactrepositoryv1.ValidateProfile(profile)
	if err != nil {
		return nil, err
	}
	if profile.Protocol != artifactrepositoryv1.ProtocolLocalTest || manager == nil {
		return nil, errors.New("local-test 仓库 Adapter 配置无效")
	}
	if profile.Workspace != nil {
		return nil, errors.New("workspace 生命周期将在 ADR-0145 P3 接入，当前 Adapter 只允许 testing")
	}
	return &LocalTestAdapter{profile: profile, manager: manager}, nil
}

func (a *LocalTestAdapter) ReadExact(ctx context.Context, ref pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	if err := ctx.Err(); err != nil {
		return artifacttrust.Envelope{}, err
	}
	if err := artifactrepositoryv1.ValidateRef(a.profile, ref); err != nil {
		return artifacttrust.Envelope{}, err
	}
	if _, found := a.entry(ref); !found {
		return artifacttrust.Envelope{}, fmt.Errorf("%w: %s@%s/%s", artifacttrust.ErrNotFound, ref.PluginID, ref.Version, ref.Channel)
	}
	artifact, packageBytes, proof, provenance, verification, admission, err := a.manager.ReadWithSupplyChain(ref)
	if err != nil {
		return artifacttrust.Envelope{}, err
	}
	status, err := a.manager.ReadSecurityStatusChain(ref)
	if err != nil {
		return artifacttrust.Envelope{}, err
	}
	return artifacttrust.Envelope{
		Artifact: artifact, PackageBytes: packageBytes, Proof: proof,
		Provenance: provenance, ProvenanceVerification: verification,
		SecurityAdmission: admission, SecurityStatusChain: status,
	}, nil
}

func (a *LocalTestAdapter) Publish(ctx context.Context, envelope artifacttrust.Envelope) (artifactrepositoryv1.Receipt, error) {
	if err := ctx.Err(); err != nil {
		return artifactrepositoryv1.Receipt{}, err
	}
	ref := pluginv1.ArtifactRef{PluginID: envelope.Artifact.PluginID, Version: envelope.Artifact.Version, Channel: envelope.Artifact.Channel}
	if err := artifactrepositoryv1.ValidateRef(a.profile, ref); err != nil {
		return artifactrepositoryv1.Receipt{}, err
	}
	if len(envelope.SecurityStatusChain) != 0 {
		return artifactrepositoryv1.Receipt{}, errors.New("local-test 发布不得写入追加式 security status chain")
	}
	published, err := a.manager.PublishWithSupplyChain(envelope.Proof, envelope.PackageBytes, envelope.Provenance, envelope.ProvenanceVerification, envelope.SecurityAdmission)
	if err != nil {
		return artifactrepositoryv1.Receipt{}, err
	}
	entry, found := a.entry(ref)
	if !found || published.SHA256 != entry.SHA256 {
		return artifactrepositoryv1.Receipt{}, errors.New("local-test 发布完成后 Catalog 未形成精确回执")
	}
	return a.receipt(entry.Ref, entry.SHA256, entry.RepositoryRevision), nil
}

func (a *LocalTestAdapter) CatalogSnapshot(ctx context.Context) (artifactrepositoryv1.CatalogSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return artifactrepositoryv1.CatalogSnapshot{}, err
	}
	a.manager.mu.RLock()
	revision, entries := a.manager.active.catalog.Entries()
	a.manager.mu.RUnlock()
	items := make([]artifactrepositoryv1.Receipt, 0, len(entries))
	for _, entry := range entries {
		if artifactrepositoryv1.ValidateRef(a.profile, entry.Ref) != nil {
			continue
		}
		items = append(items, a.receipt(entry.Ref, entry.SHA256, entry.RepositoryRevision))
	}
	return artifactrepositoryv1.CatalogSnapshot{
		SchemaVersion: artifactrepositoryv1.ProfileVersion,
		RepositoryID:  a.profile.ID, Protocol: a.profile.Protocol, ProfileDigest: a.profile.Digest(),
		Revision: revision, Items: items,
	}, nil
}

func (a *LocalTestAdapter) entry(ref pluginv1.ArtifactRef) (entry catalogEntry, found bool) {
	a.manager.mu.RLock()
	defer a.manager.mu.RUnlock()
	value, found := a.manager.active.catalog.Lookup(ref)
	if !found {
		return catalogEntry{}, false
	}
	return catalogEntry{Ref: value.Ref, SHA256: value.SHA256, RepositoryRevision: value.RepositoryRevision}, true
}

type catalogEntry struct {
	Ref                pluginv1.ArtifactRef
	SHA256             string
	RepositoryRevision uint64
}

func (a *LocalTestAdapter) receipt(ref pluginv1.ArtifactRef, digest string, revision uint64) artifactrepositoryv1.Receipt {
	return artifactrepositoryv1.Receipt{
		SchemaVersion: artifactrepositoryv1.ProfileVersion,
		RepositoryID:  a.profile.ID, Protocol: a.profile.Protocol, ProfileDigest: a.profile.Digest(),
		Ref: ref, SHA256: digest, Revision: revision,
	}
}
