package deploymentmanager

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func TestDeploymentStateIsSharedAcrossInstancesAndTenantIsolated(t *testing.T) {
	host := newDeploymentStateHost(t)
	callA := userCall("tenant-a", "alice")
	repository, err := newDeploymentStateRepository(host)
	if err != nil {
		t.Fatal(err)
	}
	value := emptyTenantState()
	plan := validPlan()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	value.Nodes["node-a"] = platformadminapi.ManagedNode{ID: "node-a", Plan: plan, Version: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := repository.save(context.Background(), callA, value, 0); err != nil {
		t.Fatal(err)
	}

	result, raw, err := New().Handler(context.Background(), host, callA, []byte(`{}`), "listNodes")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || !strings.Contains(string(raw), `"node-a"`) {
		t.Fatalf("第二实例未读取共享节点状态: result=%+v raw=%s err=%v", result, raw, err)
	}
	result, raw, err = New().Handler(context.Background(), host, userCall("tenant-b", "bob"), []byte(`{}`), "listNodes")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || string(raw) != `{"items":[]}` {
		t.Fatalf("跨 tenant 节点状态泄漏: result=%+v raw=%s err=%v", result, raw, err)
	}
}

func TestDeploymentStateCASFencesStaleLeader(t *testing.T) {
	host := newDeploymentStateHost(t)
	call := userCall("tenant-a", "alice")
	repository, _ := newDeploymentStateRepository(host)
	seed := emptyTenantState()
	revision, err := repository.save(context.Background(), call, seed, 0)
	if err != nil {
		t.Fatal(err)
	}
	first, firstRevision, err := repository.load(context.Background(), call)
	if err != nil || firstRevision != revision {
		t.Fatal(err)
	}
	second, secondRevision, err := repository.load(context.Background(), call)
	if err != nil || secondRevision != revision {
		t.Fatal(err)
	}
	first.NextRevision = 1
	if _, err := repository.save(context.Background(), call, first, firstRevision); err != nil {
		t.Fatal(err)
	}
	second.NextRevision = 2
	if _, err := repository.save(context.Background(), call, second, secondRevision); !errors.Is(err, errStoreConflict) {
		t.Fatalf("旧 leader 写入必须被 CAS fencing: %v", err)
	}
}

func TestNewLeaderRecoversInterruptedBootstrapOnce(t *testing.T) {
	host := newDeploymentStateHost(t)
	call := userCall("tenant-a", "alice")
	repository, _ := newDeploymentStateRepository(host)
	value := emptyTenantState()
	now := time.Now().UTC()
	value.Nodes["node-a"] = platformadminapi.ManagedNode{ID: "node-a", Plan: validPlan(), Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	value.Jobs["job-a"] = platformadminapi.BootstrapJob{ID: "job-a", NodeID: "node-a", NodeVersion: 1, State: platformadminapi.BootstrapConnecting, RequestedBy: "alice", ApprovedBy: "bob", CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano)}
	if _, err := repository.save(context.Background(), call, value, 0); err != nil {
		t.Fatal(err)
	}
	result, raw, err := New().Handler(context.Background(), host, call, []byte(`{}`), "listBootstrapJobs")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || !strings.Contains(string(raw), `"Failed"`) || !strings.Contains(string(raw), `platform.deployment.interrupted`) {
		t.Fatalf("新 leader 未恢复中断引导: result=%+v raw=%s err=%v", result, raw, err)
	}
}

type deploymentStateHost struct {
	store sharedstate.Store
	mu    sync.Mutex
}

var _ sdk.Host = (*deploymentStateHost)(nil)

func newDeploymentStateHost(t *testing.T) *deploymentStateHost {
	store, err := sharedstate.OpenFileStore(filepath.Join(t.TempDir(), "shared-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &deploymentStateHost{store: store}
}

func (h *deploymentStateHost) Call(ctx context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	operation := strings.TrimPrefix(target.GetCapability(), sharedstatev1.KernelServicePrefix)
	request, err := sharedstatev1.ParseRequest(operation, payload)
	if err != nil {
		return deploymentStateResult("state.invalid", false), nil, nil
	}
	scope := sharedstate.Scope{Kind: sharedstate.ScopeTenant, TenantID: call.GetTenantId(), PluginID: PluginID, RuntimeScope: "platform-deployment", Namespace: deploymentStateNamespace}
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
	}
	if err != nil {
		switch {
		case errors.Is(err, sharedstate.ErrNotFound):
			return deploymentStateResult("state.not_found", false), nil, nil
		case errors.Is(err, sharedstate.ErrConflict):
			return deploymentStateResult("state.conflict", true), nil, nil
		default:
			return deploymentStateResult("state.unavailable", true), nil, nil
		}
	}
	entry := response.(sharedstate.Entry)
	raw, _ := json.Marshal(sharedstatev1.Entry{Protocol: sharedstatev1.Protocol, Key: entry.Key, Value: sharedstatev1.EncodeValue(entry.Value), Revision: entry.Revision, UpdatedAt: entry.UpdatedAt})
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func deploymentStateResult(code string, retryable bool) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: code, Retryable: retryable}}
}
