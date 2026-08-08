package runtimehost

import (
	"context"
	"errors"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

type preparedMigration struct {
	plan     StateMigrationPlan
	instance *protocolbus.PluginInstance
}

func prepareMigrations(ctx context.Context, host *protocolbus.Host, plans []StateMigrationPlan, instances []*protocolbus.PluginInstance) ([]preparedMigration, error) {
	byPlugin := make(map[string]*protocolbus.PluginInstance, len(instances))
	for _, instance := range instances {
		byPlugin[instance.PluginID] = instance
	}
	prepared := make([]preparedMigration, 0, len(plans))
	for _, plan := range plans {
		instance := byPlugin[plan.PluginID]
		if instance == nil {
			err := &StateMigrationError{PluginID: plan.PluginID, Phase: "prepare", Err: errors.New("迁移计划引用未启动的候选插件")}
			return nil, errors.Join(err, rollbackMigrations(host, prepared, nil))
		}
		migration := preparedMigration{plan: plan, instance: instance}
		// 即使 PREPARE 返回错误，也可能已经产生了部分候选状态；先登记再调用，
		// 失败路径才能把本插件一并纳入逆序 ROLLBACK。
		prepared = append(prepared, migration)
		if err := host.Migrate(ctx, instance, migrationRequest(plan, protocolbus.MigrationPrepare)); err != nil {
			prepareErr := &StateMigrationError{PluginID: plan.PluginID, Phase: "prepare", Err: err}
			return nil, errors.Join(prepareErr, rollbackMigrations(host, prepared, nil))
		}
	}
	return prepared, nil
}

func migrationRequest(plan StateMigrationPlan, operation protocolbus.MigrationOperation) protocolbus.MigrationCommand {
	return protocolbus.MigrationCommand{
		Operation: operation, TransactionID: plan.TransactionID,
		From: plan.From.ContractIdentity(),
		To:   plan.To.ContractIdentity(),
	}
}

func rollbackMigrations(host *protocolbus.Host, prepared []preparedMigration, logf func(string, ...any)) error {
	var rollbackErr error
	for index := len(prepared) - 1; index >= 0; index-- {
		migration := prepared[index]
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := host.Migrate(ctx, migration.instance, migrationRequest(migration.plan, protocolbus.MigrationRollback))
		cancel()
		if err != nil && logf != nil {
			logf("回滚插件 %s 状态迁移失败 transaction=%s: %v",
				migration.plan.PluginID, migration.plan.TransactionID, err)
		}
		if err != nil {
			rollbackErr = errors.Join(rollbackErr, &StateMigrationError{
				PluginID: migration.plan.PluginID, Phase: "rollback", Err: err,
			})
		}
	}
	return rollbackErr
}
