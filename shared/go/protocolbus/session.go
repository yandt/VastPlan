package protocolbus

import (
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	pluginhostv1 "github.com/yandt/VastPlan/shared/go/pluginhost/v1"
)

// session 一个已接入插件的运行态：持有双向流，并把流上的消息按 request_id
// 关联回各自的等待者（§2.3 四类消息多路复用于单连接）。
type session struct {
	id            string
	pluginID      string
	pluginVersion string
	launchToken   string // 关联到发起它的那次 Launch
	cmd           *exec.Cmd

	stream pluginhostv1.PluginHost_ChannelServer

	// gRPC 规定：同一个流上 SendMsg 不可并发调用（RecvMsg 与 SendMsg 可并发）。
	sendMu sync.Mutex

	pendingMu sync.Mutex
	pending   map[string]chan *pluginhostv1.FromPlugin

	seq atomic.Uint64

	// lastSeen 最近一次收到插件消息的时刻（任何消息都算活着）。
	lastSeen atomic.Int64

	closeOnce sync.Once
	done      chan struct{}
	// deadErr 会话死亡原因（崩溃/心跳超时/主动关闭）。
	deadErr atomic.Value
}

func newSession(id, pluginID, pluginVersion string) *session {
	s := &session{
		id:            id,
		pluginID:      pluginID,
		pluginVersion: pluginVersion,
		pending:       map[string]chan *pluginhostv1.FromPlugin{},
		done:          make(chan struct{}),
	}
	s.touch()
	return s
}

func (s *session) touch() { s.lastSeen.Store(time.Now().UnixNano()) }

func (s *session) idleFor() time.Duration {
	return time.Since(time.Unix(0, s.lastSeen.Load()))
}

func (s *session) nextRequestID() string {
	return fmt.Sprintf("req-%d", s.seq.Add(1))
}

// send 向插件发一条消息（串行化，遵守 gRPC 流的发送约束）。
func (s *session) send(msg *pluginhostv1.FromHost) error {
	select {
	case <-s.done:
		return s.err()
	default:
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.stream.Send(msg)
}

// await 注册一个等待者并返回其接收通道；调用方须在用完后 release。
func (s *session) await(requestID string) chan *pluginhostv1.FromPlugin {
	ch := make(chan *pluginhostv1.FromPlugin, 1)
	s.pendingMu.Lock()
	s.pending[requestID] = ch
	s.pendingMu.Unlock()
	return ch
}

func (s *session) release(requestID string) {
	s.pendingMu.Lock()
	delete(s.pending, requestID)
	s.pendingMu.Unlock()
}

// deliver 把带 request_id 的响应投递给等待者；无人等待则丢弃（迟到的响应）。
func (s *session) deliver(requestID string, msg *pluginhostv1.FromPlugin) {
	s.pendingMu.Lock()
	ch, ok := s.pending[requestID]
	s.pendingMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- msg:
	default: // 已有结果或等待者已走，不阻塞流的读取循环
	}
}

// markDead 标记会话死亡并唤醒所有在途等待者——插件崩溃时它们必须立刻脱身，
// 不能挂到各自超时（ADR-0004 故障隔离）。
func (s *session) markDead(err error) {
	s.closeOnce.Do(func() {
		s.deadErr.Store(errBox{err})
		close(s.done)

		s.pendingMu.Lock()
		for id, ch := range s.pending {
			close(ch) // 关闭通道：等待者据此判定"未收到响应"
			delete(s.pending, id)
		}
		s.pendingMu.Unlock()
	})
}

func (s *session) err() error {
	if v := s.deadErr.Load(); v != nil {
		return v.(errBox).err
	}
	return nil
}

// errBox 让 atomic.Value 能存 nil 之外的 error（类型必须一致）。
type errBox struct{ err error }
