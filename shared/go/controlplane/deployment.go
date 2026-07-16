package controlplane

import (
	"context"

	"github.com/nats-io/nats.go/jetstream"

	deploymentv2 "cdsoft.com.cn/VastPlan/schemas/deployment/v2"
)

// ApplyDeployment 以 CAS 发布全局 v2 部署规格；同 revision 不允许被改写，显式回滚仍可用。
func ApplyDeployment(ctx context.Context, kv jetstream.KeyValue, key string, raw []byte) (uint64, deploymentv2.Deployment, error) {
	return applyVersioned(ctx, kv, key, raw, versionedCodec[deploymentv2.Deployment]{
		parse: deploymentv2.Parse, noun: "集群部署",
		revision: func(value deploymentv2.Deployment) uint64 { return value.Revision },
		digest:   func(value deploymentv2.Deployment) string { return value.Digest() },
	})
}
