package platformcontrol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	sharedstatesqlv1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstatesql/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

// Invoker is the only transport seam required by the trusted host. Its
// implementation must create the fixed SYSTEM caller and select an instance
// through the global capability directory.
type Invoker interface {
	Invoke(context.Context, string, string, []byte) (*contractv1.CallResult, []byte, error)
}

// RemoteBootstrapper adapts the Database Runtime bootstrap capability to the
// transport-neutral Bootstrapper port.
type RemoteBootstrapper struct{ invoke Invoker }

func NewRemoteBootstrapper(invoke Invoker) (*RemoteBootstrapper, error) {
	if invoke == nil {
		return nil, errors.New("Platform Control Remote Bootstrapper 缺少 Invoker")
	}
	return &RemoteBootstrapper{invoke: invoke}, nil
}

func (b *RemoteBootstrapper) Test(ctx context.Context, profile platformcontrolv1.Profile, secret SecretSource) error {
	_, err := b.call(ctx, platformcontrolv1.OperationTest, profile, secret)
	return err
}

func (b *RemoteBootstrapper) Initialize(ctx context.Context, profile platformcontrolv1.Profile, secret SecretSource) (ManagedStore, error) {
	if _, err := b.call(ctx, platformcontrolv1.OperationInitialize, profile, secret); err != nil {
		return nil, err
	}
	return &RemoteStore{invoke: b.invoke}, nil
}

func (b *RemoteBootstrapper) Open(ctx context.Context, profile platformcontrolv1.Profile, secret SecretSource) (ManagedStore, error) {
	if _, err := b.call(ctx, platformcontrolv1.OperationOpen, profile, secret); err != nil {
		return nil, err
	}
	return &RemoteStore{invoke: b.invoke}, nil
}

func (b *RemoteBootstrapper) call(ctx context.Context, operation string, profile platformcontrolv1.Profile, secret SecretSource) (platformcontrolv1.Status, error) {
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
	result, raw, err := b.invoke.Invoke(ctx, platformcontrolv1.BootstrapCapability, operation, payload)
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
// Runtime process. Close is intentionally local: Runtime generation lifecycle
// owns the physical pool.
type RemoteStore struct{ invoke Invoker }

func (s *RemoteStore) Close() error { return nil }

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
	result, raw, err := s.invoke.Invoke(ctx, sharedstatesqlv1.Capability, operation, payload)
	if err != nil {
		return nil, err
	}
	if err := resultError(result); err != nil {
		return nil, err
	}
	return raw, nil
}

func resultError(result *contractv1.CallResult) error {
	if result == nil {
		return sharedstate.ErrUnavailable
	}
	if result.GetStatus() == contractv1.CallResult_STATUS_OK {
		return nil
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
