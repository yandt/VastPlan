// Package artifactrepository selects exact repository protocol adapters.
package artifactrepository

import (
	"context"
	"errors"
	"fmt"
	"sync"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

type Adapter interface {
	Profile() artifactrepositoryv1.Profile
	ReadExact(context.Context, pluginv1.ArtifactRef) (artifacttrust.Envelope, error)
	Publish(context.Context, artifacttrust.Envelope) (artifactrepositoryv1.Receipt, error)
	CatalogSnapshot(context.Context) (artifactrepositoryv1.CatalogSnapshot, error)
}

// WorkspaceAdapter is intentionally optional: remote.v1 must not grow a
// no-op workspace method merely to satisfy the shared repository contract.
type WorkspaceAdapter interface {
	Adapter
	ExpireWorkspace(context.Context) (artifactrepositoryv1.ExpireWorkspaceResult, error)
}

type Factory func(artifactrepositoryv1.Profile) (Adapter, error)

type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

func NewRegistry() *Registry {
	return &Registry{factories: map[string]Factory{}}
}

func (r *Registry) Register(protocol string, factory Factory) error {
	if r == nil || factory == nil || len(artifactrepositoryv1.ProtocolOperations(protocol)) == 0 {
		return errors.New("仓库 Adapter protocol 或 factory 无效")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[protocol]; exists {
		return fmt.Errorf("仓库协议 %s 已注册", protocol)
	}
	r.factories[protocol] = factory
	return nil
}

func (r *Registry) Open(profile artifactrepositoryv1.Profile) (Adapter, error) {
	profile, err := artifactrepositoryv1.ValidateProfile(profile)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, errors.New("仓库 Adapter Registry 为空")
	}
	r.mu.RLock()
	factory := r.factories[profile.Protocol]
	r.mu.RUnlock()
	if factory == nil {
		return nil, fmt.Errorf("仓库协议 %s 没有精确 Adapter", profile.Protocol)
	}
	adapter, err := factory(profile)
	if err != nil {
		return nil, err
	}
	if adapter == nil || adapter.Profile().Protocol != profile.Protocol || adapter.Profile().Digest() != profile.Digest() {
		return nil, errors.New("仓库 Adapter 返回的 Profile 身份不匹配")
	}
	return adapter, nil
}
