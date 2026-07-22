// Package configurationcatalog persists trusted configuration catalogs next
// to their active Deployment facts. Catalogs are sidecar snapshots: readers
// accept one only when its revision and digest still match the active
// Deployment, so publication races fail closed instead of serving stale forms.
package configurationcatalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/nats-io/nats.go/jetstream"

	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	sharedcontrolplane "cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

const keySuffix = ".configuration_catalog.v1"

type Store struct{ KV jetstream.KeyValue }

func Key(tenant, deployment string) string {
	return sharedcontrolplane.DeploymentKey(tenant, deployment) + keySuffix
}

func (s Store) Publish(ctx context.Context, tenant string, catalog pluginconfiguration.Catalog) error {
	if s.KV == nil || strings.TrimSpace(tenant) == "" {
		return errors.New("配置目录控制面未配置或 tenant 为空")
	}
	if err := catalog.Validate(); err != nil {
		return err
	}
	if err := s.matchesActiveDeployment(ctx, tenant, catalog); err != nil {
		return err
	}
	raw, err := json.Marshal(catalog)
	if err != nil {
		return fmt.Errorf("编码配置目录: %w", err)
	}
	key := Key(tenant, catalog.Deployment)
	current, err := s.KV.Get(ctx, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		if _, err := s.KV.Create(ctx, key, raw); err != nil {
			return fmt.Errorf("创建配置目录 %s: %w", catalog.Deployment, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取配置目录 %s: %w", catalog.Deployment, err)
	}
	existing, err := parseCatalog(current.Value())
	if err != nil {
		return fmt.Errorf("既有配置目录损坏，拒绝覆盖: %w", err)
	}
	if existing.DeploymentRevision == catalog.DeploymentRevision {
		if existing.Digest != catalog.Digest {
			return fmt.Errorf("配置目录业务 revision %d 已存在且内容不同", catalog.DeploymentRevision)
		}
		return nil
	}
	if existing.DeploymentRevision > catalog.DeploymentRevision {
		return fmt.Errorf("配置目录 revision 必须单调递增: current=%d requested=%d", existing.DeploymentRevision, catalog.DeploymentRevision)
	}
	if _, err := s.KV.Update(ctx, key, raw, current.Revision()); err != nil {
		return fmt.Errorf("CAS 更新配置目录 %s: %w", catalog.Deployment, err)
	}
	return nil
}

func (s Store) List(ctx context.Context, tenant string) ([]pluginconfiguration.Catalog, error) {
	if s.KV == nil || strings.TrimSpace(tenant) == "" {
		return nil, errors.New("配置目录控制面未配置或 tenant 为空")
	}
	keys, err := s.KV.Keys(ctx)
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return []pluginconfiguration.Catalog{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("列出配置目录控制面 key: %w", err)
	}
	prefix := sharedcontrolplane.DeploymentPrefix(tenant)
	items := make([]pluginconfiguration.Catalog, 0)
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, keySuffix) {
			continue
		}
		entry, err := s.KV.Get(ctx, key)
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("读取配置目录控制面 key %s: %w", key, err)
		}
		catalog, err := parseCatalog(entry.Value())
		if err != nil {
			return nil, fmt.Errorf("配置目录控制面 key %s 损坏: %w", key, err)
		}
		if key != Key(tenant, catalog.Deployment) {
			return nil, fmt.Errorf("配置目录控制面 key 与内容身份不匹配: %s", key)
		}
		if err := s.matchesActiveDeployment(ctx, tenant, catalog); err != nil {
			// Deployment 与目录分两次 CAS 更新。短暂窗口内不返回旧表单，
			// 但也不让一个部署的更新阻断其他部署的配置管理。
			continue
		}
		items = append(items, catalog)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Deployment < items[j].Deployment })
	return items, nil
}

func (s Store) matchesActiveDeployment(ctx context.Context, tenant string, catalog pluginconfiguration.Catalog) error {
	entry, err := s.KV.Get(ctx, sharedcontrolplane.DeploymentKey(tenant, catalog.Deployment))
	if err != nil {
		return fmt.Errorf("读取配置目录对应 Deployment: %w", err)
	}
	deployment, err := deploymentv2.Parse(entry.Value())
	if err != nil {
		return fmt.Errorf("解析配置目录对应 Deployment: %w", err)
	}
	if deployment.Metadata.Tenant != tenant || deployment.Metadata.Name != catalog.Deployment || deployment.Revision != catalog.DeploymentRevision || deployment.Digest() != catalog.DeploymentDigest {
		return errors.New("配置目录与活动 Deployment revision/digest 不匹配")
	}
	return nil
}

func parseCatalog(raw []byte) (pluginconfiguration.Catalog, error) {
	var catalog pluginconfiguration.Catalog
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		return catalog, err
	}
	if err := ensureEOF(decoder); err != nil {
		return catalog, err
	}
	return catalog, catalog.Validate()
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("配置目录只能包含一个 JSON 文档")
		}
		return err
	}
	return nil
}
