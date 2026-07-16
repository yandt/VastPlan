package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

type versionedCodec[T any] struct {
	parse    func([]byte) (T, error)
	revision func(T) uint64
	digest   func(T) string
	noun     string
}

// applyVersioned 集中实现控制面配置的发布不变量：同业务 revision 不可改写，
// 不同 revision（包括显式回滚）通过 KV revision CAS 更新。
func applyVersioned[T any](ctx context.Context, kv jetstream.KeyValue, key string, raw []byte, codec versionedCodec[T]) (uint64, T, error) {
	var zero T
	value, err := codec.parse(raw)
	if err != nil {
		return 0, zero, err
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return 0, zero, fmt.Errorf("序列化%s: %w", codec.noun, err)
	}
	current, err := kv.Get(ctx, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		revision, createErr := kv.Create(ctx, key, normalized)
		if createErr != nil {
			return 0, zero, fmt.Errorf("创建%s key %s: %w", codec.noun, key, createErr)
		}
		return revision, value, nil
	}
	if err != nil {
		return 0, zero, fmt.Errorf("读取既有%s key %s: %w", codec.noun, key, err)
	}
	existing, err := codec.parse(current.Value())
	if err != nil {
		return 0, zero, fmt.Errorf("既有%s损坏，拒绝覆盖: %w", codec.noun, err)
	}
	if codec.revision(existing) == codec.revision(value) {
		if codec.digest(existing) != codec.digest(value) {
			return 0, zero, fmt.Errorf("业务 revision %d 已存在且内容不同", codec.revision(value))
		}
		return current.Revision(), value, nil
	}
	revision, err := kv.Update(ctx, key, normalized, current.Revision())
	if err != nil {
		return 0, zero, fmt.Errorf("CAS 更新%s key %s: %w", codec.noun, key, err)
	}
	return revision, value, nil
}
