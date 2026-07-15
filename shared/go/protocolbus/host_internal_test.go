package protocolbus

// 同包测试：验证包内私有逻辑的边界。
//
// 这些符号（session 的关联/唤醒、票据生成）不导出，只有同包 _test.go 能测——
// 正是"单元测试与源码同目录"的理由（ADR-0018 §1）。

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	pluginhostv1 "github.com/yandt/VastPlan/shared/go/pluginhost/v1"
)

// 双向流上的请求/响应关联：deliver 必须把响应交给对应 request_id 的等待者。
func TestSession_AwaitDeliverCorrelation(t *testing.T) {
	s := newSession("sess-1", "p1", "0.1.0")

	chA := s.await("req-A")
	chB := s.await("req-B")

	s.deliver("req-B", &pluginhostv1.FromPlugin{
		Msg: &pluginhostv1.FromPlugin_Pong{Pong: &pluginhostv1.Pong{RequestId: "req-B"}},
	})

	select {
	case msg := <-chB:
		if msg.GetPong().RequestId != "req-B" {
			t.Fatalf("投递错了请求：期望 req-B，实际 %s", msg.GetPong().RequestId)
		}
	case <-time.After(time.Second):
		t.Fatal("req-B 的等待者未收到响应")
	}

	// A 不该被 B 的响应误唤醒
	select {
	case <-chA:
		t.Fatal("req-A 不应收到 req-B 的响应")
	default:
	}
}

// 无人等待的响应（迟到/重复）应被安静丢弃，不得阻塞读循环或 panic。
func TestSession_DeliverToUnknownRequestIsDropped(t *testing.T) {
	s := newSession("sess-1", "p1", "0.1.0")
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.deliver("req-不存在", &pluginhostv1.FromPlugin{})
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("投递给不存在的等待者阻塞了——会拖死读循环")
	}
}

// release 之后再投递同样不得阻塞（等待者已超时走人）。
func TestSession_DeliverAfterReleaseIsDropped(t *testing.T) {
	s := newSession("sess-1", "p1", "0.1.0")
	s.await("req-A")
	s.release("req-A")

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.deliver("req-A", &pluginhostv1.FromPlugin{})
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("向已 release 的请求投递时阻塞了")
	}
}

// 插件崩溃时，所有在途等待者必须**立刻**被唤醒（通道关闭），
// 而不是各自挂到超时——这是 ADR-0004 故障隔离的实质。
func TestSession_MarkDeadWakesAllWaiters(t *testing.T) {
	s := newSession("sess-1", "p1", "0.1.0")

	chs := make([]chan *pluginhostv1.FromPlugin, 5)
	for i := range chs {
		chs[i] = s.await(requestIDFor(i))
	}

	s.markDead(errors.New("插件崩溃"))

	for i, ch := range chs {
		select {
		case _, ok := <-ch:
			if ok {
				t.Fatalf("等待者 %d 应收到通道关闭信号，而非真实消息", i)
			}
		case <-time.After(time.Second):
			t.Fatalf("等待者 %d 未被唤醒——插件崩溃后它会一直挂着", i)
		}
	}

	if s.err() == nil || !strings.Contains(s.err().Error(), "崩溃") {
		t.Fatalf("会话应记录死亡原因，实际: %v", s.err())
	}
}

// markDead 可被重复调用（崩溃与主动关闭可能并发到达），不得 panic。
func TestSession_MarkDeadIsIdempotent(t *testing.T) {
	s := newSession("sess-1", "p1", "0.1.0")
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.markDead(errors.New("并发关闭"))
		}()
	}
	wg.Wait() // 不 panic 即通过（close(done) 二次调用会 panic）
}

// request_id 必须唯一，否则响应会串台。
func TestSession_RequestIDsAreUnique(t *testing.T) {
	s := newSession("sess-1", "p1", "0.1.0")
	const n = 200
	seen := make(map[string]struct{}, n)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := s.nextRequestID()
			mu.Lock()
			defer mu.Unlock()
			if _, dup := seen[id]; dup {
				t.Errorf("request_id 重复: %s——响应会串台", id)
			}
			seen[id] = struct{}{}
		}()
	}
	wg.Wait()
	if len(seen) != n {
		t.Fatalf("期望 %d 个唯一 request_id，实际 %d", n, len(seen))
	}
}

// 会话票据与 launch token 必须唯一：前者是审计与回调鉴权的锚，后者用于对应 Launch。
func TestNewSessionIDAndToken_Unique(t *testing.T) {
	const n = 100
	ids := make(map[string]struct{}, n)
	tokens := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		ids[newSessionID()] = struct{}{}
		tokens[newToken()] = struct{}{}
	}
	if len(ids) != n {
		t.Fatalf("会话票据重复：期望 %d 个唯一值，实际 %d", n, len(ids))
	}
	if len(tokens) != n {
		t.Fatalf("launch token 重复：期望 %d 个唯一值，实际 %d", n, len(tokens))
	}
	if !strings.HasPrefix(newSessionID(), "sess-") || !strings.HasPrefix(newToken(), "lt-") {
		t.Fatal("票据/令牌应带可辨识前缀，便于日志排障")
	}
}

// touch 后闲置时长应被重置——心跳据此判定失联。
func TestSession_TouchResetsIdle(t *testing.T) {
	s := newSession("sess-1", "p1", "0.1.0")
	time.Sleep(30 * time.Millisecond)
	if s.idleFor() < 20*time.Millisecond {
		t.Fatalf("闲置时长应随时间增长，实际 %v", s.idleFor())
	}
	s.touch()
	if s.idleFor() > 10*time.Millisecond {
		t.Fatalf("touch 后闲置应归零，实际 %v", s.idleFor())
	}
}

// 生产默认时限必须是合理正数——防止有人误改为 0（=立即超时，插件永远装不上）。
func TestDefaultTimeouts_Sane(t *testing.T) {
	cases := []struct {
		name string
		v    time.Duration
		min  time.Duration
	}{
		{"launch", defaultLaunchTimeout, time.Second},
		{"call", defaultCallTimeout, time.Second},
		{"heartbeatEvery", defaultHeartbeatEvery, 100 * time.Millisecond},
		{"heartbeatTimeout", defaultHeartbeatTimeout, time.Second},
	}
	for _, c := range cases {
		if c.v < c.min {
			t.Errorf("%s 默认时限 %v 过短（应 ≥ %v），会误杀正常插件", c.name, c.v, c.min)
		}
	}
	if defaultHeartbeatTimeout <= defaultHeartbeatEvery {
		t.Fatalf("心跳超时(%v)必须大于心跳间隔(%v)，否则每次都会误判失联",
			defaultHeartbeatTimeout, defaultHeartbeatEvery)
	}
}

// Host 未 Start 时 Launch 应立刻报错——插件无处回连（宿主是服务端）。
func TestHost_LaunchWithoutStartFails(t *testing.T) {
	h := NewHost("backend", "0.1.0", nil, nil)
	if _, err := h.Launch(t.Context(), "/nonexistent"); err == nil {
		t.Fatal("未 Start 就 Launch 应报错")
	}
}

func requestIDFor(i int) string { return "req-" + string(rune('A'+i)) }
