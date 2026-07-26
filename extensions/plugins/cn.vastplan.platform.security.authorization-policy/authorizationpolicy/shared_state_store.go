package authorizationpolicy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	sharedstatesdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/sharedstate"
)

const (
	sharedStateNamespace = "authorization.policy"
	sharedStateKey       = "active"
	maxStateBytes        = 16 << 20
)

// StoreFactory binds Shared State calls to one trusted host invocation. The
// plugin never receives physical bucket names or NATS credentials.
type StoreFactory func(context.Context, sdk.Host, *contractv1.CallContext) (Store, error)

func SharedStateStoreFactory(ctx context.Context, host sdk.Host, call *contractv1.CallContext) (Store, error) {
	client, err := sharedstatesdk.NewFenced(host, "service", sharedStateNamespace)
	if err != nil {
		return nil, err
	}
	return &sharedStateStore{ctx: ctx, call: call, client: client}, nil
}

type sharedStateStore struct {
	ctx    context.Context
	call   *contractv1.CallContext
	client *sharedstatesdk.Client
}

func (s *sharedStateStore) Load() (State, error) {
	entry, err := s.client.Get(s.ctx, s.call, sharedStateKey)
	if sharedstatesdk.IsNotFound(err) {
		return emptyState(), nil
	}
	if err != nil {
		return State{}, fmt.Errorf("%w: 读取 Shared State: %v", ErrStoreUnavailable, err)
	}
	return decodeStoredState(entry.Value)
}

func (s *sharedStateStore) CompareAndSwap(expected uint64, next State) (State, error) {
	if next.Version != stateVersion || next.Generation != expected+1 {
		return State{}, fmt.Errorf("Authorization Policy CAS 参数无效: expected=%d next=%d", expected, next.Generation)
	}
	raw, err := json.Marshal(next)
	if err != nil || len(raw) > maxStateBytes {
		return State{}, errors.New("Authorization Policy Shared State 序列化失败或超过 16 MiB")
	}
	entry, err := s.client.Get(s.ctx, s.call, sharedStateKey)
	if sharedstatesdk.IsNotFound(err) {
		if expected != 0 {
			return State{}, fmt.Errorf("Authorization Policy CAS 冲突: expected=%d actual=0", expected)
		}
		_, err = s.client.Create(s.ctx, s.call, sharedStateKey, raw)
	} else if err == nil {
		current, decodeErr := decodeStoredState(entry.Value)
		if decodeErr != nil {
			return State{}, decodeErr
		}
		if current.Generation != expected {
			return State{}, fmt.Errorf("Authorization Policy CAS 冲突: expected=%d actual=%d", expected, current.Generation)
		}
		_, err = s.client.Update(s.ctx, s.call, sharedStateKey, raw, entry.Revision)
	}
	if err != nil {
		if sharedstatesdk.IsConflict(err) {
			return State{}, fmt.Errorf("Authorization Policy CAS 冲突: expected=%d", expected)
		}
		return State{}, fmt.Errorf("%w: 写入 Shared State: %v", ErrStoreUnavailable, err)
	}
	return next, nil
}

func decodeStoredState(raw []byte) (State, error) {
	if len(raw) == 0 || len(raw) > maxStateBytes {
		return State{}, errors.New("Authorization Policy Shared State 为空或超过 16 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var state State
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("解析 Authorization Policy Shared State: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return State{}, errors.New("Authorization Policy Shared State 包含尾随数据")
	}
	if state.Version != stateVersion || state.Generation == 0 {
		return State{}, errors.New("Authorization Policy Shared State 版本或 generation 无效")
	}
	return state, nil
}

func emptyState() State {
	return State{Version: stateVersion, Roles: []RoleRevision{}, Bindings: []BindingRevision{}, Revocations: []authorizationv1.Revocation{}, Audit: []AuditEvent{}}
}
