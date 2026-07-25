package plugin

import (
	"context"
	"fmt"

	pluginhostv1 "cdsoft.com.cn/VastPlan/core/shared/go/pluginhost/v1"
)

func (p *Plugin) handleLifecycle(lc *pluginhostv1.Lifecycle) {
	ack := &pluginhostv1.LifecycleAck{RequestId: lc.RequestId, Ready: true}
	if p.lifecycle != nil {
		lifecycleCtx, cancel := context.WithTimeout(context.Background(), p.Limits.Normalize().DefaultDeadline)
		err := p.lifecycle(lifecycleCtx, lc)
		cancel()
		if err != nil {
			ack.Ready = false
			message := err.Error()
			ack.Message = &message
			_ = p.send(&pluginhostv1.FromPlugin{
				Msg: &pluginhostv1.FromPlugin_LifecycleAck{LifecycleAck: ack},
			})
			return
		}
	}

	switch lc.Op {
	case pluginhostv1.Lifecycle_OP_ACTIVATE:
		p.lifecycleMu.Lock()
		p.active = true
		p.lifecycleMu.Unlock()
	case pluginhostv1.Lifecycle_OP_DEACTIVATE:
		p.lifecycleMu.Lock()
		p.active = false
		p.lifecycleMu.Unlock()
	case pluginhostv1.Lifecycle_OP_DRAIN:
		p.lifecycleMu.Lock()
		p.active = false
		p.lifecycleMu.Unlock()
		p.inflight.Wait()
	case pluginhostv1.Lifecycle_OP_SHUTDOWN:
		p.lifecycleMu.Lock()
		p.active = false
		p.lifecycleMu.Unlock()
		p.inflight.Wait()
		p.shuttingDown.Store(true)
	case pluginhostv1.Lifecycle_OP_MIGRATION_PREPARE,
		pluginhostv1.Lifecycle_OP_MIGRATION_COMMIT,
		pluginhostv1.Lifecycle_OP_MIGRATION_ROLLBACK:
		if p.lifecycle != nil {
			break // trusted adapter already handled the full wire lifecycle
		}
		phase, err := migrationPhase(lc.Op)
		if err != nil {
			ack.Ready = false
			msg := err.Error()
			ack.Message = &msg
			break
		}
		if p.migration == nil {
			ack.Ready = false
			msg := "插件未实现清单声明的状态迁移处理器"
			ack.Message = &msg
			break
		}
		request := MigrationRequest{
			TransactionID: lc.TransactionId,
			From:          StateIdentity{Format: lc.FromStateFormat, FormatVersion: lc.FromStateVersion},
			To:            StateIdentity{Format: lc.ToStateFormat, FormatVersion: lc.ToStateVersion},
		}
		if request.TransactionID == "" || request.From.Format == "" || request.From.FormatVersion <= 0 ||
			request.To.Format == "" || request.To.FormatVersion <= 0 {
			ack.Ready = false
			msg := "状态迁移请求字段不完整"
			ack.Message = &msg
			break
		}
		migrationCtx, cancel := context.WithTimeout(context.Background(), p.Limits.Normalize().DefaultDeadline)
		err = p.migration(migrationCtx, phase, request)
		cancel()
		if err != nil {
			ack.Ready = false
			msg := err.Error()
			ack.Message = &msg
		}
	default:
		msg := "未知生命周期指令"
		ack.Ready, ack.Message = false, &msg
	}

	_ = p.send(&pluginhostv1.FromPlugin{
		Msg: &pluginhostv1.FromPlugin_LifecycleAck{LifecycleAck: ack},
	})
}

func migrationPhase(op pluginhostv1.Lifecycle_Op) (MigrationPhase, error) {
	switch op {
	case pluginhostv1.Lifecycle_OP_MIGRATION_PREPARE:
		return MigrationPrepare, nil
	case pluginhostv1.Lifecycle_OP_MIGRATION_COMMIT:
		return MigrationCommit, nil
	case pluginhostv1.Lifecycle_OP_MIGRATION_ROLLBACK:
		return MigrationRollback, nil
	default:
		return "", fmt.Errorf("生命周期指令 %s 不是迁移阶段", op)
	}
}

// Call 实现 Host：插件回调宿主（内核服务，或经 capability 寻址调别的插件）。
