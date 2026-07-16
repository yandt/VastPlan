package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	deploymentv2 "cdsoft.com.cn/VastPlan/schemas/deployment/v2"
)

// ApplyDeployment 以 CAS 发布全局 v2 部署规格；同 revision 不允许被改写，显式回滚仍可用。
func ApplyDeployment(ctx context.Context, kv jetstream.KeyValue, key string, raw []byte) (uint64, deploymentv2.Deployment, error) {
	deployment, err := deploymentv2.Parse(raw)
	if err != nil {
		return 0, deploymentv2.Deployment{}, err
	}
	normalized, err := json.Marshal(deployment)
	if err != nil {
		return 0, deploymentv2.Deployment{}, fmt.Errorf("序列化集群部署: %w", err)
	}
	current, err := kv.Get(ctx, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		revision, createErr := kv.Create(ctx, key, normalized)
		if createErr != nil {
			return 0, deploymentv2.Deployment{}, fmt.Errorf("创建集群部署 key %s: %w", key, createErr)
		}
		return revision, deployment, nil
	}
	if err != nil {
		return 0, deploymentv2.Deployment{}, fmt.Errorf("读取既有集群部署 key %s: %w", key, err)
	}
	existing, err := deploymentv2.Parse(current.Value())
	if err != nil {
		return 0, deploymentv2.Deployment{}, fmt.Errorf("既有集群部署损坏，拒绝覆盖: %w", err)
	}
	if existing.Revision == deployment.Revision {
		if existing.Digest() != deployment.Digest() {
			return 0, deploymentv2.Deployment{}, fmt.Errorf("业务 revision %d 已存在且内容不同", deployment.Revision)
		}
		return current.Revision(), deployment, nil
	}
	revision, err := kv.Update(ctx, key, normalized, current.Revision())
	if err != nil {
		return 0, deploymentv2.Deployment{}, fmt.Errorf("CAS 更新集群部署 key %s: %w", key, err)
	}
	return revision, deployment, nil
}
