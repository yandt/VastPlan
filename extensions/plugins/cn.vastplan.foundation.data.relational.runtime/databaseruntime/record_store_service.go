package databaseruntime

import (
	"context"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/recordstore"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) RecordContribution() sdk.Contribution {
	handlers := map[string]sdk.Handler{}
	for _, operation := range []string{
		recordstorev1.OperationSyncModels, recordstorev1.OperationCreate, recordstorev1.OperationGet,
		recordstorev1.OperationList, recordstorev1.OperationUpdate, recordstorev1.OperationDelete,
		recordstorev1.OperationBatch, recordstorev1.OperationBegin, recordstorev1.OperationCommit,
		recordstorev1.OperationRollback, recordstorev1.OperationAppendOutbox,
		recordstorev1.OperationSchemaPlan, recordstorev1.OperationSchemaApply, recordstorev1.OperationSchemaStatus,
	} {
		handlers[operation] = s.recordHandler(operation)
	}
	handlers["recordTransactionRelay"] = s.recordTransactionRelayHandler()
	return sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage,
		ID:             recordstorev1.Capability,
		Descriptor:     recordDescriptor(),
		Handlers:       handlers,
	}
}

func (s *Service) recordHandler(operation string) sdk.Handler {
	return func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		parsed, err := recordstorev1.ParseRequest(operation, payload)
		if err != nil {
			return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err))
		}
		if request, ok := parsed.(*recordstorev1.SyncModelsRequest); ok {
			if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_SYSTEM || !hasInventoryEvidence(call, request.InventoryDigest) {
				return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("只有持有已验证 Plugin Inventory 证据的系统组合器可以同步数据目录")))
			}
			value, syncErr := s.recordModels.Replace(*request)
			return recordResult(value, syncErr)
		}
		handle := recordTransactionHandle(parsed)
		if handle != "" {
			if result, raw, proxyErr := s.proxyRecordTransaction(ctx, host, call, operation, handle, payload); result != nil || proxyErr != nil {
				return result, raw, proxyErr
			}
		}
		return s.executeRecord(ctx, host, call, operation, parsed)
	}
}

func hasInventoryEvidence(call *contractv1.CallContext, inventoryDigest string) bool {
	expected := "plugin.inventory/" + inventoryDigest
	for _, credential := range call.GetCredentials() {
		if credential.GetName() == expected && credential.GetScope() == "service" {
			return true
		}
	}
	return false
}

func (s *Service) executeRecord(ctx context.Context, host sdk.Host, call *contractv1.CallContext, operation string, request any) (*contractv1.CallResult, []byte, error) {
	if operation == recordstorev1.OperationCommit || operation == recordstorev1.OperationRollback {
		return s.endRecordTransaction(ctx, call, operation, request.(*recordstorev1.EndRequest))
	}
	modelRef, storage, err := recordRequestIdentity(request)
	if err != nil {
		return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err))
	}
	entry, err := s.recordModels.Resolve(modelRef)
	if err != nil {
		return recordResult(nil, err)
	}
	if err := requireModelOwner(call, entry); err != nil {
		return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err))
	}
	ref, err := recordConnection(entry, storage)
	if err != nil {
		return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err))
	}
	if err := requireExecutor(call, ref); err != nil {
		return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err))
	}
	scope, err := requestScope(call, true)
	if err != nil {
		return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err))
	}
	trusted := recordstore.TrustedScope{TenantID: scope.TenantID, ServiceID: call.GetCaller().GetId(), ActorID: call.GetCaller().GetId()}
	identity := recordstore.ExecutionIdentity{OwnerPluginID: entry.OwnerPluginID, ModelID: entry.Model.ID, TenantID: scope.TenantID, ServiceID: trusted.ServiceID, CallerID: scope.CallerID}
	if operation == recordstorev1.OperationSchemaPlan || operation == recordstorev1.OperationSchemaApply || operation == recordstorev1.OperationSchemaStatus {
		return s.executeSchemaController(ctx, host, call, operation, ref, entry, request.(*recordstorev1.SchemaRequest))
	}
	if operation == recordstorev1.OperationBegin {
		return s.beginRecordTransaction(ctx, host, call, ref, entry, request.(*recordstorev1.BeginRequest))
	}
	handle := recordTransactionHandle(request)
	if handle != "" {
		return s.executeRecordTransaction(ctx, call, handle, ref, entry, operation, request, trusted, identity)
	}
	return s.executeRecordPool(ctx, host, call, ref, entry, operation, request, trusted, identity)
}

func (s *Service) beginRecordTransaction(ctx context.Context, host sdk.Host, call *contractv1.CallContext,
	ref databasev1.ConnectionRef, entry recordstore.ModelEntry, request *recordstorev1.BeginRequest) (*contractv1.CallResult, []byte, error) {
	lease, err := s.acquire(ctx, host, call, ref)
	if err != nil {
		return recordResult(nil, err)
	}
	dialect, err := recordstore.DialectFor(lease.ProviderID())
	if err == nil {
		err = s.recordEngine.CheckSchema(ctx, lease, dialect, entry)
	}
	if err != nil {
		lease.Release()
		return recordResult(nil, err)
	}
	value, err := s.transactions.Begin(ctx, call, ref, request.Options, lease)
	if err != nil {
		lease.Release()
	}
	return recordResult(value, err)
}

func (s *Service) endRecordTransaction(ctx context.Context, call *contractv1.CallContext, operation string,
	request *recordstorev1.EndRequest) (*contractv1.CallResult, []byte, error) {
	if err := requireTransactionCaller(call); err != nil {
		return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err))
	}
	if operation == recordstorev1.OperationCommit {
		ref, err := s.transactions.Connection(request.TransactionHandle, call)
		if err != nil {
			return recordResult(nil, err)
		}
		if err := requireExecutor(call, ref); err != nil {
			return recordResult(nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err))
		}
	}
	err := s.transactions.End(ctx, call, request.TransactionHandle, operation == recordstorev1.OperationCommit)
	return recordResult(struct{}{}, err)
}

func (s *Service) executeRecordTransaction(ctx context.Context, call *contractv1.CallContext, handle string,
	ref databasev1.ConnectionRef, entry recordstore.ModelEntry, operation string, request any,
	scope recordstore.TrustedScope, identity recordstore.ExecutionIdentity) (*contractv1.CallResult, []byte, error) {
	provider, err := s.transactions.ProviderID(handle, call)
	if err != nil {
		return recordResult(nil, err)
	}
	dialect, err := recordstore.DialectFor(provider)
	if err != nil {
		return recordResult(nil, err)
	}
	compiler, err := recordstore.NewCompiler(dialect, entry.Model)
	if err != nil {
		return recordResult(nil, err)
	}
	var value any
	err = s.transactions.WithTransaction(ctx, call, handle, ref, func(transaction Transaction) error {
		var runErr error
		value, runErr = s.runRecord(ctx, transaction, compiler, dialect, entry, operation, request, scope, identity)
		return runErr
	})
	return recordResult(value, err)
}

func (s *Service) executeRecordPool(ctx context.Context, host sdk.Host, call *contractv1.CallContext,
	ref databasev1.ConnectionRef, entry recordstore.ModelEntry, operation string, request any,
	scope recordstore.TrustedScope, identity recordstore.ExecutionIdentity) (*contractv1.CallResult, []byte, error) {
	lease, err := s.acquire(ctx, host, call, ref)
	if err != nil {
		return recordResult(nil, err)
	}
	dialect, err := recordstore.DialectFor(lease.ProviderID())
	if err != nil {
		lease.Release()
		return recordResult(nil, err)
	}
	compiler, err := recordstore.NewCompiler(dialect, entry.Model)
	if err != nil {
		lease.Release()
		return recordResult(nil, err)
	}
	if !recordWriteOperation(operation) {
		defer lease.Release()
		value, runErr := s.runRecord(ctx, lease, compiler, dialect, entry, operation, request, scope, identity)
		return recordResult(value, runErr)
	}
	transaction, err := lease.Begin(ctx, databasev1.TransactionOptions{Isolation: "serializable", TimeoutMS: 30_000})
	if err != nil {
		lease.Release()
		return recordResult(nil, err)
	}
	value, runErr := s.runRecord(ctx, transaction, compiler, dialect, entry, operation, request, scope, identity)
	if runErr == nil {
		runErr = transaction.Commit(ctx)
	} else {
		_ = transaction.Rollback(context.Background())
	}
	lease.Release()
	return recordResult(value, runErr)
}

func (s *Service) runRecord(ctx context.Context, session recordstore.Session, compiler *recordstore.Compiler,
	dialect recordstore.Dialect, entry recordstore.ModelEntry, operation string, request any,
	scope recordstore.TrustedScope, identity recordstore.ExecutionIdentity) (any, error) {
	switch operation {
	case recordstorev1.OperationCreate:
		return s.recordEngine.Create(ctx, session, compiler, entry, *request.(*recordstorev1.CreateRequest), scope, identity)
	case recordstorev1.OperationGet:
		return s.recordEngine.Get(ctx, session, compiler, entry, *request.(*recordstorev1.GetRequest), scope)
	case recordstorev1.OperationList:
		return s.recordEngine.List(ctx, session, compiler, entry, *request.(*recordstorev1.ListRequest), scope)
	case recordstorev1.OperationUpdate:
		return s.recordEngine.Update(ctx, session, compiler, entry, *request.(*recordstorev1.UpdateRequest), scope, identity)
	case recordstorev1.OperationDelete:
		return struct{}{}, s.recordEngine.Delete(ctx, session, compiler, entry, *request.(*recordstorev1.DeleteRequest), scope, identity)
	case recordstorev1.OperationBatch:
		return s.recordEngine.Batch(ctx, session, compiler, entry, *request.(*recordstorev1.BatchRequest), scope, identity)
	case recordstorev1.OperationAppendOutbox:
		return s.recordEngine.AppendOutbox(ctx, session, dialect, entry, *request.(*recordstorev1.AppendOutboxRequest), identity)
	default:
		return nil, NewRuntimeError(databasev1.ErrorUnsupported, false, errors.New("Record Store 操作尚未实现"))
	}
}

func recordWriteOperation(operation string) bool {
	switch operation {
	case recordstorev1.OperationCreate, recordstorev1.OperationUpdate, recordstorev1.OperationDelete,
		recordstorev1.OperationBatch, recordstorev1.OperationAppendOutbox:
		return true
	default:
		return false
	}
}

func requireModelOwner(call *contractv1.CallContext, entry recordstore.ModelEntry) error {
	if call.GetCaller().GetKind() == contractv1.CallerKind_CALLER_KIND_SYSTEM {
		return nil
	}
	if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || call.GetCaller().GetId() != entry.OwnerPluginID {
		return recordstore.ErrStorageDenied
	}
	return nil
}

func recordConnection(entry recordstore.ModelEntry, storage recordstorev1.StorageTarget) (databasev1.ConnectionRef, error) {
	if entry.Model.Storage.Kind == "platform-control" {
		return databasev1.ConnectionRef{}, recordstore.ErrStorageDenied
	}
	if storage.Connection == nil || databasev1.ValidateConnectionRef(*storage.Connection) != nil {
		return databasev1.ConnectionRef{}, errors.New("connection-ref DataModel 必须指定有效连接")
	}
	return *storage.Connection, nil
}

func recordRequestIdentity(request any) (recordstorev1.ModelRef, recordstorev1.StorageTarget, error) {
	switch value := request.(type) {
	case *recordstorev1.CreateRequest:
		return value.Model, value.Storage, nil
	case *recordstorev1.GetRequest:
		return value.Model, value.Storage, nil
	case *recordstorev1.ListRequest:
		return value.Model, value.Storage, nil
	case *recordstorev1.UpdateRequest:
		return value.Model, value.Storage, nil
	case *recordstorev1.DeleteRequest:
		return value.Model, value.Storage, nil
	case *recordstorev1.BatchRequest:
		return value.Model, value.Storage, nil
	case *recordstorev1.BeginRequest:
		return value.Model, value.Storage, nil
	case *recordstorev1.AppendOutboxRequest:
		return value.Model, value.Storage, nil
	case *recordstorev1.SchemaRequest:
		return value.Model, value.Storage, nil
	default:
		return recordstorev1.ModelRef{}, recordstorev1.StorageTarget{}, errors.New("Record Store 请求身份无效")
	}
}

func recordTransactionHandle(request any) string {
	switch value := request.(type) {
	case *recordstorev1.CreateRequest:
		return value.TransactionHandle
	case *recordstorev1.GetRequest:
		return value.TransactionHandle
	case *recordstorev1.ListRequest:
		return value.TransactionHandle
	case *recordstorev1.UpdateRequest:
		return value.TransactionHandle
	case *recordstorev1.DeleteRequest:
		return value.TransactionHandle
	case *recordstorev1.BatchRequest:
		return value.TransactionHandle
	case *recordstorev1.AppendOutboxRequest:
		return value.TransactionHandle
	case *recordstorev1.EndRequest:
		return value.TransactionHandle
	default:
		return ""
	}
}

func recordDescriptor() []byte {
	return []byte(`{"title":"Record Store","subcommands":[{"name":"syncModels","description":"同步同代已验证制品的 DataModel 与签名迁移目录"},{"name":"create","description":"创建声明式记录"},{"name":"get","description":"按主键读取声明式记录"},{"name":"list","description":"按受限过滤和游标分页列出记录"},{"name":"update","description":"按 CAS 更新声明式记录"},{"name":"delete","description":"按 CAS 删除声明式记录"},{"name":"batch","description":"在同一事务执行批量 mutation"},{"name":"begin","description":"开始 Repository UnitOfWork"},{"name":"commit","description":"提交 Repository UnitOfWork"},{"name":"rollback","description":"回滚 Repository UnitOfWork"},{"name":"appendOutbox","description":"在数据事务内追加 Outbox"},{"name":"schemaPlan","description":"读取 DataModel 迁移计划"},{"name":"schemaApply","description":"由唯一 Schema Controller 应用安全迁移"},{"name":"schemaStatus","description":"读取持久迁移账本状态"}]}`)
}
