package protocolbus

import (
	"context"
	"errors"
	"fmt"

	pluginhostv1 "cdsoft.com.cn/VastPlan/core/shared/go/pluginhost/v1"
)

func (h *Host) lifecycle(ctx context.Context, sess *session, op pluginhostv1.Lifecycle_Op) (*pluginhostv1.LifecycleAck, error) {
	return h.lifecycleRequest(ctx, sess, &pluginhostv1.Lifecycle{Op: op})
}

// Migrate 向指定候选进程发送状态迁移事务阶段。调用方只可在候选尚未取得路由
// 所有权时调用；任一阶段拒绝都会返回错误，由 Runtime 负责逆序 rollback。
func (h *Host) Migrate(ctx context.Context, process *PluginInstance, request MigrationCommand) error {
	if process != nil && process.embedded != nil {
		op, err := migrationLifecycleOp(request.Operation)
		if err != nil {
			return err
		}
		if request.TransactionID == "" || request.From.Format == "" || request.From.FormatVersion <= 0 ||
			request.To.Format == "" || request.To.FormatVersion <= 0 {
			return errors.New("状态迁移请求字段不完整")
		}
		h.mu.RLock()
		owned := h.embedded[process.SessionID] == process.embedded
		h.mu.RUnlock()
		if !owned {
			return errors.New("迁移目标内嵌实例不属于当前宿主")
		}
		return process.embedded.lifecycle(ctx, op, &request)
	}
	if process == nil || process.session == nil {
		return errors.New("迁移目标插件进程无有效会话")
	}
	op, err := migrationLifecycleOp(request.Operation)
	if err != nil {
		return err
	}
	if request.TransactionID == "" || request.From.Format == "" || request.From.FormatVersion <= 0 ||
		request.To.Format == "" || request.To.FormatVersion <= 0 {
		return errors.New("状态迁移请求字段不完整")
	}
	h.mu.RLock()
	sess, ok := h.sessions[process.SessionID]
	h.mu.RUnlock()
	if !ok || sess != process.session {
		return errors.New("迁移目标插件会话不属于当前宿主")
	}
	ack, err := h.lifecycleRequest(ctx, sess, &pluginhostv1.Lifecycle{
		Op: op, TransactionId: request.TransactionID,
		FromStateFormat: request.From.Format, FromStateVersion: request.From.FormatVersion,
		ToStateFormat: request.To.Format, ToStateVersion: request.To.FormatVersion,
	})
	if err != nil {
		return err
	}
	if !ack.Ready {
		message := "插件拒绝状态迁移"
		if ack.Message != nil && *ack.Message != "" {
			message += ": " + *ack.Message
		}
		return errors.New(message)
	}
	return nil
}

func migrationLifecycleOp(operation MigrationOperation) (pluginhostv1.Lifecycle_Op, error) {
	switch operation {
	case MigrationPrepare:
		return pluginhostv1.Lifecycle_OP_MIGRATION_PREPARE, nil
	case MigrationCommit:
		return pluginhostv1.Lifecycle_OP_MIGRATION_COMMIT, nil
	case MigrationRollback:
		return pluginhostv1.Lifecycle_OP_MIGRATION_ROLLBACK, nil
	default:
		return pluginhostv1.Lifecycle_OP_UNSPECIFIED, fmt.Errorf("未知状态迁移阶段 %q", operation)
	}
}

func (h *Host) lifecycleRequest(ctx context.Context, sess *session, request *pluginhostv1.Lifecycle) (*pluginhostv1.LifecycleAck, error) {
	timeout := h.callTimeout()
	if request.Op == pluginhostv1.Lifecycle_OP_DEACTIVATE || request.Op == pluginhostv1.Lifecycle_OP_DRAIN || request.Op == pluginhostv1.Lifecycle_OP_SHUTDOWN {
		sess.autonomousActive.Store(false)
	}
	if request.Op == pluginhostv1.Lifecycle_OP_DRAIN || request.Op == pluginhostv1.Lifecycle_OP_SHUTDOWN {
		timeout = h.limits().DrainTimeout
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	reqID := sess.nextRequestID()
	request.RequestId = reqID
	ch, err := sess.await(reqID, h.limits().MaxPendingRequests)
	if err != nil {
		return nil, err
	}
	defer sess.release(reqID)

	if err := sess.send(&pluginhostv1.FromHost{
		Msg: &pluginhostv1.FromHost_Lifecycle{
			Lifecycle: request,
		},
	}); err != nil {
		return nil, err
	}

	select {
	case msg, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("插件已失联: %w", sess.err())
		}
		return msg.GetLifecycleAck(), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// dispatch 按类型分发插件发来的消息。
