package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	deploymentv1 "cdsoft.com.cn/VastPlan/schemas/deployment/v1"
)

// ApplyDesiredState 以 KV CAS 发布期望态。它允许显式回滚到较小业务 revision，
// 但拒绝同业务 revision 的不同内容，也拒绝两个控制面写者静默覆盖彼此。
func ApplyDesiredState(ctx context.Context, kv jetstream.KeyValue, key string, raw []byte) (uint64, deploymentv1.DesiredState, error) {
	state, err := deploymentv1.Parse(raw)
	if err != nil {
		return 0, deploymentv1.DesiredState{}, err
	}
	normalized, err := json.Marshal(state)
	if err != nil {
		return 0, deploymentv1.DesiredState{}, fmt.Errorf("序列化期望态: %w", err)
	}
	current, err := kv.Get(ctx, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		revision, createErr := kv.Create(ctx, key, normalized)
		if createErr != nil {
			return 0, deploymentv1.DesiredState{}, fmt.Errorf("创建期望态 key %s: %w", key, createErr)
		}
		return revision, state, nil
	}
	if err != nil {
		return 0, deploymentv1.DesiredState{}, fmt.Errorf("读取既有期望态 key %s: %w", key, err)
	}
	existing, err := deploymentv1.Parse(current.Value())
	if err != nil {
		return 0, deploymentv1.DesiredState{}, fmt.Errorf("既有期望态损坏，拒绝覆盖: %w", err)
	}
	if existing.Revision == state.Revision {
		if existing.Digest() != state.Digest() {
			return 0, deploymentv1.DesiredState{}, fmt.Errorf("业务 revision %d 已存在且内容不同", state.Revision)
		}
		return current.Revision(), state, nil
	}
	revision, err := kv.Update(ctx, key, normalized, current.Revision())
	if err != nil {
		return 0, deploymentv1.DesiredState{}, fmt.Errorf("CAS 更新期望态 key %s: %w", key, err)
	}
	return revision, state, nil
}
