package protocolbus

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	pluginhostv1 "cdsoft.com.cn/VastPlan/core/shared/go/pluginhost/v1"
	"google.golang.org/protobuf/proto"
)

var errPendingQueueFull = errors.New("会话 pending 请求队列已满")

// session 一个已接入插件的运行态：持有双向流，并把流上的消息按 request_id
// 关联回各自的等待者（§2.3 四类消息多路复用于单连接）。
type session struct {
	id            string
	pluginID      string
	pluginVersion string
	policy        LaunchPolicy
	launchToken   string // 关联到发起它的那次 Launch
	cmdMu         sync.Mutex
	cmd           *exec.Cmd

	streamMu      sync.Mutex
	stream        pluginhostv1.PluginHost_ChannelServer
	streamClaimed bool
	features      map[string]bool

	// gRPC 规定：同一个流上 SendMsg 不可并发调用（RecvMsg 与 SendMsg 可并发）。
	sendMu sync.Mutex

	pendingMu  sync.Mutex
	pending    map[string]chan *pluginhostv1.FromPlugin
	hostCallMu sync.Mutex
	hostCalls  map[string]context.CancelFunc

	// delegations 保存宿主为当前入站调用签发的短生命周期身份委托。插件只拿到
	// 随机引用，HostCall 时宿主据此重建权威 CallContext，而不信任插件回传的
	// principal / tenant / caller。委托随对应 Invoke 完成而销毁。
	delegationMu sync.RWMutex
	delegations  map[string]*contractv1.CallContext

	seq atomic.Uint64

	// lastSeen 最近一次收到插件消息的时刻（任何消息都算活着）。
	lastSeen atomic.Int64

	closeOnce    sync.Once
	done         chan struct{}
	teardownOnce sync.Once
	teardownDone chan struct{}
	// deadErr 会话死亡原因（崩溃/心跳超时/主动关闭）。
	deadErr atomic.Value
}

func newSession(id, pluginID, pluginVersion string) *session {
	s := &session{
		id:            id,
		pluginID:      pluginID,
		pluginVersion: pluginVersion,
		pending:       map[string]chan *pluginhostv1.FromPlugin{},
		hostCalls:     map[string]context.CancelFunc{},
		features:      map[string]bool{},
		delegations:   map[string]*contractv1.CallContext{},
		done:          make(chan struct{}),
		teardownDone:  make(chan struct{}),
	}
	s.touch()
	return s
}

func (s *session) claimStream(stream pluginhostv1.PluginHost_ChannelServer) bool {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	if s.streamClaimed {
		return false
	}
	s.stream = stream
	s.streamClaimed = true
	return true
}

func (s *session) hasFeature(feature string) bool {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	return s.features[feature]
}

func (s *session) beginHostCall(requestID string, cancel context.CancelFunc) bool {
	if requestID == "" {
		return false
	}
	s.hostCallMu.Lock()
	defer s.hostCallMu.Unlock()
	select {
	case <-s.done:
		return false
	default:
	}
	if _, duplicate := s.hostCalls[requestID]; duplicate {
		return false
	}
	s.hostCalls[requestID] = cancel
	return true
}

func (s *session) endHostCall(requestID string) {
	s.hostCallMu.Lock()
	delete(s.hostCalls, requestID)
	s.hostCallMu.Unlock()
}

func (s *session) cancelHostCall(requestID string) {
	s.hostCallMu.Lock()
	cancel := s.hostCalls[requestID]
	s.hostCallMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *session) issueDelegation(callCtx *contractv1.CallContext) (string, *contractv1.CallContext) {
	token := randomHex(32)
	trusted := &contractv1.CallContext{}
	if callCtx != nil {
		trusted = proto.Clone(callCtx).(*contractv1.CallContext)
	}
	forwarded := proto.Clone(trusted).(*contractv1.CallContext)

	s.delegationMu.Lock()
	s.delegations[token] = trusted
	s.delegationMu.Unlock()
	return token, forwarded
}

func (s *session) delegatedContext(token string) (*contractv1.CallContext, bool) {
	if token == "" {
		return nil, false
	}
	s.delegationMu.RLock()
	trusted, ok := s.delegations[token]
	s.delegationMu.RUnlock()
	if !ok {
		return nil, false
	}
	return proto.Clone(trusted).(*contractv1.CallContext), true
}

func (s *session) releaseDelegation(token string) {
	s.delegationMu.Lock()
	delete(s.delegations, token)
	s.delegationMu.Unlock()
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
func (s *session) await(requestID string, maxPending int) (chan *pluginhostv1.FromPlugin, error) {
	ch := make(chan *pluginhostv1.FromPlugin, 1)
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if len(s.pending) >= maxPending {
		return nil, errPendingQueueFull
	}
	s.pending[requestID] = ch
	return ch, nil
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

		s.hostCallMu.Lock()
		for id, cancel := range s.hostCalls {
			cancel()
			delete(s.hostCalls, id)
		}
		s.hostCallMu.Unlock()
	})
}

func (s *session) err() error {
	if v := s.deadErr.Load(); v != nil {
		return v.(errBox).err
	}
	return nil
}

// bindProcess 在握手完成后把进程句柄交给会话；若会话已经死亡则拒绝绑定，
// 避免“刚激活就崩溃”被 Launch 误报为成功。
func (s *session) bindProcess(cmd *exec.Cmd) bool {
	s.cmdMu.Lock()
	defer s.cmdMu.Unlock()
	select {
	case <-s.done:
		return false
	default:
		s.cmd = cmd
		return true
	}
}

func (s *session) killProcess() {
	s.cmdMu.Lock()
	defer s.cmdMu.Unlock()
	if s.cmd != nil && s.cmd.Process != nil {
		// cmd.Wait 由 Launch 创建的唯一 goroutine 负责，避免重复 Wait 的竞态。
		_ = s.cmd.Process.Kill()
	}
}

// errBox 让 atomic.Value 能存 nil 之外的 error（类型必须一致）。
type errBox struct{ err error }
