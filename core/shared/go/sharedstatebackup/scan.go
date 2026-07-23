package sharedstatebackup

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"sort"

	"github.com/nats-io/nats.go/jetstream"

	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
)

// LogicalEntry is visible only to trusted in-process backup validators. Value
// is cleared after Observe returns and must not be logged or retained unless a
// validator needs a bounded copy for referential validation.
type LogicalEntry struct {
	PhysicalKey string
	Scope       sharedstate.Scope
	Key         string
	Revision    uint64
	Value       []byte
}

type ValidatorFactory interface {
	NewValidator(jetstream.KeyValue) Validator
}

type Validator interface {
	Name() string
	Observe(LogicalEntry) error
	Finish(context.Context) (ValidationResult, error)
}

func Scan(ctx context.Context, kv jetstream.KeyValue, factories []ValidatorFactory) (LogicalSummary, []ValidationResult, error) {
	if kv == nil {
		return LogicalSummary{}, nil, errors.New("Shared State KV 不能为空")
	}
	validators := make([]Validator, 0, len(factories))
	seen := map[string]struct{}{}
	for _, factory := range factories {
		if factory == nil {
			return LogicalSummary{}, nil, errors.New("Shared State 备份验证器工厂不能为空")
		}
		validator := factory.NewValidator(kv)
		if validator == nil || !safeName(validator.Name()) {
			return LogicalSummary{}, nil, errors.New("Shared State 备份验证器无效")
		}
		if _, exists := seen[validator.Name()]; exists {
			return LogicalSummary{}, nil, errors.New("Shared State 备份验证器名称重复")
		}
		seen[validator.Name()] = struct{}{}
		validators = append(validators, validator)
	}

	lister, err := kv.ListKeys(ctx)
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return finishScan(ctx, sha256.New(), LogicalSummary{}, validators)
	}
	if err != nil {
		return LogicalSummary{}, nil, fmt.Errorf("列举 Shared State keys: %w", err)
	}
	defer lister.Stop()
	keys := make([]string, 0)
	for key := range lister.Keys() {
		keys = append(keys, key)
	}
	if err := ctx.Err(); err != nil {
		return LogicalSummary{}, nil, fmt.Errorf("Shared State key 扫描未完整结束: %w", err)
	}
	sort.Strings(keys)
	unique := keys[:0]
	for _, key := range keys {
		if len(unique) == 0 || unique[len(unique)-1] != key {
			unique = append(unique, key)
		}
	}
	keys = unique

	digest := sha256.New()
	summary := LogicalSummary{}
	for _, physical := range keys {
		entry, getErr := kv.Get(ctx, physical)
		if errors.Is(getErr, jetstream.ErrKeyNotFound) {
			return LogicalSummary{}, nil, fmt.Errorf("Shared State 扫描期间 key 已变化: %w", getErr)
		}
		if getErr != nil {
			return LogicalSummary{}, nil, fmt.Errorf("读取 Shared State key: %w", getErr)
		}
		scope, key, parseErr := sharedstate.ParsePhysicalKeyForOperations(physical)
		if parseErr != nil {
			return LogicalSummary{}, nil, fmt.Errorf("Shared State 包含非法物理 key: %w", parseErr)
		}
		value := append([]byte(nil), entry.Value()...)
		writeLogicalDigest(digest, physical, entry.Revision(), value)
		summary.Entries++
		summary.ValueBytes += uint64(len(value))
		if entry.Revision() > summary.MaxRevision {
			summary.MaxRevision = entry.Revision()
		}
		observed := LogicalEntry{PhysicalKey: physical, Scope: scope, Key: key, Revision: entry.Revision(), Value: value}
		for _, validator := range validators {
			if err := validator.Observe(observed); err != nil {
				clear(value)
				return LogicalSummary{}, nil, fmt.Errorf("Shared State 验证器 %s: %w", validator.Name(), err)
			}
		}
		clear(value)
	}
	return finishScan(ctx, digest, summary, validators)
}

func finishScan(ctx context.Context, digest hash.Hash, summary LogicalSummary, validators []Validator) (LogicalSummary, []ValidationResult, error) {
	summary.Digest = hex.EncodeToString(digest.Sum(nil))
	results := make([]ValidationResult, 0, len(validators))
	for _, validator := range validators {
		result, err := validator.Finish(ctx)
		if err != nil {
			return LogicalSummary{}, nil, fmt.Errorf("Shared State 验证器 %s: %w", validator.Name(), err)
		}
		if result.Name != validator.Name() {
			return LogicalSummary{}, nil, fmt.Errorf("Shared State 验证器 %s 返回错误身份", validator.Name())
		}
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	return summary, results, nil
}

func writeLogicalDigest(target hash.Hash, key string, revision uint64, value []byte) {
	writeSized(target, []byte(key))
	var number [8]byte
	binary.BigEndian.PutUint64(number[:], revision)
	_, _ = target.Write(number[:])
	binary.BigEndian.PutUint64(number[:], uint64(len(value)))
	_, _ = target.Write(number[:])
	digest := sha256.Sum256(value)
	_, _ = target.Write(digest[:])
}

func writeSized(target hash.Hash, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = target.Write(size[:])
	_, _ = target.Write(value)
}
