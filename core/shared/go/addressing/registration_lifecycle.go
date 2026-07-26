package addressing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
)

func (registration *Registration) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			registration.recordMu.Lock()
			record := registration.record
			now := time.Now().UTC()
			record.UpdatedAt = now
			record.LeaseExpiresAt = now.Add(30 * time.Second)
			heartbeatCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := registration.router.putAnnouncement(heartbeatCtx, registration.router.Directory, registration.key, record)
			cancel()
			if err == nil {
				registration.record = record
			}
			registration.recordMu.Unlock()
			if err != nil {
				registration.router.Logf("刷新 capability 租约失败 %s: %v", record.Capability, err)
			}
		}
	}
}

// SetReadiness 原子更新能力租约的就绪状态。draining 会从路由选择中摘除，但保留
// 目录记录供控制面审计；旧 generation/fencing token 不得被新状态覆盖。
func (registration *Registration) SetReadiness(ctx context.Context, readiness, reason string) error {
	if readiness != "ready" && readiness != "degraded" && readiness != "draining" {
		return fmt.Errorf("未知 readiness 状态 %q", readiness)
	}
	registration.recordMu.Lock()
	defer registration.recordMu.Unlock()
	record := registration.record
	record.Readiness, record.ReadinessReason = readiness, reason
	if readiness == "draining" {
		record.Health = "draining"
	} else {
		record.Health = "healthy"
	}
	record.UpdatedAt = time.Now().UTC()
	if registration.localOnly {
		registration.record = record
		registration.updateLocalRecord(record)
		return nil
	}
	if err := registration.router.putAnnouncement(ctx, registration.router.Directory, registration.key, record); err != nil {
		return err
	}
	registration.record = record
	registration.updateLocalRecord(record)
	return nil
}

func (registration *Registration) updateLocalRecord(record Announcement) {
	registration.router.mu.Lock()
	defer registration.router.mu.Unlock()
	entries := registration.router.local[record.Capability]
	for index := range entries {
		if entries[index].registrationID == registration.id {
			entries[index].record = record
			registration.router.local[record.Capability] = entries
			registration.router.notifyTopologyChangeLocked()
			return
		}
	}
}

func (registration *Registration) Close(ctx context.Context) error {
	registration.once.Do(func() {
		registration.active.Store(false)
		registration.cancel()
		registration.recordMu.Lock()
		record := registration.record
		registration.recordMu.Unlock()
		registration.router.mu.Lock()
		locals := registration.router.local[record.Capability]
		for index := range locals {
			if locals[index].registrationID != registration.id {
				continue
			}
			locals = append(locals[:index], locals[index+1:]...)
			break
		}
		if len(locals) == 0 {
			delete(registration.router.local, record.Capability)
			delete(registration.router.localCursor, record.Capability)
		} else {
			registration.router.local[record.Capability] = locals
		}
		registration.router.notifyTopologyChangeLocked()
		if registration.stream {
			streams := registration.router.streamLocal[record.Capability]
			for index := range streams {
				if streams[index].registrationID == registration.id {
					streams = append(streams[:index], streams[index+1:]...)
					break
				}
			}
			if len(streams) == 0 {
				delete(registration.router.streamLocal, record.Capability)
				delete(registration.router.streamCursor, record.Capability)
			} else {
				registration.router.streamLocal[record.Capability] = streams
			}
		}
		delete(registration.router.registrations, registration.id)
		if instances := registration.router.instances[record.Capability]; instances != nil {
			delete(instances, registration.key)
			if len(instances) == 0 {
				delete(registration.router.instances, record.Capability)
			}
		}
		registration.router.mu.Unlock()
		if err := registration.router.Directory.Delete(ctx, registration.key); err != nil {
			if !errors.Is(err, jetstream.ErrKeyNotFound) {
				registration.closeErr = errors.Join(registration.closeErr, err)
			}
		}
		if registration.sub != nil {
			registration.closeErr = errors.Join(registration.closeErr, registration.sub.Drain())
		}
		if registration.directSub != nil {
			registration.closeErr = errors.Join(registration.closeErr, registration.directSub.Drain())
		}
	})
	return registration.closeErr
}

func (r *Router) putAnnouncement(ctx context.Context, directory interface {
	Put(context.Context, string, []byte) (uint64, error)
}, key string, record Announcement) error {
	prepared, err := r.prepareAnnouncement(key, record)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(prepared)
	if err != nil {
		return err
	}
	if _, err := directory.Put(ctx, key, raw); err != nil {
		return fmt.Errorf("发布 capability 租约 %s: %w", record.Capability, err)
	}
	return nil
}

// HostHandler 把协议宿主适配成寻址层 handler，不复制业务契约。
func HostHandler(invoke func(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)) InvokeHandler {
	return invoke
}
