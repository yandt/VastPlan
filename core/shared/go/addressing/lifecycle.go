package addressing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func (r *Router) Close() error {
	var closeErr error
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		registrations := make([]*Registration, 0, len(r.registrations))
		for _, registration := range r.registrations {
			registrations = append(registrations, registration)
		}
		r.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, registration := range registrations {
			closeErr = errors.Join(closeErr, registration.Close(ctx))
		}

		r.cancel()
		if r.streamServer != nil {
			r.streamServer.Stop()
		}
		if r.streamListener != nil {
			if err := r.streamListener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				closeErr = errors.Join(closeErr, err)
			}
		}
		if r.directoryW != nil {
			closeErr = errors.Join(closeErr, r.directoryW.Stop())
		}
		if r.cancelSub != nil {
			closeErr = errors.Join(closeErr, r.cancelSub.Unsubscribe())
		}
		r.mu.Lock()
		for _, cancel := range r.inflight {
			cancel()
		}
		r.inflight = map[string]context.CancelFunc{}
		r.pendingCancels = map[string]time.Time{}
		r.local = map[string][]localHandler{}
		r.localCursor = map[string]uint64{}
		r.streamLocal = map[string][]localStreamHandler{}
		r.streamCursor = map[string]uint64{}
		r.streamResolve = map[string]uint64{}
		r.mu.Unlock()
	})
	return closeErr
}

func randomID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// TransportError 保持远端处理器的传输失败与应用层 CallResult 分离。
type TransportError struct {
	Code      string
	Message   string
	Retryable bool
}

func (e *TransportError) Error() string { return e.Code + ": " + e.Message }

type Subscription struct{ sub *nats.Subscription }

func (s *Subscription) Close() error {
	if s == nil || s.sub == nil {
		return nil
	}
	return s.sub.Drain()
}

func (r *Router) handleCancel(msg *nats.Msg) {
	requestID := string(msg.Data)
	if requestID == "" {
		return
	}
	r.mu.Lock()
	cancel := r.inflight[requestID]
	if cancel == nil {
		// 取消消息与请求走不同 subject，跨连接时可能先到；短暂记忆可关闭这个竞态窗口。
		r.pendingCancels[requestID] = time.Now().Add(time.Minute)
	}
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (r *Router) startDirectoryWatch() error {
	watcher, err := r.Directory.WatchAll(r.ctx)
	if err != nil {
		return fmt.Errorf("watch 能力目录: %w", err)
	}
	r.directoryW = watcher
	go func() {
		for entry := range watcher.Updates() {
			if entry == nil {
				continue
			}
			r.applyDirectoryEntry(entry)
		}
	}()
	return nil
}

func (r *Router) applyDirectoryEntry(entry jetstream.KeyValueEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry.Operation() != jetstream.KeyValuePut {
		for capability, instances := range r.instances {
			delete(instances, entry.Key())
			if len(instances) == 0 {
				delete(r.instances, capability)
			}
		}
		return
	}
	var announcement Announcement
	if err := json.Unmarshal(entry.Value(), &announcement); err != nil {
		r.Logf("忽略非法能力目录记录 key=%s: %v", entry.Key(), err)
		return
	}
	if err := r.validateAnnouncement(entry.Key(), announcement); err != nil {
		r.Logf("忽略未通过身份校验的能力目录记录 key=%s: %v", entry.Key(), err)
		return
	}
	if r.instances[announcement.Capability] == nil {
		r.instances[announcement.Capability] = map[string]Announcement{}
	}
	r.instances[announcement.Capability][entry.Key()] = announcement
}

func (r *Router) directoryRefreshLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.refreshDirectory()
			r.expirePendingCancels()
		}
	}
}

func (r *Router) refreshDirectory() {
	ctx, cancel := context.WithTimeout(r.ctx, 5*time.Second)
	defer cancel()
	keys, err := r.Directory.Keys(ctx)
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			r.mu.Lock()
			r.instances = map[string]map[string]Announcement{}
			r.mu.Unlock()
		}
		return
	}
	next := map[string]map[string]Announcement{}
	for _, key := range keys {
		entry, err := r.Directory.Get(ctx, key)
		if err != nil {
			continue
		}
		var announcement Announcement
		if json.Unmarshal(entry.Value(), &announcement) != nil {
			continue
		}
		if err := r.validateAnnouncement(key, announcement); err != nil {
			continue
		}
		if next[announcement.Capability] == nil {
			next[announcement.Capability] = map[string]Announcement{}
		}
		next[announcement.Capability][key] = announcement
	}
	r.mu.Lock()
	r.instances = next
	r.mu.Unlock()
}

func (r *Router) expirePendingCancels() {
	now := time.Now()
	r.mu.Lock()
	for requestID, expiresAt := range r.pendingCancels {
		if !expiresAt.After(now) {
			delete(r.pendingCancels, requestID)
		}
	}
	r.mu.Unlock()
}

func deadlineContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := parent.Deadline(); ok {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}
