package controller

import (
	"context"
	"time"

	pluginhostv1 "cdsoft.com.cn/VastPlan/core/shared/go/pluginhost/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (c *Controller) Lifecycle() sdk.LifecycleHandler {
	return func(_ context.Context, lifecycle *pluginhostv1.Lifecycle) error {
		switch lifecycle.GetOp() {
		case pluginhostv1.Lifecycle_OP_ACTIVATE:
			c.start()
		case pluginhostv1.Lifecycle_OP_DEACTIVATE, pluginhostv1.Lifecycle_OP_DRAIN, pluginhostv1.Lifecycle_OP_SHUTDOWN:
			c.stop()
		}
		return nil
	}
}

func (c *Controller) start() {
	c.cancelMu.Lock()
	defer c.cancelMu.Unlock()
	if c.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	go c.loop(ctx)
}

func (c *Controller) stop() {
	c.cancelMu.Lock()
	cancel := c.cancel
	c.cancel = nil
	c.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *Controller) loop(ctx context.Context) {
	initial := time.NewTimer(250 * time.Millisecond)
	defer initial.Stop()
	select {
	case <-ctx.Done():
		return
	case <-initial.C:
	}
	_, _ = c.ReconcileOnce(ctx)
	ticker := time.NewTicker(c.config.Interval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = c.ReconcileOnce(ctx)
		}
	}
}
