package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	sharedstatesdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/sharedstate"
)

const (
	stateNamespace = "settings.values"
	stateKey       = "active"
)

type tenantRepository struct{ client *sharedstatesdk.Client }

func newTenantRepository(host sdk.Host) (*tenantRepository, error) {
	client, err := sharedstatesdk.New(host, "tenant", stateNamespace)
	if err != nil {
		return nil, err
	}
	return &tenantRepository{client: client}, nil
}

func (r *tenantRepository) load(ctx context.Context, call *contractv1.CallContext) (tenantState, uint64, error) {
	entry, err := r.client.Get(ctx, call, stateKey)
	if sharedstatesdk.IsNotFound(err) {
		return emptyTenantState(), 0, nil
	}
	if err != nil {
		return tenantState{}, 0, err
	}
	decoder := json.NewDecoder(bytes.NewReader(entry.Value))
	decoder.DisallowUnknownFields()
	var state tenantState
	if err := decoder.Decode(&state); err != nil {
		return tenantState{}, 0, errors.New("解析全局设置 Shared State 失败")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return tenantState{}, 0, errors.New("全局设置 Shared State 包含尾随数据")
	}
	if state.Changes == nil {
		state.Changes = []change{}
	}
	if err := validateTenantState(state); err != nil {
		return tenantState{}, 0, err
	}
	return state, entry.Revision, nil
}

func (r *tenantRepository) save(ctx context.Context, call *contractv1.CallContext, state tenantState, expected uint64) error {
	if err := validateTenantState(state); err != nil {
		return err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if expected == 0 {
		_, err = r.client.Create(ctx, call, stateKey, raw)
	} else {
		_, err = r.client.Update(ctx, call, stateKey, raw, expected)
	}
	if sharedstatesdk.IsConflict(err) {
		return ErrVersionConflict
	}
	if err != nil {
		return fmt.Errorf("保存全局设置 Shared State: %w", err)
	}
	return nil
}
