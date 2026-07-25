// Package addressing 实现后端服务间位置透明的 capability 寻址。
//
// 本地命中走函数直调；远端走 NATS request-reply。业务签名始终是
// CallTarget + CallContext + payload，传输差异不泄漏给调用方。
package addressing

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/observability"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/protocollimit"
)

const cancelSubject = "vp.rpc.cancel.v1"

var ErrCapabilityNotFound = errors.New("全局能力目录中没有健康实例")

// InvokeHandler 是本地与远端服务实现共用的处理签名。
type InvokeHandler func(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)

type EventHandler func(context.Context, *contractv1.CallContext, *contractv1.CallEvent) error

// Announcement 是全局能力目录的一条实例租约。
type Announcement struct {
	SchemaVersion      int       `json:"schema_version"`
	Capability         string    `json:"capability"`
	ExtensionPoint     string    `json:"extension_point"`
	ServiceRole        string    `json:"service_role"`
	LogicalService     string    `json:"logical_service,omitempty"`
	RoutingDomain      string    `json:"routing_domain,omitempty"`
	PartitionKey       string    `json:"partition_key,omitempty"`
	InstancePolicy     string    `json:"instance_policy,omitempty"`
	StateModel         string    `json:"state_model,omitempty"`
	Visibility         string    `json:"visibility,omitempty"`
	Routing            string    `json:"routing,omitempty"`
	InstanceID         string    `json:"instance_id"`
	NodeID             string    `json:"node_id"`
	UnitID             string    `json:"unit_id"`
	Subject            string    `json:"subject"`
	StreamEndpoint     string    `json:"stream_endpoint,omitempty"`
	Version            string    `json:"version,omitempty"`
	Health             string    `json:"health"`
	Readiness          string    `json:"readiness,omitempty"`
	ReadinessReason    string    `json:"readiness_reason,omitempty"`
	Generation         uint64    `json:"generation,omitempty"`
	FencingToken       string    `json:"fencing_token,omitempty"`
	LeaseExpiresAt     time.Time `json:"lease_expires_at,omitempty"`
	UpdatedAt          time.Time `json:"updated_at"`
	TransportPublicKey string    `json:"transport_public_key,omitempty"`
	TransportTimestamp string    `json:"transport_timestamp,omitempty"`
	TransportNonce     string    `json:"transport_nonce,omitempty"`
	TransportSignature string    `json:"transport_signature,omitempty"`
}

type localHandler struct {
	registrationID string
	handler        InvokeHandler
	record         Announcement
}

type localStreamHandler struct {
	registrationID string
	handler        StreamHandler
	record         Announcement
}

// Router 同时持有本地 fast path、NATS 数据面和能力目录缓存。
type Router struct {
	NC          *nats.Conn
	Directory   jetstream.KeyValue
	JetStream   jetstream.JetStream
	Events      jetstream.Stream
	NodeID      string
	CallTimeout time.Duration
	// Limits 约束一元调用和流式调用的资源占用。CallTimeout 仅作为旧配置兼容覆盖项。
	Limits   protocollimit.Limits
	Logf     func(string, ...any)
	Observer *observability.Observer
	// Transport 在生产模式下必须配置：它对 NATS/stream 信封签名，并在处理前
	// 重建权威工作负载身份。nil 仅保留给显式本地开发和测试。
	Transport *TransportSecurity

	ctx    context.Context
	cancel context.CancelFunc

	mu             sync.RWMutex
	local          map[string][]localHandler
	localCursor    map[string]uint64
	streamLocal    map[string][]localStreamHandler
	streamCursor   map[string]uint64
	streamResolve  map[string]uint64
	instances      map[string]map[string]Announcement // capability -> directory key -> record
	registrations  map[string]*Registration
	inflight       map[string]context.CancelFunc
	outboundCalls  int
	activeCalls    int
	pendingCancels map[string]time.Time
	closed         bool
	closeOnce      sync.Once
	cancelSub      *nats.Subscription
	directoryW     jetstream.KeyWatcher
	streamServer   *grpc.Server
	streamListener net.Listener
	streamEndpoint string
	streamCreds    credentials.TransportCredentials
	streamInsecure bool
}

func NewRouter(nc *nats.Conn, directory jetstream.KeyValue, nodeID string, logf func(string, ...any)) (*Router, error) {
	return newRouter(nc, directory, nodeID, logf, nil)
}

func NewSecureRouter(nc *nats.Conn, directory jetstream.KeyValue, nodeID string, logf func(string, ...any), security *TransportSecurity) (*Router, error) {
	if security == nil {
		return nil, errors.New("生产 addressing router 必须配置传输身份")
	}
	identity := security.SelfIdentity()
	if identity.NodeID == "" || identity.NodeID != nodeID {
		return nil, fmt.Errorf("传输身份 nodeID %q 与 router nodeID %q 不一致", identity.NodeID, nodeID)
	}
	return newRouter(nc, directory, nodeID, logf, security)
}

func newRouter(nc *nats.Conn, directory jetstream.KeyValue, nodeID string, logf func(string, ...any), security *TransportSecurity) (*Router, error) {
	if nc == nil || directory == nil || nodeID == "" {
		return nil, errors.New("addressing router 的 NATS、目录和 node id 必须配置")
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	js, err := jetstream.New(nc)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("初始化 JetStream: %w", err)
	}
	events, err := js.Stream(ctx, controlplane.EventsStream)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("打开持久事件 stream: %w", err)
	}
	r := &Router{
		NC: nc, Directory: directory, JetStream: js, Events: events,
		NodeID: nodeID, Limits: protocollimit.Default(), Logf: logf, Observer: observability.New(nil, nil), Transport: security,
		ctx: ctx, cancel: cancel, local: map[string][]localHandler{}, localCursor: map[string]uint64{},
		streamLocal: map[string][]localStreamHandler{}, streamCursor: map[string]uint64{},
		streamResolve: map[string]uint64{},
		instances:     map[string]map[string]Announcement{}, registrations: map[string]*Registration{},
		inflight: map[string]context.CancelFunc{}, pendingCancels: map[string]time.Time{},
	}
	sub, err := nc.Subscribe(cancelSubject, r.handleCancel)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("订阅取消信号: %w", err)
	}
	r.cancelSub = sub
	if err := nc.Flush(); err != nil {
		_ = sub.Unsubscribe()
		cancel()
		return nil, fmt.Errorf("确认取消订阅: %w", err)
	}
	if err := r.startDirectoryWatch(); err != nil {
		_ = sub.Unsubscribe()
		cancel()
		return nil, err
	}
	go r.directoryRefreshLoop()
	return r, nil
}
