package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
)

// SourceEvent 表示期望态存储发生变化。Agent 收到后总是重新 Load 最新值，
// 因而连续 KV 更新可以自然合并，不会逐个应用已经过期的中间 revision。
type SourceEvent struct {
	Revision uint64
	Err      error
}

// WatchableDesiredStateSource 是支持推送触发的配置源；不实现它的 FileSource 仍按轮询工作。
type WatchableDesiredStateSource interface {
	Watch(context.Context) (<-chan SourceEvent, error)
}

// NATSDesiredStateSource 从 JetStream KV 读取并 watch 一份完整期望态。
type NATSDesiredStateSource struct {
	KV   jetstream.KeyValue
	Key  string
	Conn *nats.Conn
}

func (s NATSDesiredStateSource) Load(ctx context.Context) (deploymentv1.DesiredState, error) {
	if s.KV == nil || s.Key == "" {
		return deploymentv1.DesiredState{}, errors.New("NATS 期望态 source 未配置")
	}
	entry, err := s.KV.Get(ctx, s.Key)
	if err != nil {
		return deploymentv1.DesiredState{}, fmt.Errorf("读取 NATS 期望态 key %s: %w", s.Key, err)
	}
	return deploymentv1.Parse(entry.Value())
}

func (s NATSDesiredStateSource) Watch(ctx context.Context) (<-chan SourceEvent, error) {
	if s.KV == nil || s.Key == "" {
		return nil, errors.New("NATS 期望态 source 未配置")
	}
	watcher, err := s.KV.Watch(ctx, s.Key)
	if err != nil {
		return nil, fmt.Errorf("watch NATS 期望态 key %s: %w", s.Key, err)
	}
	out := make(chan SourceEvent, 8)
	go func() {
		defer close(out)
		defer func() {
			if watcher != nil {
				_ = watcher.Stop()
			}
		}()
		updates := watcher.Updates()
		var statusChanges <-chan nats.Status
		if s.Conn != nil {
			statusChanges = s.Conn.StatusChanged(nats.CONNECTED)
		}
		var retry <-chan time.Time
		var retryTimer *time.Timer
		lastRevision := uint64(0)
		restartWatcher := func() {
			if watcher != nil {
				_ = watcher.Stop()
			}
			watcher = nil
			updates = nil
			var watchErr error
			watcher, watchErr = s.KV.Watch(ctx, s.Key)
			if watchErr != nil {
				select {
				case out <- SourceEvent{Err: fmt.Errorf("重建 NATS 期望态 watcher: %w", watchErr)}:
				default:
				}
				if retryTimer == nil {
					retryTimer = time.NewTimer(250 * time.Millisecond)
				} else {
					retryTimer.Reset(250 * time.Millisecond)
				}
				retry = retryTimer.C
				return
			}
			updates = watcher.Updates()
			if retryTimer != nil && !retryTimer.Stop() {
				select {
				case <-retryTimer.C:
				default:
				}
			}
			retry = nil
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-statusChanges:
				// StatusChanged 对每个状态是一次性监听；每次重连后立即登记下一次。
				statusChanges = s.Conn.StatusChanged(nats.CONNECTED)
				restartWatcher()
			case <-retry:
				retry = nil
				restartWatcher()
			case entry, ok := <-updates:
				if !ok {
					updates = nil
					if retryTimer == nil {
						retryTimer = time.NewTimer(250 * time.Millisecond)
					} else {
						retryTimer.Reset(250 * time.Millisecond)
					}
					retry = retryTimer.C
					continue
				}
				if entry == nil { // 初始快照发送完毕标记，不代表配置变化。
					continue
				}
				if entry.Revision() <= lastRevision {
					continue
				}
				lastRevision = entry.Revision()
				event := SourceEvent{Revision: lastRevision}
				select {
				case out <- event:
				default:
					// Agent 总会 Load 最新值；队列满时合并中间 revision 是正确语义。
				}
			}
		}
	}()
	return out, nil
}

// NATSStateStore 把某节点实际态上报到独立 KV key。
type NATSStateStore struct {
	KV  jetstream.KeyValue
	Key string
}

func (s NATSStateStore) Load() (ActualState, error) {
	if s.KV == nil || s.Key == "" {
		return ActualState{}, errors.New("NATS 实际态 store 未配置")
	}
	entry, err := s.KV.Get(context.Background(), s.Key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return emptyActualState(), nil
	}
	if err != nil {
		return ActualState{}, fmt.Errorf("读取 NATS 实际态 key %s: %w", s.Key, err)
	}
	state, err := decodeActualState(entry.Value())
	if err != nil {
		return ActualState{}, fmt.Errorf("解析 NATS 实际态: %w", err)
	}
	return state, nil
}

func (s NATSStateStore) Save(state ActualState) error {
	if s.KV == nil || s.Key == "" {
		return errors.New("NATS 实际态 store 未配置")
	}
	if err := validateActualState(state); err != nil {
		return err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("序列化 NATS 实际态: %w", err)
	}
	if _, err := s.KV.Put(context.Background(), s.Key, raw); err != nil {
		return fmt.Errorf("上报 NATS 实际态 key %s: %w", s.Key, err)
	}
	return nil
}

// ReplicatedStateStore 以本地文件为恢复真源，并同步一份远端实际态供控制面观察。
// 任一副本写失败都会让 Agent 重试，但不会回滚已经健康运行的 unit。
type ReplicatedStateStore struct {
	Primary  StateStore
	Replicas []StateStore
}

func (s ReplicatedStateStore) Load() (ActualState, error) {
	if s.Primary == nil {
		return ActualState{}, errors.New("实际态 primary 未配置")
	}
	return s.Primary.Load()
}

func (s ReplicatedStateStore) Save(state ActualState) error {
	if s.Primary == nil {
		return errors.New("实际态 primary 未配置")
	}
	if err := s.Primary.Save(state); err != nil {
		return err
	}
	var joined error
	for _, replica := range s.Replicas {
		if replica == nil {
			continue
		}
		joined = errors.Join(joined, replica.Save(state))
	}
	return joined
}
