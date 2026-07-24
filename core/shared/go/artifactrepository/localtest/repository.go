// Package localtest implements artifact.repository.local-test.v1 over a
// private Unix Domain Socket. It is a development repository protocol, not a
// local deployment of the production remote protocol.
package localtest

import (
	"context"
	"errors"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

const (
	ProtocolHeader  = "X-VastPlan-Repository-Protocol"
	MaxPackageBytes = int64(256 << 20)
	minTokenBytes   = 32
)

// Repository is the protocol-facing port. Storage, signing and workspace
// lifecycle stay behind this boundary and can be supplied by the repository
// plugin without becoming transport concerns.
type Repository interface {
	ReadExact(context.Context, pluginv1.ArtifactRef) (artifacttrust.Envelope, error)
	Publish(context.Context, artifacttrust.Envelope) (artifactrepositoryv1.Receipt, error)
	CatalogSnapshot(context.Context) (artifactrepositoryv1.CatalogSnapshot, error)
}

type WorkspaceRepository interface {
	Repository
	ExpireWorkspace(context.Context) (artifactrepositoryv1.ExpireWorkspaceResult, error)
}

func validateBinding(profile artifactrepositoryv1.Profile, token string) (artifactrepositoryv1.Profile, error) {
	profile, err := artifactrepositoryv1.ValidateProfile(profile)
	if err != nil {
		return artifactrepositoryv1.Profile{}, err
	}
	if profile.Protocol != artifactrepositoryv1.ProtocolLocalTest {
		return artifactrepositoryv1.Profile{}, errors.New("local-test Adapter 只接受 local-test.v1 Profile")
	}
	if len(token) < minTokenBytes {
		return artifactrepositoryv1.Profile{}, errors.New("local-test 访问令牌至少需要 32 字节")
	}
	return profile, nil
}
