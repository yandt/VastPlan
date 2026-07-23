package deploymentmanager

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
	deploymentStateNamespace = "deployment.control"
	deploymentStateKey       = "tenant"
)

type deploymentStateSession struct {
	ctx        context.Context
	call       *contractv1.CallContext
	repository *deploymentStateRepository
	tenant     string
	revision   uint64
}

type deploymentStateRepository struct{ client *sharedstatesdk.Client }

func newDeploymentStateRepository(host sdk.Host) (*deploymentStateRepository, error) {
	client, err := sharedstatesdk.New(host, "tenant", deploymentStateNamespace)
	if err != nil {
		return nil, err
	}
	return &deploymentStateRepository{client: client}, nil
}

func (r *deploymentStateRepository) load(ctx context.Context, call *contractv1.CallContext) (*tenantState, uint64, error) {
	entry, err := r.client.Get(ctx, call, deploymentStateKey)
	if sharedstatesdk.IsNotFound(err) {
		return emptyTenantState(), 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("读取 Deployment Manager Shared State: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(entry.Value))
	decoder.DisallowUnknownFields()
	value := emptyTenantState()
	if err := decoder.Decode(value); err != nil {
		return nil, 0, fmt.Errorf("解析 Deployment Manager Shared State: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, 0, errors.New("Deployment Manager Shared State 包含尾随数据")
	}
	return value, entry.Revision, nil
}

func (r *deploymentStateRepository) save(ctx context.Context, call *contractv1.CallContext, value *tenantState, expected uint64) (uint64, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	if len(raw) > maxStateBytes {
		return 0, errors.New("Deployment Manager tenant 聚合超过 Shared State 单值上限")
	}
	var entry sharedstatesdk.Entry
	if expected == 0 {
		entry, err = r.client.Create(ctx, call, deploymentStateKey, raw)
	} else {
		entry, err = r.client.Update(ctx, call, deploymentStateKey, raw, expected)
	}
	if sharedstatesdk.IsConflict(err) {
		return 0, errStoreConflict
	}
	if err != nil {
		return 0, fmt.Errorf("保存 Deployment Manager Shared State: %w", err)
	}
	return entry.Revision, nil
}
