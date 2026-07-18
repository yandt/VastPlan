package addressing

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	addressingv1 "cdsoft.com.cn/VastPlan/core/shared/go/addressing/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/errorcode"
)

var durableNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// PersistentSubscriptionOptions 定义一个可断点续消费的持久事件订阅。
// 同一 durable 代表同一个逻辑消费方，关闭本地进程不会删除服务端游标。
type PersistentSubscriptionOptions struct {
	Durable       string
	EventType     string
	AckWait       time.Duration
	RetryDelay    time.Duration
	MaxDeliver    int
	MaxAckPending int
}

// PersistentSubscription 只停止当前拉取循环，故意保留 durable consumer。
type PersistentSubscription struct {
	consume jetstream.ConsumeContext
}

func (s *PersistentSubscription) Close() {
	if s != nil && s.consume != nil {
		s.consume.Stop()
		<-s.consume.Closed()
	}
}

// PublishPersistent 把领域事件写入 JetStream，并以事件 ID 作为去重键。
// 调用成功表示服务端已经确认写入，不只是进入客户端发送缓冲区。
func (r *Router) PublishPersistent(ctx context.Context, callCtx *contractv1.CallContext, event *contractv1.CallEvent) error {
	if event == nil || event.Type == "" {
		return errors.New("持久事件 type 不能为空")
	}
	if event.Id == "" {
		return errors.New("持久事件 id 不能为空")
	}
	limits := r.Limits.Normalize()
	if !limits.MetadataAllowed(proto.Size(callCtx)) {
		return &TransportError{Code: errorcode.MetadataTooLarge, Message: "持久事件 CallContext 超过 metadata 上限"}
	}
	if !limits.PayloadAllowed(event.Payload) {
		return &TransportError{Code: errorcode.PayloadTooLarge, Message: "持久事件 payload 超过上限"}
	}
	raw, err := proto.Marshal(&addressingv1.EventEnvelope{Context: callCtx, Event: event})
	if err != nil {
		return fmt.Errorf("编码持久事件: %w", err)
	}
	subject := controlplane.PersistentEventSubject(event.Type)
	message := nats.NewMsg(subject)
	message.Data = raw
	if r.Transport != nil {
		if err := r.Transport.signMessage(message); err != nil {
			return err
		}
	}
	ack, err := r.JetStream.PublishMsg(ctx, message, jetstream.WithMsgID(event.Id))
	if err != nil {
		return fmt.Errorf("发布持久事件 %s: %w", event.Type, err)
	}
	if ack.Stream != controlplane.EventsStream {
		return fmt.Errorf("持久事件写入了非预期 stream %q", ack.Stream)
	}
	return nil
}

// SubscribePersistent 使用显式 ACK 提供至少一次投递。handler 成功后才 ACK；
// 失败会延迟重投。MaxAckPending 默认为 1，以保证同一 durable 内严格按序处理。
func (r *Router) SubscribePersistent(ctx context.Context, options PersistentSubscriptionOptions, handler EventHandler) (*PersistentSubscription, error) {
	if !durableNamePattern.MatchString(options.Durable) {
		return nil, errors.New("durable 只能包含 1-64 个字母、数字、下划线或连字符")
	}
	if options.EventType == "" || handler == nil {
		return nil, errors.New("持久事件类型和 handler 不能为空")
	}
	if options.AckWait <= 0 {
		options.AckWait = 30 * time.Second
	}
	if options.RetryDelay <= 0 {
		options.RetryDelay = time.Second
	}
	if options.MaxDeliver <= 0 {
		options.MaxDeliver = 5
	}
	if options.MaxAckPending <= 0 {
		options.MaxAckPending = 1
	}
	filter := controlplane.PersistentEventSubject(options.EventType)
	if options.EventType == ">" {
		filter = "vp.event.persist.v1.>"
	}
	consumer, err := r.Events.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name: options.Durable, Durable: options.Durable,
		AckPolicy: jetstream.AckExplicitPolicy, AckWait: options.AckWait,
		MaxDeliver: options.MaxDeliver, MaxAckPending: options.MaxAckPending,
		FilterSubject: filter, DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("创建持久事件订阅 %s: %w", options.Durable, err)
	}
	consume, err := consumer.Consume(func(msg jetstream.Msg) {
		if len(msg.Data()) > r.Limits.Normalize().MaxMessageBytes() {
			_ = msg.Term()
			return
		}
		var identity TransportIdentity
		if r.Transport != nil {
			var verifyErr error
			identity, verifyErr = r.Transport.verifyNoReplay(msg.Subject(), msg.Data(), transportHeaderValues(msg.Headers()))
			if verifyErr != nil {
				r.Logf("持久事件传输身份校验失败 subject=%s: %v", msg.Subject(), verifyErr)
				_ = msg.Term()
				return
			}
		}
		var envelope addressingv1.EventEnvelope
		if err := proto.Unmarshal(msg.Data(), &envelope); err != nil || envelope.Event == nil {
			r.Logf("终止非法持久事件 subject=%s: %v", msg.Subject(), err)
			_ = msg.Term()
			return
		}
		if controlplane.PersistentEventSubject(envelope.Event.Type) != msg.Subject() {
			r.Logf("持久事件 type 与 subject 不一致 subject=%s type=%s", msg.Subject(), envelope.Event.Type)
			_ = msg.Term()
			return
		}
		limits := r.Limits.Normalize()
		if !limits.MetadataAllowed(proto.Size(envelope.Context)) || !limits.PayloadAllowed(envelope.Event.Payload) {
			_ = msg.Term()
			return
		}
		if r.Transport != nil {
			authenticated, authErr := authenticatedTransportContext(identity, envelope.Context)
			if authErr != nil {
				r.Logf("持久事件身份上下文校验失败 subject=%s: %v", msg.Subject(), authErr)
				_ = msg.Term()
				return
			}
			envelope.Context = authenticated
		}
		if err := handler(r.ctx, envelope.Context, envelope.Event); err != nil {
			r.Logf("持久事件 handler 失败 type=%s: %v", envelope.Event.Type, err)
			_ = msg.NakWithDelay(options.RetryDelay)
			return
		}
		if err := msg.Ack(); err != nil {
			r.Logf("确认持久事件失败 type=%s id=%s: %v", envelope.Event.Type, envelope.Event.Id, err)
		}
	}, jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
		r.Logf("持久事件消费循环异常 durable=%s: %v", options.Durable, err)
	}))
	if err != nil {
		return nil, fmt.Errorf("启动持久事件订阅 %s: %w", options.Durable, err)
	}
	return &PersistentSubscription{consume: consume}, nil
}
