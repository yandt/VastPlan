package databaseruntime

import (
	"context"
	"errors"
	"fmt"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/recordstore"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) executeSchemaController(ctx context.Context, host sdk.Host, call *contractv1.CallContext,
	operation string, ref databasev1.ConnectionRef, entry recordstore.ModelEntry, request *recordstorev1.SchemaRequest) (*contractv1.CallResult, []byte, error) {
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
	return s.executeSchemaWithPinned(ctx, call, operation, ref, entry, request, dialect, lease.WithPinned)
}

func (s *Service) executePlatformSchemaController(ctx context.Context, call *contractv1.CallContext,
	operation string, ref databasev1.ConnectionRef, snapshot platformRecordSnapshot, entry recordstore.ModelEntry,
	request *recordstorev1.SchemaRequest) (*contractv1.CallResult, []byte, error) {
	dialect, err := recordstore.DialectFor(snapshot.store.ProviderID())
	if err != nil {
		return recordResult(nil, err)
	}
	return s.executeSchemaWithPinned(ctx, call, operation, ref, entry, request, dialect, snapshot.store.WithPinned)
}

// PreparePlatformModels applies only migrations classified as safe by the
// signed DataModel policy before a Platform Control candidate is bound. Manual
// or destructive changes remain fail-closed and require the normal evidence
// carrying Schema Controller workflow.
func (s *Service) PreparePlatformModels(ctx context.Context, store PlatformRecordStore) error {
	if s == nil || store == nil {
		return recordstore.ErrStorageUnavailable
	}
	dialect, err := recordstore.DialectFor(store.ProviderID())
	if err != nil {
		return err
	}
	for _, entry := range s.recordModels.PlatformModels() {
		err = store.WithPinned(ctx, func(pinned PinnedSession) error {
			_, applyErr := s.applySchema(ctx, pinned, dialect, entry, "", nil)
			return applyErr
		})
		if err != nil {
			return fmt.Errorf("准备 Platform Control DataModel %s: %w", entry.Model.ID, err)
		}
	}
	return nil
}

func (s *Service) executeSchemaWithPinned(ctx context.Context, call *contractv1.CallContext,
	operation string, ref databasev1.ConnectionRef, entry recordstore.ModelEntry, request *recordstorev1.SchemaRequest,
	dialect recordstore.Dialect, withPinned func(context.Context, func(PinnedSession) error) error) (*contractv1.CallResult, []byte, error) {
	if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_SYSTEM {
		return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("Schema Controller 只允许可信系统调用")))
	}
	if operation == recordstorev1.OperationSchemaApply && !hasSchemaControllerEvidence(call, ref) {
		return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("Schema Controller 缺少当前 leader evidence")))
	}
	var value any
	err := withPinned(ctx, func(pinned PinnedSession) error {
		switch operation {
		case recordstorev1.OperationSchemaPlan:
			state, readErr := recordstore.ReadSchemaState(ctx, pinned, dialect, entry.Model.ID)
			if readErr != nil {
				return readErr
			}
			plan, planErr := s.resolveSchemaPlan(dialect, state, entry, request.MigrationID)
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
			plan, applyErr := s.applySchema(ctx, pinned, dialect, entry, request.MigrationID, func(migrationID string) bool {
				return hasSignedMigrationEvidence(call, ref, entry.Model.ID, migrationID)
			})
			value = recordstorev1.SchemaPlanResult{Kind: string(plan.Kind), Statements: len(plan.Statements), Reasons: append([]string(nil), plan.Reasons...)}
			return applyErr
		default:
			return errors.New("Schema Controller 操作无效")
		}
	})
	return recordResult(value, err)
}

func hasSignedMigrationEvidence(call *contractv1.CallContext, ref databasev1.ConnectionRef, modelID, migrationID string) bool {
	base := ref.ResourceID + "/" + modelID + "/" + migrationID
	required := map[string]bool{
		"database.schema-backup/" + base:   false,
		"database.schema-approval/" + base: false,
	}
	for _, credential := range call.GetCredentials() {
		if credential.GetScope() == "service" {
			if _, expected := required[credential.GetName()]; expected {
				required[credential.GetName()] = true
			}
		}
	}
	for _, present := range required {
		if !present {
			return false
		}
	}
	return true
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

func (s *Service) resolveSchemaPlan(dialect recordstore.Dialect, state *recordstore.SchemaState,
	entry recordstore.ModelEntry, migrationID string) (recordstore.MigrationPlan, error) {
	plan, err := schemaPlan(dialect, state, entry)
	if err != nil || plan.Kind != recordstore.MigrationManual || migrationID == "" {
		return plan, err
	}
	if state == nil {
		return plan, recordstore.ErrMigrationNeeded
	}
	migration, err := s.recordModels.ResolveMigration(entry, *state, migrationID)
	if err != nil {
		return plan, err
	}
	return recordstore.SignedMigrationPlan(dialect, migration)
}

func (s *Service) applySchema(ctx context.Context, pinned PinnedSession, dialect recordstore.Dialect,
	entry recordstore.ModelEntry, migrationID string, signedEvidence func(string) bool) (recordstore.MigrationPlan, error) {
	if dialect.ProviderID() == "postgresql" {
		transaction, err := pinned.Begin(ctx, databasev1.TransactionOptions{Isolation: "serializable", TimeoutMS: 60_000})
		if err != nil {
			return recordstore.MigrationPlan{}, err
		}
		plan, err := s.applySchemaSession(ctx, transaction, dialect, entry, true, migrationID, signedEvidence)
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
	return s.applySchemaSession(ctx, pinned, dialect, entry, false, migrationID, signedEvidence)
}

func (s *Service) applySchemaSession(ctx context.Context, session recordstore.Session, dialect recordstore.Dialect,
	entry recordstore.ModelEntry, lockInTransaction bool, migrationID string, signedEvidence func(string) bool) (recordstore.MigrationPlan, error) {
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
	plan, err := s.resolveSchemaPlan(dialect, state, entry, migrationID)
	if err != nil || plan.Kind == recordstore.MigrationManual {
		if err == nil {
			err = recordstore.ErrMigrationNeeded
		}
		return plan, err
	}
	if plan.Kind == recordstore.MigrationNone {
		return plan, nil
	}
	if plan.Kind == recordstore.MigrationSigned && (signedEvidence == nil || !signedEvidence(plan.MigrationID)) {
		return plan, recordstore.ErrStorageDenied
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
	ledger, err := recordstore.SchemaLedgerInsert(dialect, entry, plan.MigrationID)
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
