package repositoryruntime

import (
	"context"
	"errors"
	"fmt"
	"time"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/workspacelease"
)

// LocalTestAdapter exposes the managed repository's existing trust, catalog
// and immutable storage through artifact.repository.local-test.v1. It does not
// create another repository or copy artifacts into a development side store.
type LocalTestAdapter struct {
	profile artifactrepositoryv1.Profile
	manager *Manager
	leases  *workspacelease.Store
}

func NewLocalTestAdapter(profile artifactrepositoryv1.Profile, manager *Manager) (*LocalTestAdapter, error) {
	profile, err := artifactrepositoryv1.ValidateProfile(profile)
	if err != nil {
		return nil, err
	}
	if profile.Protocol != artifactrepositoryv1.ProtocolLocalTest || manager == nil {
		return nil, errors.New("local-test 仓库 Adapter 配置无效")
	}
	var leases *workspacelease.Store
	if profile.Workspace != nil {
		root, err := manager.activeRepositoryRoot()
		if err != nil {
			return nil, err
		}
		leases, err = workspacelease.Open(root)
		if err != nil {
			return nil, err
		}
	}
	return &LocalTestAdapter{profile: profile, manager: manager, leases: leases}, nil
}

func (a *LocalTestAdapter) Profile() artifactrepositoryv1.Profile { return a.profile }

func (a *LocalTestAdapter) ReadExact(ctx context.Context, ref pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	if err := ctx.Err(); err != nil {
		return artifacttrust.Envelope{}, err
	}
	if err := artifactrepositoryv1.ValidateRef(a.profile, ref); err != nil {
		return artifacttrust.Envelope{}, err
	}
	if ref.Channel == "workspace" {
		if a.leases == nil {
			return artifacttrust.Envelope{}, errors.New("local-test Profile 未启用 workspace")
		}
		if _, active := a.leases.Active(ref, time.Now().UTC()); !active {
			return artifacttrust.Envelope{}, fmt.Errorf("%w: workspace lease 已过期或不存在", artifacttrust.ErrNotFound)
		}
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
	if ref.Channel == "workspace" {
		now := time.Now().UTC()
		if _, _, err := a.leases.Expire(now, a.manager.artifactProtected); err != nil {
			return artifactrepositoryv1.Receipt{}, err
		}
		if err := a.leases.CanGrant(ref, envelope.Artifact.SHA256, a.profile.Workspace.MaxArtifacts, now); err != nil {
			return artifactrepositoryv1.Receipt{}, err
		}
	}
	published, err := a.manager.PublishWithSupplyChain(envelope.Proof, envelope.PackageBytes, envelope.Provenance, envelope.ProvenanceVerification, envelope.SecurityAdmission)
	if err != nil {
		return artifactrepositoryv1.Receipt{}, err
	}
	entry, found := a.entry(ref)
	if !found || published.SHA256 != entry.SHA256 {
		return artifactrepositoryv1.Receipt{}, errors.New("local-test 发布完成后 Catalog 未形成精确回执")
	}
	if ref.Channel == "workspace" {
		lease, _, err := a.leases.Grant(ref, entry.SHA256, time.Duration(a.profile.Workspace.TTLSeconds)*time.Second, a.profile.Workspace.MaxArtifacts, time.Now().UTC())
		if err != nil {
			return artifactrepositoryv1.Receipt{}, err
		}
		return a.workspaceReceipt(entry, lease), nil
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
		if entry.Ref.Channel == "workspace" {
			lease, active := a.leases.Active(entry.Ref, time.Now().UTC())
			if !active || lease.SHA256 != entry.SHA256 {
				continue
			}
			items = append(items, a.workspaceReceipt(catalogEntry{Ref: entry.Ref, SHA256: entry.SHA256, RepositoryRevision: entry.RepositoryRevision}, lease))
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

func (a *LocalTestAdapter) ExpireWorkspace(ctx context.Context) (artifactrepositoryv1.ExpireWorkspaceResult, error) {
	if err := ctx.Err(); err != nil {
		return artifactrepositoryv1.ExpireWorkspaceResult{}, err
	}
	if a.leases == nil || a.profile.Workspace == nil {
		return artifactrepositoryv1.ExpireWorkspaceResult{}, errors.New("local-test Profile 未启用 workspace")
	}
	revision, expired, err := a.leases.Expire(time.Now().UTC(), a.manager.artifactProtected)
	if err != nil {
		return artifactrepositoryv1.ExpireWorkspaceResult{}, err
	}
	return artifactrepositoryv1.ExpireWorkspaceResult{SchemaVersion: artifactrepositoryv1.ProfileVersion, Revision: revision, Expired: expired}, nil
}

func (a *LocalTestAdapter) ValidateReceipt(receipt artifactrepositoryv1.Receipt, now time.Time) error {
	if err := artifactrepositoryv1.ValidateReceipt(a.profile, receipt); err != nil {
		return err
	}
	entry, found := a.entry(receipt.Ref)
	if !found || entry.SHA256 != receipt.SHA256 || entry.RepositoryRevision != receipt.Revision {
		return errors.New("仓库回执与当前不可变 Catalog 不一致")
	}
	if receipt.Ref.Channel == "workspace" {
		lease, active := a.leases.Active(receipt.Ref, now)
		if !active || lease.SHA256 != receipt.SHA256 || lease.Token != receipt.WorkspaceLease || receipt.ExpiresAt == nil || !lease.ExpiresAt.Equal(*receipt.ExpiresAt) {
			return errors.New("workspace 回执 lease 已过期或发生漂移")
		}
	}
	return nil
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

func (a *LocalTestAdapter) workspaceReceipt(entry catalogEntry, lease workspacelease.Lease) artifactrepositoryv1.Receipt {
	receipt := a.receipt(entry.Ref, entry.SHA256, entry.RepositoryRevision)
	receipt.WorkspaceLease, receipt.ExpiresAt = lease.Token, &lease.ExpiresAt
	return receipt
}
