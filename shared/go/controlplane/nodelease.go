package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// NodeRecord 是调度器可见的节点租约内容。KV bucket TTL 是存活真相，UpdatedAt 仅供审计。
type NodeRecord struct {
	SchemaVersion int               `json:"schema_version"`
	NodeID        string            `json:"node_id"`
	Labels        map[string]string `json:"labels,omitempty"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

type NodeLeaseOptions struct {
	HeartbeatEvery time.Duration
	FailureTimeout time.Duration
	Logf           func(string, ...any)
}

// NodeLease 周期续租节点身份；连续无法续租时通过 Lost 通知主进程自我隔离。
type NodeLease struct {
	kv     jetstream.KeyValue
	record NodeRecord
	key    string
	cancel context.CancelFunc
	done   chan struct{}
	lost   chan error
	once   sync.Once
}

func StartNodeLease(parent context.Context, kv jetstream.KeyValue, nodeID string, labels map[string]string, options NodeLeaseOptions) (*NodeLease, error) {
	if kv == nil || nodeID == "" {
		return nil, errors.New("节点租约的 KV 与 node id 必须配置")
	}
	if options.HeartbeatEvery <= 0 {
		options.HeartbeatEvery = 5 * time.Second
	}
	if options.FailureTimeout <= options.HeartbeatEvery {
		options.FailureTimeout = 15 * time.Second
	}
	if options.Logf == nil {
		options.Logf = func(string, ...any) {}
	}
	ctx, cancel := context.WithCancel(parent)
	lease := &NodeLease{
		kv: kv, key: NodeKey(nodeID), cancel: cancel, done: make(chan struct{}), lost: make(chan error, 1),
		record: NodeRecord{SchemaVersion: 1, NodeID: nodeID, Labels: cloneLabels(labels)},
	}
	if err := lease.heartbeat(ctx); err != nil {
		cancel()
		return nil, err
	}
	go lease.run(ctx, options)
	return lease, nil
}

func (l *NodeLease) Lost() <-chan error { return l.lost }

func (l *NodeLease) Close(ctx context.Context) error {
	var closeErr error
	l.once.Do(func() {
		l.cancel()
		select {
		case <-l.done:
		case <-ctx.Done():
			closeErr = ctx.Err()
		}
		if err := l.kv.Delete(ctx, l.key); err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
			closeErr = errors.Join(closeErr, fmt.Errorf("删除节点租约: %w", err))
		}
	})
	return closeErr
}

func (l *NodeLease) run(ctx context.Context, options NodeLeaseOptions) {
	defer close(l.done)
	ticker := time.NewTicker(options.HeartbeatEvery)
	defer ticker.Stop()
	var failedSince time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := l.heartbeat(ctx)
			if err == nil {
				failedSince = time.Time{}
				continue
			}
			options.Logf("节点租约续租失败 node=%s: %v", l.record.NodeID, err)
			if failedSince.IsZero() {
				failedSince = time.Now()
			}
			if time.Since(failedSince) >= options.FailureTimeout {
				select {
				case l.lost <- fmt.Errorf("节点 %s 连续 %v 无法续租: %w", l.record.NodeID, options.FailureTimeout, err):
				default:
				}
				return
			}
		}
	}
}

func (l *NodeLease) heartbeat(ctx context.Context) error {
	record := l.record
	record.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := l.kv.Put(ctx, l.key, raw); err != nil {
		return fmt.Errorf("写入节点租约: %w", err)
	}
	return nil
}

func cloneLabels(labels map[string]string) map[string]string {
	clone := make(map[string]string, len(labels))
	for key, value := range labels {
		clone[key] = value
	}
	return clone
}
