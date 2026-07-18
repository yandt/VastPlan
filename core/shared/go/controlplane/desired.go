package controlplane

import (
	"context"

	"github.com/nats-io/nats.go/jetstream"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
)

// ApplyDesiredState 以 KV CAS 发布期望态。它允许显式回滚到较小业务 revision，
// 但拒绝同业务 revision 的不同内容，也拒绝两个控制面写者静默覆盖彼此。
func ApplyDesiredState(ctx context.Context, kv jetstream.KeyValue, key string, raw []byte) (uint64, deploymentv1.DesiredState, error) {
	return applyVersioned(ctx, kv, key, raw, versionedCodec[deploymentv1.DesiredState]{
		parse: deploymentv1.Parse, noun: "期望态",
		revision: func(value deploymentv1.DesiredState) uint64 { return value.Revision },
		digest:   func(value deploymentv1.DesiredState) string { return value.Digest() },
	})
}
