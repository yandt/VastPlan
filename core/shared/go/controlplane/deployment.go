package controlplane

import (
	"context"

	"github.com/nats-io/nats.go/jetstream"

	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
)

// ApplyDeployment 以 CAS 发布 Resolver 生成的全局 v2 部署规格；revision 必须单调递增，
// 回滚通过用新的 revision 重新解析并发布旧组合内容完成。
func ApplyDeployment(ctx context.Context, kv jetstream.KeyValue, key string, raw []byte) (uint64, deploymentv2.Deployment, error) {
	return applyVersioned(ctx, kv, key, raw, versionedCodec[deploymentv2.Deployment]{
		parse: deploymentv2.Parse, noun: "集群部署",
		revision:  func(value deploymentv2.Deployment) uint64 { return value.Revision },
		digest:    func(value deploymentv2.Deployment) string { return value.Digest() },
		monotonic: true,
	})
}
