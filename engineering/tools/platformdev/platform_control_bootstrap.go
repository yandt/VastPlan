package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"time"

	recoveryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/recovery/v1"
)

// completePlatformBootstrap owns the human-paced transition from the minimal
// Recovery tier to the complete platform and its independently published Portal.
func (r *runtime) completePlatformBootstrap(ctx context.Context, env map[string]string, natsURL string, startedAt time.Time) {
	complete := func() error {
		// A non-positive timeout leaves the first-time configuration wait governed
		// only by the parent process context.
		if err := r.waitForRecoveryStage(ctx, recoveryv1.StageControlPlane, startedAt, 0); err != nil {
			return fmt.Errorf("平台控制面未收敛: %w", err)
		}
		if err := r.waitForRecoveryStage(ctx, recoveryv1.StagePlatform, startedAt, 0); err != nil {
			return fmt.Errorf("平台完整能力未收敛: %w", err)
		}
		// Deployment already references this Seed candidate. Commit its LKG
		// before the independent Portal business publication.
		if err := r.commitSeedRuntimeSnapshot(); err != nil {
			return fmt.Errorf("提交已收敛的 Seed Runtime 快照: %w", err)
		}
		if err := r.publishInitialPortal(ctx, env, natsURL); err != nil {
			return fmt.Errorf("显式发布初始 Portal 组合: %w", err)
		}
		return nil
	}
	if err := complete(); err != nil {
		if ctx.Err() != nil {
			return
		}
		log.Printf("平台完整组合后台收敛失败，Bootstrap Portal 保持可用: %v", err)
		r.mu.Lock()
		r.platformPhase, r.platformError = "bootstrap-recovery", err.Error()
		r.mu.Unlock()
		return
	}
	r.mu.Lock()
	r.platformPhase, r.platformError = "platform-ready", ""
	r.mu.Unlock()
	log.Printf("平台控制数据库与完整 Seed 组合已就绪，Portal Activation 已提交")
}

func (r *runtime) platformControlProfilePath() string {
	return filepath.Join(r.persistentStateRoot(), "bootstrap", "platform-control.json")
}

func (r *runtime) platformControlCredentialsDirectory() string {
	return filepath.Join(r.persistentStateRoot(), "bootstrap", "credentials")
}
