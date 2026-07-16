package controlplane

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

type LeaderRecord struct {
	SchemaVersion int       `json:"schema_version"`
	Election      string    `json:"election"`
	Holder        string    `json:"holder"`
	Token         string    `json:"token"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type LeaderElectionOptions struct {
	LeaseDuration time.Duration
	RenewEvery    time.Duration
	RetryEvery    time.Duration
	Logf          func(string, ...any)
}

// LeaderElector 使用 JetStream KV CAS 选出单写者。KV bucket TTL 是最终故障恢复，
// record.UpdatedAt 让接任者无需等待额外墓碑清理即可 CAS 接管已经过期的记录。
type LeaderElector struct {
	KV       jetstream.KeyValue
	Election string
	Identity string
	Options  LeaderElectionOptions
}

// Leadership 表示一段有 fencing token 的领导权。Lost 一旦有值，持有者必须立即停止写入。
type Leadership struct {
	kv       jetstream.KeyValue
	key      string
	record   LeaderRecord
	revision uint64
	options  LeaderElectionOptions
	cancel   context.CancelFunc
	done     chan struct{}
	lost     chan error
	mu       sync.Mutex
	once     sync.Once
}

func (e LeaderElector) Acquire(ctx context.Context) (*Leadership, error) {
	if e.KV == nil || e.Election == "" || e.Identity == "" {
		return nil, errors.New("leader election 的 KV、election 和 identity 必须配置")
	}
	options := normalizeLeaderOptions(e.Options)
	ticker := time.NewTicker(options.RetryEvery)
	defer ticker.Stop()
	for {
		leadership, acquired, err := e.tryAcquire(ctx, options)
		if err != nil {
			options.Logf("controller 选主失败 election=%s identity=%s: %v", e.Election, e.Identity, err)
		} else if acquired {
			return leadership, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (e LeaderElector) tryAcquire(parent context.Context, options LeaderElectionOptions) (*Leadership, bool, error) {
	key := "leaders." + keyToken(e.Election)
	record := LeaderRecord{
		SchemaVersion: 1, Election: e.Election, Holder: e.Identity,
		Token: randomLeaderToken(), UpdatedAt: time.Now().UTC(),
	}
	raw, _ := json.Marshal(record)
	revision, err := e.KV.Create(parent, key, raw)
	if err == nil {
		return startLeadership(parent, e.KV, key, record, revision, options), true, nil
	}
	entry, getErr := e.KV.Get(parent, key)
	if errors.Is(getErr, jetstream.ErrKeyNotFound) {
		return nil, false, nil
	}
	if getErr != nil {
		return nil, false, fmt.Errorf("读取 leader 记录: %w", getErr)
	}
	var current LeaderRecord
	if json.Unmarshal(entry.Value(), &current) != nil || current.SchemaVersion != 1 || current.Election != e.Election || current.Holder == "" || current.Token == "" {
		return nil, false, errors.New("leader 记录损坏")
	}
	if time.Since(current.UpdatedAt) < options.LeaseDuration {
		return nil, false, nil
	}
	revision, err = e.KV.Update(parent, key, raw, entry.Revision())
	if err != nil {
		return nil, false, nil // 另一候选者先完成 CAS；回到等待循环。
	}
	return startLeadership(parent, e.KV, key, record, revision, options), true, nil
}

func startLeadership(parent context.Context, kv jetstream.KeyValue, key string, record LeaderRecord, revision uint64, options LeaderElectionOptions) *Leadership {
	ctx, cancel := context.WithCancel(parent)
	leadership := &Leadership{
		kv: kv, key: key, record: record, revision: revision, options: options,
		cancel: cancel, done: make(chan struct{}), lost: make(chan error, 1),
	}
	go leadership.renew(ctx)
	return leadership
}

func (l *Leadership) Record() LeaderRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.record
}
func (l *Leadership) Lost() <-chan error { return l.lost }

func (l *Leadership) renew(ctx context.Context) {
	defer close(l.done)
	ticker := time.NewTicker(l.options.RenewEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.mu.Lock()
			record := l.record
			record.UpdatedAt = time.Now().UTC()
			raw, _ := json.Marshal(record)
			revision, err := l.kv.Update(ctx, l.key, raw, l.revision)
			if err == nil {
				l.record, l.revision = record, revision
			}
			l.mu.Unlock()
			if err != nil {
				select {
				case l.lost <- fmt.Errorf("controller 领导权续租失败: %w", err):
				default:
				}
				return
			}
		}
	}
}

func (l *Leadership) Close(ctx context.Context) error {
	var closeErr error
	l.once.Do(func() {
		l.cancel()
		select {
		case <-l.done:
		case <-ctx.Done():
			closeErr = ctx.Err()
		}
		l.mu.Lock()
		revision := l.revision
		l.mu.Unlock()
		if err := l.kv.Delete(ctx, l.key, jetstream.LastRevision(revision)); err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
			closeErr = errors.Join(closeErr, fmt.Errorf("释放 controller 领导权: %w", err))
		}
	})
	return closeErr
}

func normalizeLeaderOptions(options LeaderElectionOptions) LeaderElectionOptions {
	if options.LeaseDuration <= 0 {
		options.LeaseDuration = 12 * time.Second
	}
	if options.RenewEvery <= 0 || options.RenewEvery >= options.LeaseDuration/2 {
		options.RenewEvery = options.LeaseDuration / 3
	}
	if options.RetryEvery <= 0 {
		options.RetryEvery = time.Second
	}
	if options.Logf == nil {
		options.Logf = func(string, ...any) {}
	}
	return options
}

func randomLeaderToken() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
}
