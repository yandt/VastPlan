package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.credentials/credentialsstate"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func TestCredentialSnapshotSupportsChunksAndCrossInstanceRead(t *testing.T) {
	host := newCredentialStateHost(t)
	call := credentialContext("tenant-a")
	repository, err := newCredentialStateRepository(host)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	value := emptyCredentialSnapshot()
	value.Records["large"] = Record{Name: "large", Version: 1, KeyVersion: "v1", CreatedAt: now, UpdatedAt: now, Ciphertext: "vault:v1:" + strings.Repeat("x", 2*credentialsChunkBytes+1000)}
	if _, err := repository.save(context.Background(), call, value, 0); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := repository.load(context.Background(), call)
	if err != nil || loaded.Records["large"].Ciphertext != value.Records["large"].Ciphertext {
		t.Fatalf("多 chunk 快照跨实例读取失败: err=%v size=%d", err, len(loaded.Records["large"].Ciphertext))
	}
	other, _, err := repository.load(context.Background(), credentialContext("tenant-b"))
	if err != nil || len(other.Records) != 0 {
		t.Fatalf("Credentials Shared State 必须按 tenant 隔离: value=%+v err=%v", other, err)
	}
}

func TestCredentialSnapshotCASRejectsStaleLeaderAndTamperedChunk(t *testing.T) {
	host := newCredentialStateHost(t)
	call := credentialContext("tenant-a")
	repository, _ := newCredentialStateRepository(host)
	seed := emptyCredentialSnapshot()
	revision, err := repository.save(context.Background(), call, seed, 0)
	if err != nil {
		t.Fatal(err)
	}
	first, firstRevision, _ := repository.load(context.Background(), call)
	second, secondRevision, _ := repository.load(context.Background(), call)
	now := time.Now().UTC()
	first.Records["a"] = Record{Name: "a", Version: 1, KeyVersion: "v1", CreatedAt: now, UpdatedAt: now, Ciphertext: "vault:v1:a"}
	if _, err := repository.save(context.Background(), call, first, firstRevision); err != nil {
		t.Fatal(err)
	}
	second.Records["b"] = Record{Name: "b", Version: 1, KeyVersion: "v1", CreatedAt: now, UpdatedAt: now, Ciphertext: "vault:v1:b"}
	if _, err := repository.save(context.Background(), call, second, secondRevision); !errors.Is(err, errStateConflict) {
		t.Fatalf("旧 leader Root CAS 必须失败: seed=%d err=%v", revision, err)
	}

	rootEntry, err := host.store.Get(context.Background(), host.scope("tenant-a"), credentialsRootKey)
	if err != nil {
		t.Fatal(err)
	}
	root, err := credentialsstate.ParseRoot(rootEntry.Value)
	if err != nil {
		t.Fatal(err)
	}
	chunkKey := credentialsBlobPrefix + root.Chunks[0].Digest
	chunk, err := host.store.Get(context.Background(), host.scope("tenant-a"), chunkKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.store.Update(context.Background(), host.scope("tenant-a"), chunkKey, []byte("tampered"), chunk.Revision); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.load(context.Background(), call); err == nil || !strings.Contains(err.Error(), "chunk") {
		t.Fatalf("被篡改 chunk 必须 fail-closed: %v", err)
	}
}

func TestMaterialLeaseReloadsRootAfterDecrypt(t *testing.T) {
	host := newCredentialStateHost(t)
	transit := &blockingCredentialTransit{decryptStarted: make(chan struct{}), decryptRelease: make(chan struct{})}
	owner := managedContext("tenant-a", "plugin.database")
	writer, _ := New(transit)
	result, raw, err := writer.Handler(context.Background(), host, owner, []byte(`{"purpose":"database.connection","resource":"primary","value":"secret"}`), "stageManaged")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("暂存凭证失败: result=%+v raw=%s err=%v", result, raw, err)
	}
	var staged pluginconfig.StagedCredential
	if err := json.Unmarshal(raw, &staged); err != nil {
		t.Fatal(err)
	}
	activate, _ := json.Marshal(map[string]string{"stageId": staged.ID})
	if result, _, err = writer.Handler(context.Background(), host, owner, activate, "activateManaged"); err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("激活凭证失败: result=%+v err=%v", result, err)
	}
	request, recipient, err := credentiallease.NewRequest(staged.Ref)
	if err != nil {
		t.Fatal(err)
	}
	defer recipient.Discard()
	payload, _ := json.Marshal(request)
	kernel := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "runtime-a"}}
	reader, _ := New(transit)
	type response struct {
		result *contractv1.CallResult
		err    error
	}
	done := make(chan response, 1)
	go func() {
		result, _, err := reader.MaterialLeaseHandler(context.Background(), host, kernel, payload, "issue")
		done <- response{result: result, err: err}
	}()
	<-transit.decryptStarted
	retire, _ := json.Marshal(map[string]string{"handle": staged.Ref.Handle})
	secondLeader, _ := New(transit)
	if result, _, err := secondLeader.Handler(context.Background(), host, owner, retire, "retireManaged"); err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("并发退役失败: result=%+v err=%v", result, err)
	}
	close(transit.decryptRelease)
	got := <-done
	if got.err != nil || got.result.GetStatus() != contractv1.CallResult_STATUS_ERROR || got.result.GetError().GetCode() != "platform.credentials.material_lease.denied" {
		t.Fatalf("decrypt 期间发生退役后不得签发 lease: result=%+v err=%v", got.result, got.err)
	}
}

func TestTenantRequestRunsBoundedLazyMaintenance(t *testing.T) {
	host := newCredentialStateHost(t)
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	call := auditContext("tenant-a")
	repository, _ := newCredentialStateRepository(host)
	value := emptyCredentialSnapshot()
	value.Managed["stage-old"] = ManagedRecord{
		StageID: "stage-old", Ref: pluginconfig.ManagedCredentialRef{Handle: "credential://managed/old", Scope: "tenant", Owner: "plugin.database", Purpose: "database.connection", Version: 1},
		Resource: "primary", State: managedPreparing, CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour), Ciphertext: "vault:v1:secret",
	}
	if _, err := repository.save(context.Background(), call, value, 0); err != nil {
		t.Fatal(err)
	}
	policy := MaintenancePolicy{PreparingMaxAge: time.Hour, AbortedRetention: 24 * time.Hour, AuditRetention: 48 * time.Hour, Interval: time.Hour, BatchSize: 20}
	service, err := NewWithOptions(&fakeTransit{}, ServiceOptions{Maintenance: policy, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := service.Handler(context.Background(), host, call, []byte(`{"limit":100}`), "listManagedAudit")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("租户请求触发维护失败: result=%+v err=%v", result, err)
	}
	loaded, _, err := repository.load(context.Background(), call)
	record := loaded.Managed["stage-old"]
	if err != nil || record.State != managedAborted || record.Ciphertext != "" || len(loaded.Audit.Events) != 1 || loaded.Audit.Events[0].Action != "managed.auto-aborted" {
		t.Fatalf("lazy maintenance 未原子持久化终止和审计: record=%+v audit=%+v err=%v", record, loaded.Audit.Events, err)
	}
}

func TestSharedStateUnavailableNeverFallsBackToLocalCredentials(t *testing.T) {
	service, _ := New(&fakeTransit{})
	result, _, err := service.Handler(context.Background(), unavailableCredentialStateHost{}, credentialContext("tenant-a"), []byte(`{}`), "list")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_ERROR || result.GetError().GetCode() != "platform.credentials.unavailable" || !result.GetError().GetRetryable() {
		t.Fatalf("Shared State 故障必须 fail-closed 且可重试: result=%+v err=%v", result, err)
	}
}

type blockingCredentialTransit struct {
	decryptStarted chan struct{}
	decryptRelease chan struct{}
}

func (b *blockingCredentialTransit) Encrypt(_ context.Context, value []byte) (string, error) {
	return "vault:v1:" + string(value), nil
}
func (b *blockingCredentialTransit) Rewrap(_ context.Context, value string) (string, error) {
	return value, nil
}
func (b *blockingCredentialTransit) Decrypt(_ context.Context, value string) ([]byte, error) {
	close(b.decryptStarted)
	<-b.decryptRelease
	return []byte(strings.TrimPrefix(value, "vault:v1:")), nil
}

type credentialStateHost struct{ store sharedstate.Store }

var _ sdk.Host = (*credentialStateHost)(nil)

func newCredentialStateHost(t *testing.T) *credentialStateHost {
	store, err := sharedstate.OpenFileStore(filepath.Join(t.TempDir(), "shared-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &credentialStateHost{store: store}
}

func (h *credentialStateHost) scope(tenantID string) sharedstate.Scope {
	return sharedstate.Scope{Kind: sharedstate.ScopeTenant, TenantID: tenantID, PluginID: PluginID, RuntimeScope: "platform-credentials", Namespace: credentialsStateNamespace}
}

func (h *credentialStateHost) Call(ctx context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	operation := strings.TrimPrefix(target.GetCapability(), sharedstatev1.FencedKernelServicePrefix)
	if operation == target.GetCapability() {
		operation = strings.TrimPrefix(target.GetCapability(), sharedstatev1.KernelServicePrefix)
	}
	request, err := sharedstatev1.ParseRequest(operation, payload)
	if err != nil {
		return credentialStateResult("state.invalid", false), nil, nil
	}
	scope := h.scope(call.GetTenantId())
	var response any
	switch typed := request.(type) {
	case *sharedstatev1.KeyRequest:
		response, err = h.store.Get(ctx, scope, typed.Key)
	case *sharedstatev1.WriteRequest:
		value, decodeErr := sharedstatev1.DecodeValue(typed.Value)
		if decodeErr != nil {
			err = decodeErr
		} else if operation == sharedstatev1.OperationCreate {
			response, err = h.store.Create(ctx, scope, typed.Key, value)
		} else {
			response, err = h.store.Update(ctx, scope, typed.Key, value, typed.ExpectedRevision)
		}
	case *sharedstatev1.DeleteRequest:
		err = h.store.Delete(ctx, scope, typed.Key, typed.ExpectedRevision)
		response = sharedstatev1.Ack{Protocol: sharedstatev1.Protocol}
	case *sharedstatev1.ListRequest:
		response, err = h.store.List(ctx, scope, typed.Prefix, typed.Limit, typed.PageCursor)
	}
	if err != nil {
		switch {
		case errors.Is(err, sharedstate.ErrNotFound):
			return credentialStateResult("state.not_found", false), nil, nil
		case errors.Is(err, sharedstate.ErrConflict):
			return credentialStateResult("state.conflict", true), nil, nil
		default:
			return credentialStateResult("state.unavailable", true), nil, nil
		}
	}
	var wire any
	switch typed := response.(type) {
	case sharedstate.Entry:
		wire = sharedstatev1.Entry{Protocol: sharedstatev1.Protocol, Key: typed.Key, Value: sharedstatev1.EncodeValue(typed.Value), Revision: typed.Revision, UpdatedAt: typed.UpdatedAt}
	case sharedstate.Page:
		page := sharedstatev1.Page{Protocol: sharedstatev1.Protocol, Items: make([]sharedstatev1.Entry, 0, len(typed.Items)), NextPageCursor: typed.NextCursor}
		for _, item := range typed.Items {
			page.Items = append(page.Items, sharedstatev1.Entry{Protocol: sharedstatev1.Protocol, Key: item.Key, Value: sharedstatev1.EncodeValue(item.Value), Revision: item.Revision, UpdatedAt: item.UpdatedAt})
		}
		wire = page
	default:
		wire = typed
	}
	raw, _ := json.Marshal(wire)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func credentialStateResult(code string, retryable bool) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: code, Retryable: retryable}}
}

type unavailableCredentialStateHost struct{}

func (unavailableCredentialStateHost) Call(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
	return credentialStateResult("state.unavailable", true), nil, nil
}
