package platformcontrol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	sharedstatesqlv1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstatesql/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

// Invoker is the only transport seam required by the trusted host. Its
// implementation must create the fixed SYSTEM caller and select an instance
// through the global capability directory.
type Invoker interface {
	Invoke(context.Context, string, string, []byte) (*contractv1.CallResult, []byte, error)
	InvokeInstance(context.Context, string, string, string, []byte) (*contractv1.CallResult, []byte, error)
	Instances(string) []RuntimeInstance
}

// RemoteBootstrapper adapts the Database Runtime bootstrap capability to the
// transport-neutral Bootstrapper port.
type RemoteBootstrapper struct {
	invoke   Invoker
	replicas *runtimeReplicaSet
}

func NewRemoteBootstrapper(invoke Invoker) (*RemoteBootstrapper, error) {
	if invoke == nil {
		return nil, errors.New("Platform Control Remote Bootstrapper 缺少 Invoker")
	}
	return &RemoteBootstrapper{invoke: invoke, replicas: newRuntimeReplicaSet()}, nil
}

func (b *RemoteBootstrapper) Test(ctx context.Context, profile platformcontrolv1.Profile, secret SecretSource) error {
	return b.callFirst(ctx, platformcontrolv1.OperationTest, profile, secret)
}

func (b *RemoteBootstrapper) Provision(ctx context.Context, profile platformcontrolv1.Profile, secret SecretSource) error {
	return b.callFirst(ctx, platformcontrolv1.OperationProvision, profile, secret)
}

func (b *RemoteBootstrapper) callFirst(ctx context.Context, operation string, profile platformcontrolv1.Profile, secret SecretSource) error {
	instances := b.invoke.Instances(platformcontrolv1.BootstrapCapability)
	if len(instances) == 0 {
		return sharedstate.ErrUnavailable
	}
	_, err := b.callInstance(ctx, operation, instances[0].ID, profile, secret)
	return err
}

func (b *RemoteBootstrapper) Initialize(ctx context.Context, profile platformcontrolv1.Profile, secret SecretSource) (ManagedStore, error) {
	return b.openReplicas(ctx, platformcontrolv1.OperationInitialize, profile, secret)
}

func (b *RemoteBootstrapper) Open(ctx context.Context, profile platformcontrolv1.Profile, secret SecretSource) (ManagedStore, error) {
	return b.openReplicas(ctx, platformcontrolv1.OperationOpen, profile, secret)
}

func (b *RemoteBootstrapper) openReplicas(ctx context.Context, firstOperation string, profile platformcontrolv1.Profile, secret SecretSource) (ManagedStore, error) {
	instances := b.invoke.Instances(platformcontrolv1.BootstrapCapability)
	if len(instances) == 0 {
		return nil, sharedstate.ErrUnavailable
	}
	opened := make([]string, 0, len(instances))
	for index, instance := range instances {
		operation := platformcontrolv1.OperationOpen
		if index == 0 {
			operation = firstOperation
		}
		if _, err := b.callInstance(ctx, operation, instance.ID, profile, secret); err != nil {
			// The replicas opened before this failure are never reachable through
			// a returned Store, so nothing else would ever release them. They are
			// not in b.replicas either, which is why this compensation cannot be
			// left to RemoteStore.Close.
			for _, openedID := range opened {
				_ = releaseCandidate(ctx, b.invoke, openedID, profile.Generation)
			}
			return nil, err
		}
		opened = append(opened, instance.ID)
	}
	b.replicas.Replace(opened)
	return &RemoteStore{invoke: b.invoke, replicas: b.replicas, generation: profile.Generation}, nil
}

func (b *RemoteBootstrapper) callInstance(ctx context.Context, operation, instanceID string, profile platformcontrolv1.Profile, secret SecretSource) (platformcontrolv1.Status, error) {
	if secret == nil {
		return platformcontrolv1.Status{}, sharedstate.ErrUnavailable
	}
	if err := secret.WithSecret(ctx, func(value []byte) error {
		if len(value) == 0 {
			return sharedstate.ErrUnavailable
		}
		return nil
	}); err != nil {
		return platformcontrolv1.Status{}, err
	}
	payload, err := json.Marshal(profile)
	if err != nil {
		return platformcontrolv1.Status{}, err
	}
	result, raw, err := b.invoke.InvokeInstance(ctx, platformcontrolv1.BootstrapCapability, operation, instanceID, payload)
	if err != nil {
		return platformcontrolv1.Status{}, err
	}
	if err := resultError(result); err != nil {
		return platformcontrolv1.Status{}, err
	}
	var status platformcontrolv1.Status
	if err := json.Unmarshal(raw, &status); err != nil || status.Phase == "" {
		return platformcontrolv1.Status{}, sharedstate.ErrUnavailable
	}
	return status, nil
}

// RemoteStore keeps the kernel Store SPI independent of the physical Database
// Runtime process. Close releases the candidate pool each replica opened for
// this generation: once the pool lives in another process, dropping the handle
// locally is not enough, and the replica would hold it until some later
// activation happened to replace it.
type RemoteStore struct {
	invoke     Invoker
	replicas   *runtimeReplicaSet
	generation uint64
}

// closeTimeout bounds the release sweep. Close runs on failure paths that
// already have an error to report, so it must not block them.
const closeTimeout = 5 * time.Second

func (s *RemoteStore) Close() error {
	if s == nil || s.generation == 0 || s.invoke == nil || s.replicas == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	// Best effort: a replica that is already gone has released the pool with its
	// process, and the caller is handling a failure it must still report.
	for _, instanceID := range s.replicas.Preferred(s.invoke.Instances(platformcontrolv1.BootstrapCapability)) {
		_ = releaseCandidate(ctx, s.invoke, instanceID, s.generation)
	}
	return nil
}

// releaseCandidate sends one close call. It carries no secret because the
// replica is only being told to drop a pool it already owns.
func releaseCandidate(ctx context.Context, invoke Invoker, instanceID string, generation uint64) error {
	payload, err := json.Marshal(platformcontrolv1.CloseRequest{Generation: generation})
	if err != nil {
		return err
	}
	result, _, err := invoke.InvokeInstance(ctx, platformcontrolv1.BootstrapCapability, platformcontrolv1.OperationClose, instanceID, payload)
	if err != nil {
		return err
	}
	return resultError(result)
}

func (s *RemoteStore) Get(ctx context.Context, scope sharedstate.Scope, key string) (sharedstate.Entry, error) {
	raw, err := s.call(ctx, sharedstatesqlv1.OperationGet, sharedstatesqlv1.KeyRequest{Scope: scopeToWire(scope), Key: key})
	if err != nil {
		return sharedstate.Entry{}, err
	}
	var entry sharedstatesqlv1.Entry
	if json.Unmarshal(raw, &entry) != nil {
		return sharedstate.Entry{}, sharedstate.ErrUnavailable
	}
	return entryFromWire(entry)
}

func (s *RemoteStore) Create(ctx context.Context, scope sharedstate.Scope, key string, value []byte) (sharedstate.Entry, error) {
	return s.write(ctx, sharedstatesqlv1.OperationCreate, scope, key, value, 0)
}

func (s *RemoteStore) Update(ctx context.Context, scope sharedstate.Scope, key string, value []byte, expected uint64) (sharedstate.Entry, error) {
	return s.write(ctx, sharedstatesqlv1.OperationUpdate, scope, key, value, expected)
}

func (s *RemoteStore) write(ctx context.Context, operation string, scope sharedstate.Scope, key string, value []byte, expected uint64) (sharedstate.Entry, error) {
	raw, err := s.call(ctx, operation, sharedstatesqlv1.WriteRequest{Scope: scopeToWire(scope), Key: key, ValueBase64: base64.StdEncoding.EncodeToString(value), ExpectedRevision: expected})
	if err != nil {
		return sharedstate.Entry{}, err
	}
	var entry sharedstatesqlv1.Entry
	if json.Unmarshal(raw, &entry) != nil {
		return sharedstate.Entry{}, sharedstate.ErrUnavailable
	}
	return entryFromWire(entry)
}

func (s *RemoteStore) Delete(ctx context.Context, scope sharedstate.Scope, key string, expected uint64) error {
	_, err := s.call(ctx, sharedstatesqlv1.OperationDelete, sharedstatesqlv1.DeleteRequest{Scope: scopeToWire(scope), Key: key, ExpectedRevision: expected})
	return err
}

func (s *RemoteStore) List(ctx context.Context, scope sharedstate.Scope, prefix string, limit int, cursor string) (sharedstate.Page, error) {
	raw, err := s.call(ctx, sharedstatesqlv1.OperationList, sharedstatesqlv1.ListRequest{Scope: scopeToWire(scope), Prefix: prefix, Limit: limit, Cursor: cursor})
	if err != nil {
		return sharedstate.Page{}, err
	}
	var page sharedstatesqlv1.Page
	if json.Unmarshal(raw, &page) != nil {
		return sharedstate.Page{}, sharedstate.ErrUnavailable
	}
	result := sharedstate.Page{Items: make([]sharedstate.Entry, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, entry := range page.Items {
		converted, convertErr := entryFromWire(entry)
		if convertErr != nil {
			return sharedstate.Page{}, convertErr
		}
		result.Items = append(result.Items, converted)
	}
	return result, nil
}

func (s *RemoteStore) call(ctx context.Context, operation string, request any) ([]byte, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if s.replicas == nil {
		return nil, sharedstate.ErrUnavailable
	}
	instances := s.replicas.Preferred(s.invoke.Instances(sharedstatesqlv1.Capability))
	for _, instanceID := range instances {
		result, raw, err := s.invoke.InvokeInstance(ctx, sharedstatesqlv1.Capability, operation, instanceID, payload)
		if err != nil {
			continue
		}
		if err := resultError(result); err != nil {
			return nil, err
		}
		return raw, nil
	}
	return nil, sharedstate.ErrUnavailable
}

func resultError(result *contractv1.CallResult) error {
	if result == nil {
		return sharedstate.ErrUnavailable
	}
	if result.GetStatus() == contractv1.CallResult_STATUS_OK {
		return nil
	}
	if databasev1.KnownErrorCode(result.GetError().GetCode()) {
		return NewFailure(result.GetError().GetCode(), result.GetError().GetRetryable())
	}
	switch result.GetError().GetCode() {
	case sharedstatesqlv1.ErrorInvalid, platformcontrolv1.ErrorInvalid:
		return sharedstate.ErrInvalid
	case sharedstatesqlv1.ErrorNotFound:
		return sharedstate.ErrNotFound
	case sharedstatesqlv1.ErrorConflict, platformcontrolv1.ErrorConflict:
		return sharedstate.ErrConflict
	default:
		return sharedstate.ErrUnavailable
	}
}

func scopeToWire(value sharedstate.Scope) sharedstatesqlv1.Scope {
	return sharedstatesqlv1.Scope{Kind: string(value.Kind), TenantID: value.TenantID, PluginID: value.PluginID, RuntimeScope: value.RuntimeScope, Namespace: value.Namespace}
}

func entryFromWire(value sharedstatesqlv1.Entry) (sharedstate.Entry, error) {
	raw, err := base64.StdEncoding.DecodeString(value.ValueBase64)
	if err != nil || value.Key == "" || value.Revision == 0 {
		return sharedstate.Entry{}, sharedstate.ErrUnavailable
	}
	return sharedstate.Entry{Key: value.Key, Value: raw, Revision: value.Revision, UpdatedAt: value.UpdatedAt}, nil
}

var _ Bootstrapper = (*RemoteBootstrapper)(nil)
var _ ManagedStore = (*RemoteStore)(nil)
