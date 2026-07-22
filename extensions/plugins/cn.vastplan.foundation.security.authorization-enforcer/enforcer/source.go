// Package enforcer evaluates signed Authorization IR close to each kernel
// service and never treats an unavailable policy as an allow decision.
package enforcer

import (
	"os"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/authorizationtrust"
)

type PolicyBundle struct {
	Snapshot     authorizationv1.SignedPolicySnapshot
	Catalog      pluginv1.PermissionCatalog
	PolicyDigest string
}

type PolicySource interface{ Load() (PolicyBundle, error) }

type FilePolicySource struct {
	SnapshotPath string
	TrustPath    string
	CatalogPath  string
}

func (s FilePolicySource) Load() (PolicyBundle, error) {
	snapshot, err := (authorizationtrust.FileSnapshotStore{SnapshotPath: s.SnapshotPath, TrustPath: s.TrustPath}).Load()
	if err != nil {
		return PolicyBundle{}, err
	}
	raw, err := os.ReadFile(s.CatalogPath)
	if err != nil {
		return PolicyBundle{}, err
	}
	catalog, err := pluginv1.ParsePermissionCatalog(raw)
	if err != nil {
		return PolicyBundle{}, err
	}
	digest, err := authorizationv1.AuthorizationIRDigest(snapshot.Payload.Policy)
	if err != nil {
		return PolicyBundle{}, err
	}
	return PolicyBundle{Snapshot: snapshot, Catalog: catalog, PolicyDigest: digest}, nil
}

type GroupDirectory interface {
	Groups(subjectID string) ([]authorizationv1.ExternalGroup, uint64, error)
}

type EmptyGroupDirectory struct{}

func (EmptyGroupDirectory) Groups(string) ([]authorizationv1.ExternalGroup, uint64, error) {
	return []authorizationv1.ExternalGroup{}, 0, nil
}
