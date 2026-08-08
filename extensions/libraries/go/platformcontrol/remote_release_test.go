package platformcontrol

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	sharedstatesqlv1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstatesql/v1"
)

func closeCalls(calls []string) []string {
	result := make([]string, 0, len(calls))
	for _, call := range calls {
		if strings.HasPrefix(call, platformcontrolv1.BootstrapCapability+"/"+platformcontrolv1.OperationClose+"@") {
			result = append(result, strings.TrimPrefix(call, platformcontrolv1.BootstrapCapability+"/"+platformcontrolv1.OperationClose+"@"))
		}
	}
	return result
}

func releaseTestInstances() []RuntimeInstance {
	return []RuntimeInstance{
		{ID: "runtime-a", NodeID: "node-local"},
		{ID: "runtime-b", NodeID: "node-remote"},
		{ID: "runtime-c", NodeID: "node-remote"},
	}
}

// A replica opened before a later replica failed is unreachable through any
// returned Store, so nothing else would ever release its pool.
func TestOpenReplicasReleasesAlreadyOpenedReplicasOnFailure(t *testing.T) {
	instances := releaseTestInstances()
	invoke := &testInvoker{instances: map[string][]RuntimeInstance{
		platformcontrolv1.BootstrapCapability: instances,
		sharedstatesqlv1.Capability:           instances,
	}}
	invoke.result = func(_, operation, instanceID string) (*contractv1.CallResult, []byte, error) {
		if instanceID == "runtime-c" && operation == platformcontrolv1.OperationOpen {
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: platformcontrolv1.ErrorConflict}}, nil, nil
		}
		raw, _ := json.Marshal(platformcontrolv1.Status{Phase: platformcontrolv1.PhaseReady, Generation: 7})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
	bootstrapper, err := NewRemoteBootstrapper(invoke)
	if err != nil {
		t.Fatal(err)
	}

	store, err := bootstrapper.Initialize(context.Background(), platformcontrolv1.Profile{SchemaVersion: 1, Generation: 7}, testSecret("secret"))
	if err == nil {
		t.Fatal("第三个副本失败时 Initialize 必须报错")
	}
	if store != nil {
		t.Fatal("失败时不得返回 Store")
	}

	released := closeCalls(invoke.calls)
	if len(released) != 2 || released[0] != "runtime-a" || released[1] != "runtime-b" {
		t.Fatalf("中途失败必须补偿释放已打开的前序副本，实际释放: %v", released)
	}
}

// Abandoning a candidate that was opened across every replica must release each
// of them; dropping the handle locally leaves the pools held in another process.
func TestRemoteStoreCloseReleasesOpenedReplicas(t *testing.T) {
	instances := releaseTestInstances()
	invoke := &testInvoker{instances: map[string][]RuntimeInstance{
		platformcontrolv1.BootstrapCapability: instances,
		sharedstatesqlv1.Capability:           instances,
	}}
	bootstrapper, err := NewRemoteBootstrapper(invoke)
	if err != nil {
		t.Fatal(err)
	}
	store, err := bootstrapper.Open(context.Background(), platformcontrolv1.Profile{SchemaVersion: 1, Generation: 4}, testSecret("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if got := closeCalls(invoke.calls); len(got) != 0 {
		t.Fatalf("成功打开时不得释放任何副本: %v", got)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	released := closeCalls(invoke.calls)
	if len(released) != 3 {
		t.Fatalf("Close 必须释放全部已打开副本，实际: %v", released)
	}
}

// The release must name the generation it abandons, so a stale release cannot
// close the pool a later activation installed.
func TestRemoteStoreCloseCarriesGeneration(t *testing.T) {
	instances := []RuntimeInstance{{ID: "runtime-a", NodeID: "node-local"}}
	invoke := &testInvoker{instances: map[string][]RuntimeInstance{
		platformcontrolv1.BootstrapCapability: instances,
		sharedstatesqlv1.Capability:           instances,
	}}
	bootstrapper, _ := NewRemoteBootstrapper(invoke)
	store, err := bootstrapper.Open(context.Background(), platformcontrolv1.Profile{SchemaVersion: 1, Generation: 9}, testSecret("secret"))
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if got := closeCalls(invoke.calls); len(got) != 1 {
		t.Fatalf("期望一次释放调用: %v", got)
	}

	remote, ok := store.(*RemoteStore)
	if !ok {
		t.Fatalf("期望 *RemoteStore: %T", store)
	}
	if remote.generation != 9 {
		t.Fatalf("Store 必须记住自己代表的 generation，实际: %d", remote.generation)
	}
}
