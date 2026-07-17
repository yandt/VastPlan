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
	"google.golang.org/protobuf/proto"

	addressingv1 "cdsoft.com.cn/VastPlan/shared/go/addressing/v1"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/shared/go/servicemodel"
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
	limits := r.Limits.Normalize()
	options = append(options,
		grpc.MaxRecvMsgSize(limits.MaxMessageBytes()),
		grpc.MaxSendMsgSize(limits.MaxMessageBytes()),
		grpc.MaxHeaderListSize(limits.MaxMetadataBytes),
		grpc.MaxConcurrentStreams(uint32(limits.MaxConcurrentCalls)),
	)
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
	policy := servicemodel.Normalize(servicemodel.Policy{
		InstancePolicy: options.InstancePolicy, StateModel: options.StateModel,
		Visibility: options.Visibility, Routing: options.Routing,
	})
	if err := servicemodel.Validate(policy); err != nil {
		return nil, fmt.Errorf("stream capability %s 运行策略无效: %w", options.Capability, err)
	}
	if policy.Visibility == servicemodel.VisibilityLocal {
		return nil, errors.New("local capability 不能注册到全局 stream 目录")
	}
	options.InstancePolicy, options.StateModel = policy.InstancePolicy, policy.StateModel
	options.Visibility, options.Routing = policy.Visibility, policy.Routing
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
		ServiceRole: options.ServiceRole, LogicalService: options.LogicalService,
		InstancePolicy: options.InstancePolicy, StateModel: options.StateModel,
		Visibility: options.Visibility, Routing: options.Routing,
		InstanceID: options.InstanceID, NodeID: r.NodeID,
		UnitID: options.UnitID, Version: options.Version, Subject: controlplane.RPCSubject(options.Capability),
		StreamEndpoint: endpoint, Health: "healthy", UpdatedAt: nowUTC(),
	}
	key := controlplane.CapabilityKey(options.Capability, options.InstanceID)
	if err := r.putAnnouncement(ctx, r.Directory, key, record); err != nil {
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

	sendMu     sync.Mutex
	sendSeq    uint64
	recvSeq    uint64
	resultMu   sync.RWMutex
	result     *addressingv1.StreamResult
	close      sync.Once
	maxFrame   int
	maxPayload int
	release    func()
}

func (r *Router) InvokeStream(ctx context.Context, target *contractv1.CallTarget, callCtx *contractv1.CallContext, initialPayload []byte) (*RemoteStream, error) {
	if target == nil || target.Capability == "" {
		return nil, errors.New("流式调用目标 capability 不能为空")
	}
	limits := r.Limits.Normalize()
	if !limits.PayloadAllowed(initialPayload) {
		return nil, &TransportError{Code: errorcode.PayloadTooLarge,
			Message: fmt.Sprintf("流式初始 payload 为 %d bytes，超过上限 %d bytes", len(initialPayload), limits.MaxPayloadBytes)}
	}
	if !limits.MetadataAllowed(proto.Size(callCtx)) {
		return nil, &TransportError{Code: errorcode.MetadataTooLarge, Message: "流式 CallContext 超过 metadata 上限"}
	}
	if !r.enterOutboundCall() {
		return nil, &TransportError{Code: errorcode.ConcurrencyLimited, Message: "addressing 流式调用并发达到上限", Retryable: true}
	}
	releaseCall := true
	defer func() {
		if releaseCall {
			r.leaveOutboundCall()
		}
	}()
	ctx, callCtx, deadlineCancel := r.boundedCallContext(ctx, callCtx)
	instances := r.Instances(target.Capability)
	streamInstances := instances[:0]
	for _, instance := range instances {
		if instance.StreamEndpoint != "" {
			streamInstances = append(streamInstances, instance)
		}
	}
	if len(streamInstances) == 0 {
		deadlineCancel()
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
		deadlineCancel()
		return nil, errors.New("流式客户端未配置 TLS credentials")
	}
	conn, err := grpc.NewClient(streamInstances[index].StreamEndpoint,
		grpc.WithTransportCredentials(dialCreds),
		grpc.WithMaxHeaderListSize(limits.MaxMetadataBytes),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(limits.MaxMessageBytes()),
			grpc.MaxCallSendMsgSize(limits.MaxMessageBytes()),
		),
	)
	if err != nil {
		deadlineCancel()
		return nil, fmt.Errorf("连接流式端点: %w", err)
	}
	streamCtx, cancelStream := context.WithCancel(ctx)
	cancel := func() {
		cancelStream()
		deadlineCancel()
	}
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
	releaseCall = false
	remote := &RemoteStream{
		requestID: requestID, raw: raw, conn: conn, cancel: cancel, sendSeq: 1,
		maxFrame: limits.MaxStreamFrameBytes, maxPayload: limits.MaxPayloadBytes, release: r.leaveOutboundCall,
	}
	// 调用方即使忘记 Recv/Cancel，统一 deadline 到期也会关闭连接并归还并发槽。
	context.AfterFunc(streamCtx, remote.finish)
	return remote, nil
}

func (s *RemoteStream) Send(payload []byte) error {
	return sendStreamPayload(s.raw, &s.sendMu, &s.sendSeq, s.requestID, s.maxFrame, "请求", payload)
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
		if len(body.Payload.Data) > s.maxFrame {
			s.finish()
			return nil, fmt.Errorf("流式响应帧为 %d bytes，超过上限 %d bytes", len(body.Payload.Data), s.maxFrame)
		}
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
	if len(s.result.Payload) > s.maxPayload {
		return nil, nil, &TransportError{Code: errorcode.PayloadTooLarge, Message: "流式最终 payload 超过上限"}
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
		if s.release != nil {
			s.release()
		}
	})
}

type ServerStream struct {
	raw       grpc.BidiStreamingServer[addressingv1.StreamFrame, addressingv1.StreamFrame]
	requestID string
	recvSeq   uint64
	sendMu    sync.Mutex
	sendSeq   uint64
	maxFrame  int
}

func (s *ServerStream) Send(payload []byte) error {
	return sendStreamPayload(s.raw, &s.sendMu, &s.sendSeq, s.requestID, s.maxFrame, "响应", payload)
}

type streamFrameSender interface {
	Send(*addressingv1.StreamFrame) error
}

func sendStreamPayload(sender streamFrameSender, mu *sync.Mutex, sequence *uint64, requestID string, maxFrame int, direction string, payload []byte) error {
	if len(payload) > maxFrame {
		return fmt.Errorf("流式%s帧为 %d bytes，超过上限 %d bytes", direction, len(payload), maxFrame)
	}
	mu.Lock()
	defer mu.Unlock()
	err := sender.Send(&addressingv1.StreamFrame{RequestId: requestID, Sequence: *sequence, Body: &addressingv1.StreamFrame_Payload{Payload: &addressingv1.StreamPayload{Data: payload}}})
	if err == nil {
		*sequence = *sequence + 1
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
		if len(body.Payload.Data) > s.maxFrame {
			return nil, fmt.Errorf("流式请求帧为 %d bytes，超过上限 %d bytes", len(body.Payload.Data), s.maxFrame)
		}
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
	limits := service.router.Limits.Normalize()
	if !limits.PayloadAllowed(open.InitialPayload) {
		return status.Error(codes.ResourceExhausted, "流式初始 payload 超过上限")
	}
	if !limits.MetadataAllowed(proto.Size(open.Context)) {
		return status.Error(codes.ResourceExhausted, "流式 CallContext 超过 metadata 上限")
	}
	if !service.router.enterHandlerCall() {
		return status.Error(codes.ResourceExhausted, "addressing 流式 handler 并发达到上限")
	}
	defer service.router.leaveHandlerCall()
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
	handlerCtx, boundedCallCtx, cancel := service.router.boundedCallContext(raw.Context(), open.Context)
	defer cancel()
	stream := &ServerStream{raw: raw, requestID: first.RequestId, recvSeq: 1, maxFrame: limits.MaxStreamFrameBytes}
	result, payload, handlerErr := handler(handlerCtx, open.Target, boundedCallCtx, open.InitialPayload, stream)
	response := &addressingv1.StreamResult{Result: result, Payload: payload}
	if handlerErr != nil {
		response.Result = nil
		response.Payload = nil
		response.TransportError = &addressingv1.TransportError{Code: errorcode.RemoteStreamFailed, Message: handlerErr.Error(), Retryable: true}
	} else if result == nil {
		response.TransportError = &addressingv1.TransportError{Code: errorcode.RemoteInvalidResponse, Message: "stream handler 未返回 CallResult"}
	} else if !limits.PayloadAllowed(payload) {
		response.Result = nil
		response.Payload = nil
		response.TransportError = &addressingv1.TransportError{Code: errorcode.PayloadTooLarge, Message: "stream handler 最终 payload 超过上限"}
	}
	stream.sendMu.Lock()
	defer stream.sendMu.Unlock()
	return raw.Send(&addressingv1.StreamFrame{RequestId: first.RequestId, Sequence: stream.sendSeq, Body: &addressingv1.StreamFrame_Result{Result: response}})
}

func nowUTC() time.Time { return time.Now().UTC() }
