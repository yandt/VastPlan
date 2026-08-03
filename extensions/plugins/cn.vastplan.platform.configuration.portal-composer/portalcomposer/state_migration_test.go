package portalcomposer

import (
	"context"
	"testing"

	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func TestMigrateStateAcceptsOnlyAdditiveV4ToV5Transition(t *testing.T) {
	request := sdk.MigrationRequest{
		TransactionID: "migration-test",
		From:          sdk.StateIdentity{Format: composerStateFormat, FormatVersion: 4},
		To:            sdk.StateIdentity{Format: composerStateFormat, FormatVersion: 5},
	}
	for _, phase := range []sdk.MigrationPhase{sdk.MigrationPrepare, sdk.MigrationCommit, sdk.MigrationRollback} {
		if err := MigrateState(context.Background(), phase, request); err != nil {
			t.Fatalf("阶段 %s 必须接受兼容迁移: %v", phase, err)
		}
	}
	request.From.FormatVersion = 3
	if err := MigrateState(context.Background(), sdk.MigrationPrepare, request); err == nil {
		t.Fatal("未声明的状态来源必须拒绝")
	}
}
