package databaseruntime

import (
	"context"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/recordstore"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) executeSchemaController(ctx context.Context, host sdk.Host, call *contractv1.CallContext,
	operation string, ref databasev1.ConnectionRef, entry recordstore.ModelEntry) (*contractv1.CallResult, []byte, error) {
	if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_SYSTEM {
		return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("Schema Controller 只允许可信系统调用")))
	}
	if operation == recordstorev1.OperationSchemaApply && !hasSchemaControllerEvidence(call, ref) {
		return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("Schema Controller 缺少当前 leader evidence")))
	}
	lease, err := s.acquire(ctx, host, call, ref)
	if err != nil {
		return recordResult(nil, err)
	}
	defer lease.Release()
	dialect, err := recordstore.DialectFor(lease.ProviderID())
	if err != nil {
		return recordResult(nil, err)
	}
	var value any
	err = lease.WithPinned(ctx, func(pinned PinnedSession) error {
		switch operation {
		case recordstorev1.OperationSchemaPlan:
			state, readErr := recordstore.ReadSchemaState(ctx, pinned, dialect, entry.Model.ID)
			if readErr != nil {
				return readErr
			}
			plan, planErr := schemaPlan(dialect, state, entry)
			value = recordstorev1.SchemaPlanResult{Kind: string(plan.Kind), Statements: len(plan.Statements), Reasons: append([]string(nil), plan.Reasons...)}
			return planErr
		case recordstorev1.OperationSchemaStatus:
			state, readErr := recordstore.ReadSchemaState(ctx, pinned, dialect, entry.Model.ID)
			if readErr != nil {
				return readErr
			}
			status := recordstorev1.SchemaStatusResult{}
			if state != nil {
				status.SchemaVersion, status.SHA256 = state.Version, state.SHA256
				status.Ready = state.Version == entry.Model.SchemaVersion && state.SHA256 == entry.Ref.SHA256
			}
			value = status
			return nil
		case recordstorev1.OperationSchemaApply:
			plan, applyErr := applySchema(ctx, pinned, dialect, entry)
			value = recordstorev1.SchemaPlanResult{Kind: string(plan.Kind), Statements: len(plan.Statements), Reasons: append([]string(nil), plan.Reasons...)}
			return applyErr
		default:
			return errors.New("Schema Controller 操作无效")
		}
	})
	return recordResult(value, err)
}

func hasSchemaControllerEvidence(call *contractv1.CallContext, ref databasev1.ConnectionRef) bool {
	expected := "database.schema-controller/" + ref.ResourceID
	for _, credential := range call.GetCredentials() {
		if credential.GetName() == expected && credential.GetScope() == "service" {
			return true
		}
	}
	return false
}

func schemaPlan(dialect recordstore.Dialect, state *recordstore.SchemaState, entry recordstore.ModelEntry) (recordstore.MigrationPlan, error) {
	if state != nil && state.Version == entry.Model.SchemaVersion && state.SHA256 == entry.Ref.SHA256 {
		return recordstore.MigrationPlan{Kind: recordstore.MigrationNone}, nil
	}
	if state != nil && state.Version >= entry.Model.SchemaVersion {
		return recordstore.MigrationPlan{Kind: recordstore.MigrationManual, Reasons: []string{"迁移账本版本或摘要与候选不一致"}}, nil
	}
	var previous *datamodelv1.Model
	if state != nil {
		previous = &state.Document
	}
	return recordstore.PlanMigration(dialect, previous, entry.Model)
}

func applySchema(ctx context.Context, pinned PinnedSession, dialect recordstore.Dialect, entry recordstore.ModelEntry) (recordstore.MigrationPlan, error) {
	if dialect.ProviderID() == "postgresql" {
		transaction, err := pinned.Begin(ctx, databasev1.TransactionOptions{Isolation: "serializable", TimeoutMS: 60_000})
		if err != nil {
			return recordstore.MigrationPlan{}, err
		}
		plan, err := applySchemaSession(ctx, transaction, dialect, entry, true)
		if err == nil {
			err = transaction.Commit(ctx)
		} else {
			_ = transaction.Rollback(context.Background())
		}
		return plan, err
	}
	lock, err := pinned.Query(ctx, recordstore.SchemaLockStatement(dialect), 1)
	if err != nil {
		return recordstore.MigrationPlan{}, err
	}
	if err := recordstore.VerifyLockResult(lock); err != nil {
		return recordstore.MigrationPlan{}, err
	}
	defer pinned.Query(context.Background(), recordstore.SchemaUnlockStatement(dialect), 1)
	return applySchemaSession(ctx, pinned, dialect, entry, false)
}

func applySchemaSession(ctx context.Context, session recordstore.Session, dialect recordstore.Dialect,
	entry recordstore.ModelEntry, lockInTransaction bool) (recordstore.MigrationPlan, error) {
	if lockInTransaction {
		lock, err := session.Query(ctx, recordstore.SchemaLockStatement(dialect), 1)
		if err != nil {
			return recordstore.MigrationPlan{}, err
		}
		if err := recordstore.VerifyLockResult(lock); err != nil {
			return recordstore.MigrationPlan{}, err
		}
	}
	for _, statement := range recordstore.InternalSchemaStatements(dialect) {
		if _, err := session.Execute(ctx, statement); err != nil {
			return recordstore.MigrationPlan{}, err
		}
	}
	state, err := recordstore.ReadSchemaState(ctx, session, dialect, entry.Model.ID)
	if err != nil {
		return recordstore.MigrationPlan{}, err
	}
	plan, err := schemaPlan(dialect, state, entry)
	if err != nil || plan.Kind == recordstore.MigrationManual {
		if err == nil {
			err = recordstore.ErrMigrationNeeded
		}
		return plan, err
	}
	if plan.Kind == recordstore.MigrationNone {
		return plan, nil
	}
	statements := append([]databasev1.Statement(nil), plan.Statements...)
	if plan.Kind == recordstore.MigrationCreate {
		statements = append(statements, recordstore.IndexStatements(dialect, entry.Model)...)
	}
	for _, statement := range statements {
		if _, err := session.Execute(ctx, statement); err != nil {
			return plan, err
		}
	}
	ledger, err := recordstore.SchemaLedgerInsert(dialect, entry)
	if err != nil {
		return plan, err
	}
	if result, err := session.Execute(ctx, ledger); err != nil || result.RowsAffected != 1 {
		if err == nil {
			err = recordstore.ErrConflict
		}
		return plan, err
	}
	return plan, nil
}
