package addressing

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	addressingv1 "cdsoft.com.cn/VastPlan/shared/go/addressing/v1"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/shared/go/errorcode"
)

// StreamHandler 直接在双向流上处理分片。HTTP/2 流控会在对端处理速度不足时阻塞 Send，
// 因而调用方不能绕过背压无限缓存。返回值是流结束后的统一 CallResult。
type StreamHandler func(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte, *ServerStream) (*contractv1.CallResult, []byte, error)

type StreamServerConfig struct {
	Listen    string
	Advertise string
	TLSConfig *tls.Config
	Insecure  bool
}

type StreamClientConfig struct {
	Credentials credentials.TransportCredentials
	Insecure    bool
}

// ConfigureStreamClient 配置流式端点的客户端信任根。生产模式不允许静默降级明文。
func (r *Router) ConfigureStreamClient(config StreamClientConfig) error {
	if config.Insecure && config.Credentials != nil {
		return errors.New("流式客户端 insecure 不能与 TLS credentials 混用")
	}
	if !config.Insecure && config.Credentials == nil {
		return errors.New("生产流式客户端必须配置 TLS credentials")
	}
	r.mu.Lock()
	r.streamInsecure = config.Insecure
	r.streamCreds = config.Credentials
	r.mu.Unlock()
	return nil
}

// StartStreamServer 启动 capability gRPC 双向流端点。生产模式强制 TLS 1.3 mTLS；
// Insecure 只用于显式本地开发和测试。
func (r *Router) StartStreamServer(config StreamServerConfig) (string, error) {
	if config.Listen == "" {
		config.Listen = "127.0.0.1:0"
	}
	if config.Insecure && config.TLSConfig != nil {
		return "", errors.New("流式服务 insecure 不能与 TLS 配置混用")
	}
	var options []grpc.ServerOption
	if !config.Insecure {
		if config.TLSConfig == nil {
			return "", errors.New("生产流式服务必须配置 TLS 1.3 mTLS")
		}
		tlsConfig := config.TLSConfig.Clone()
		if tlsConfig.MinVersion == 0 || tlsConfig.MinVersion < tls.VersionTLS13 {
			tlsConfig.MinVersion = tls.VersionTLS13
		}
		if tlsConfig.ClientAuth != tls.RequireAndVerifyClientCert || tlsConfig.ClientCAs == nil {
			return "", errors.New("生产流式服务必须 RequireAndVerifyClientCert 并配置 ClientCAs")
		}
		options = append(options, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}
	listener, err := net.Listen("tcp", config.Listen)
	if err != nil {
		return "", fmt.Errorf("监听流式端点: %w", err)
	}
	endpoint := config.Advertise
	if endpoint == "" {
		endpoint = listener.Addr().String()
	}
	r.mu.Lock()
	if r.streamServer != nil {
		r.mu.Unlock()
		_ = listener.Close()
		return "", errors.New("流式服务已经启动")
	}
	server := grpc.NewServer(options...)
	addressingv1.RegisterCapabilityStreamServer(server, &capabilityStreamService{router: r})
	r.streamServer = server
	r.streamListener = listener
	r.streamEndpoint = endpoint
	if config.Insecure {
		r.streamInsecure = true
	}
	r.mu.Unlock()
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			r.Logf("流式 gRPC 服务退出: %v", err)
		}
	}()
	return endpoint, nil
}

// RegisterStream 发布一个只能通过双向流调用的 capability。
func (r *Router) RegisterStream(ctx context.Context, options RegisterOptions, handler StreamHandler) (*Registration, error) {
	if options.Capability == "" || options.ExtensionPoint == "" || handler == nil {
		return nil, errors.New("stream capability、extension point 和 handler 不能为空")
	}
	r.mu.RLock()
	endpoint := r.streamEndpoint
	r.mu.RUnlock()
	if endpoint == "" {
		return nil, errors.New("注册流式 capability 前必须启动流式服务")
	}
	if options.InstanceID == "" {
		options.InstanceID = r.NodeID + "." + options.UnitID + "." + randomID()
	}
	record := Announcement{
		SchemaVersion: 1, Capability: options.Capability, ExtensionPoint: options.ExtensionPoint,
		ServiceRole: options.ServiceRole, InstanceID: options.InstanceID, NodeID: r.NodeID,
		UnitID: options.UnitID, Version: options.Version, Subject: controlplane.RPCSubject(options.Capability),
		StreamEndpoint: endpoint, Health: "healthy", UpdatedAt: nowUTC(),
	}
	key := controlplane.CapabilityKey(options.Capability, options.InstanceID)
	if err := putAnnouncement(ctx, r.Directory, key, record); err != nil {
		return nil, err
	}
	id := randomID()
	registrationCtx, cancel := context.WithCancel(r.ctx)
	registration := &Registration{router: r, record: record, key: key, id: id, cancel: cancel, stream: true}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		cancel()
		_ = r.Directory.Delete(ctx, key)
		return nil, errors.New("addressing router 已关闭")
	}
	r.streamLocal[options.Capability] = append(r.streamLocal[options.Capability], localStreamHandler{registrationID: id, handler: handler})
	r.registrations[id] = registration
	r.mu.Unlock()
	go registration.heartbeat(registrationCtx)
	return registration, nil
}

// RemoteStream 是调用方持有的双向流。Send 与 Recv 可各由一个 goroutine 并发调用。
type RemoteStream struct {
	requestID string
	raw       grpc.BidiStreamingClient[addressingv1.StreamFrame, addressingv1.StreamFrame]
	conn      *grpc.ClientConn
	cancel    context.CancelFunc

	sendMu   sync.Mutex
	sendSeq  uint64
	recvSeq  uint64
	resultMu sync.RWMutex
	result   *addressingv1.StreamResult
	close    sync.Once
}

func (r *Router) InvokeStream(ctx context.Context, target *contractv1.CallTarget, callCtx *contractv1.CallContext, initialPayload []byte) (*RemoteStream, error) {
	if target == nil || target.Capability == "" {
		return nil, errors.New("流式调用目标 capability 不能为空")
	}
	instances := r.Instances(target.Capability)
	streamInstances := instances[:0]
	for _, instance := range instances {
		if instance.StreamEndpoint != "" {
			streamInstances = append(streamInstances, instance)
		}
	}
	if len(streamInstances) == 0 {
		return nil, fmt.Errorf("%w: %s 没有健康流式端点", ErrCapabilityNotFound, target.Capability)
	}
	sort.Slice(streamInstances, func(i, j int) bool { return streamInstances[i].InstanceID < streamInstances[j].InstanceID })
	r.mu.Lock()
	index := r.streamResolve[target.Capability] % uint64(len(streamInstances))
	r.streamResolve[target.Capability]++
	creds, allowInsecure := r.streamCreds, r.streamInsecure
	r.mu.Unlock()
	var dialCreds credentials.TransportCredentials
	if allowInsecure {
		dialCreds = insecure.NewCredentials()
	} else if creds != nil {
		dialCreds = creds
	} else {
		return nil, errors.New("流式客户端未配置 TLS credentials")
	}
	conn, err := grpc.NewClient(streamInstances[index].StreamEndpoint, grpc.WithTransportCredentials(dialCreds))
	if err != nil {
		return nil, fmt.Errorf("连接流式端点: %w", err)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	raw, err := addressingv1.NewCapabilityStreamClient(conn).Open(streamCtx)
	if err != nil {
		cancel()
		_ = conn.Close()
		return nil, fmt.Errorf("打开 capability 流: %w", err)
	}
	requestID := randomID()
	if err := raw.Send(&addressingv1.StreamFrame{RequestId: requestID, Sequence: 0, Body: &addressingv1.StreamFrame_Open{Open: &addressingv1.StreamOpen{
		Target: target, Context: callCtx, InitialPayload: initialPayload,
	}}}); err != nil {
		cancel()
		_ = conn.Close()
		return nil, fmt.Errorf("发送流式调用起始帧: %w", err)
	}
	return &RemoteStream{requestID: requestID, raw: raw, conn: conn, cancel: cancel, sendSeq: 1}, nil
}

func (s *RemoteStream) Send(payload []byte) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	err := s.raw.Send(&addressingv1.StreamFrame{RequestId: s.requestID, Sequence: s.sendSeq, Body: &addressingv1.StreamFrame_Payload{Payload: &addressingv1.StreamPayload{Data: payload}}})
	if err == nil {
		s.sendSeq++
	}
	return err
}

func (s *RemoteStream) CloseSend() error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if err := s.raw.Send(&addressingv1.StreamFrame{RequestId: s.requestID, Sequence: s.sendSeq, Body: &addressingv1.StreamFrame_End{End: &addressingv1.StreamEnd{}}}); err != nil {
		return err
	}
	s.sendSeq++
	return s.raw.CloseSend()
}

func (s *RemoteStream) Recv() ([]byte, error) {
	frame, err := s.raw.Recv()
	if err != nil {
		s.finish()
		return nil, err
	}
	if frame.RequestId != s.requestID || frame.Sequence != s.recvSeq {
		s.finish()
		return nil, fmt.Errorf("流式响应序列非法 request=%s sequence=%d want=%d", frame.RequestId, frame.Sequence, s.recvSeq)
	}
	s.recvSeq++
	switch body := frame.Body.(type) {
	case *addressingv1.StreamFrame_Payload:
		return body.Payload.Data, nil
	case *addressingv1.StreamFrame_Result:
		s.resultMu.Lock()
		s.result = body.Result
		s.resultMu.Unlock()
		s.finish()
		return nil, io.EOF
	default:
		s.finish()
		return nil, errors.New("流式响应包含非法帧类型")
	}
}

func (s *RemoteStream) Result() (*contractv1.CallResult, []byte, error) {
	s.resultMu.RLock()
	defer s.resultMu.RUnlock()
	if s.result == nil {
		return nil, nil, errors.New("尚未收到流式最终结果")
	}
	if failure := s.result.TransportError; failure != nil {
		return nil, nil, &TransportError{Code: failure.Code, Message: failure.Message, Retryable: failure.Retryable}
	}
	if s.result.Result == nil {
		return nil, nil, errors.New("流式响应缺少 CallResult")
	}
	return s.result.Result, s.result.Payload, nil
}

func (s *RemoteStream) Cancel() {
	s.finish()
}

func (s *RemoteStream) finish() {
	s.close.Do(func() {
		s.cancel()
		_ = s.conn.Close()
	})
}

type ServerStream struct {
	raw       grpc.BidiStreamingServer[addressingv1.StreamFrame, addressingv1.StreamFrame]
	requestID string
	recvSeq   uint64
	sendMu    sync.Mutex
	sendSeq   uint64
}

func (s *ServerStream) Send(payload []byte) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	err := s.raw.Send(&addressingv1.StreamFrame{RequestId: s.requestID, Sequence: s.sendSeq, Body: &addressingv1.StreamFrame_Payload{Payload: &addressingv1.StreamPayload{Data: payload}}})
	if err == nil {
		s.sendSeq++
	}
	return err
}

func (s *ServerStream) Recv() ([]byte, error) {
	frame, err := s.raw.Recv()
	if err != nil {
		return nil, err
	}
	if frame.RequestId != s.requestID || frame.Sequence != s.recvSeq {
		return nil, fmt.Errorf("流式请求序列非法 request=%s sequence=%d want=%d", frame.RequestId, frame.Sequence, s.recvSeq)
	}
	s.recvSeq++
	switch body := frame.Body.(type) {
	case *addressingv1.StreamFrame_Payload:
		return body.Payload.Data, nil
	case *addressingv1.StreamFrame_End:
		return nil, io.EOF
	case *addressingv1.StreamFrame_Cancel:
		return nil, context.Canceled
	default:
		return nil, errors.New("流式请求包含非法帧类型")
	}
}

type capabilityStreamService struct {
	addressingv1.UnimplementedCapabilityStreamServer
	router *Router
}

func (service *capabilityStreamService) Open(raw grpc.BidiStreamingServer[addressingv1.StreamFrame, addressingv1.StreamFrame]) error {
	first, err := raw.Recv()
	if err != nil {
		return err
	}
	open := first.GetOpen()
	if first.Sequence != 0 || first.RequestId == "" || open == nil || open.Target == nil || open.Target.Capability == "" {
		return status.Error(codes.InvalidArgument, "流式起始帧非法")
	}
	service.router.mu.Lock()
	handlers := service.router.streamLocal[open.Target.Capability]
	if len(handlers) == 0 {
		service.router.mu.Unlock()
		return status.Error(codes.NotFound, "流式 capability 不存在")
	}
	cursor := service.router.streamCursor[open.Target.Capability]
	handler := handlers[cursor%uint64(len(handlers))].handler
	service.router.streamCursor[open.Target.Capability] = cursor + 1
	service.router.mu.Unlock()
	stream := &ServerStream{raw: raw, requestID: first.RequestId, recvSeq: 1}
	result, payload, handlerErr := handler(raw.Context(), open.Target, open.Context, open.InitialPayload, stream)
	response := &addressingv1.StreamResult{Result: result, Payload: payload}
	if handlerErr != nil {
		response.Result = nil
		response.Payload = nil
		response.TransportError = &addressingv1.TransportError{Code: errorcode.RemoteStreamFailed, Message: handlerErr.Error(), Retryable: true}
	} else if result == nil {
		response.TransportError = &addressingv1.TransportError{Code: errorcode.RemoteInvalidResponse, Message: "stream handler 未返回 CallResult"}
	}
	stream.sendMu.Lock()
	defer stream.sendMu.Unlock()
	return raw.Send(&addressingv1.StreamFrame{RequestId: first.RequestId, Sequence: stream.sendSeq, Body: &addressingv1.StreamFrame_Result{Result: response}})
}

func nowUTC() time.Time { return time.Now().UTC() }
