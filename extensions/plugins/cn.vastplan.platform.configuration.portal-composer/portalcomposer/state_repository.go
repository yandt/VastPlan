package portalcomposer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	sharedstatesdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/sharedstate"
)

const (
	composerStateNamespace = "portal.composition"
	composerStateKey       = "tenant"
	maximumComposerState   = 1 << 20
)

type composerStateSession struct {
	ctx        context.Context
	call       *contractv1.CallContext
	repository *composerStateRepository
	tenant     string
	revision   uint64
}

type composerStateRepository struct{ client *sharedstatesdk.Client }

func newComposerStateRepository(host sdk.Host) (*composerStateRepository, error) {
	client, err := sharedstatesdk.New(host, "tenant", composerStateNamespace)
	if err != nil {
		return nil, err
	}
	return &composerStateRepository{client: client}, nil
}

func (r *composerStateRepository) load(ctx context.Context, call *contractv1.CallContext) (state, uint64, error) {
	entry, err := r.client.Get(ctx, call, composerStateKey)
	if sharedstatesdk.IsNotFound(err) {
		return emptyState(), 0, nil
	}
	if err != nil {
		return state{}, 0, fmt.Errorf("读取 Portal Composer Shared State: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(entry.Value))
	decoder.DisallowUnknownFields()
	value := emptyState()
	if err := decoder.Decode(&value); err != nil {
		return state{}, 0, fmt.Errorf("解析 Portal Composer Shared State: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return state{}, 0, errors.New("Portal Composer Shared State 包含尾随数据")
	}
	return value, entry.Revision, nil
}

func (r *composerStateRepository) save(ctx context.Context, call *contractv1.CallContext, value state, expected uint64) (uint64, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	if len(raw) > maximumComposerState {
		return 0, errors.New("Portal Composer 租户聚合超过 Shared State 单值上限")
	}
	var entry sharedstatesdk.Entry
	if expected == 0 {
		entry, err = r.client.Create(ctx, call, composerStateKey, raw)
	} else {
		entry, err = r.client.Update(ctx, call, composerStateKey, raw, expected)
	}
	if sharedstatesdk.IsConflict(err) {
		return 0, ErrStateConflict
	}
	if err != nil {
		return 0, fmt.Errorf("保存 Portal Composer Shared State: %w", err)
	}
	return entry.Revision, nil
}

func emptyState() state {
	return state{TestBindings: map[string]portalapi.TestTargetBinding{}}
}

func validateComposerTenantState(value state, tenant string) error {
	if tenant == "" || value.TestBindings == nil {
		return errors.New("Portal Composer tenant 状态无效")
	}
	for _, revision := range value.Revisions {
		if revision.TenantID != tenant {
			return errors.New("Portal Composer Application 跨 tenant")
		}
	}
	for _, revision := range value.Profiles {
		if revision.TenantID != "*" && revision.TenantID != tenant {
			return errors.New("Portal Composer Profile 跨 tenant")
		}
	}
	for _, revision := range value.Bindings {
		if revision.TenantID != tenant {
			return errors.New("Portal Composer Binding 跨 tenant")
		}
	}
	for _, activation := range value.Activations {
		if activation.TenantID != tenant {
			return errors.New("Portal Composer Activation 跨 tenant")
		}
	}
	for key, binding := range value.TestBindings {
		if key == "" || binding.TenantID != tenant {
			return errors.New("Portal Composer Test Binding 跨 tenant")
		}
	}
	for _, release := range value.TestReleases {
		if release.TenantID != tenant {
			return errors.New("Portal Composer Test Release 跨 tenant")
		}
	}
	for _, event := range value.Audit {
		if event.TenantID != tenant {
			return errors.New("Portal Composer Audit 跨 tenant")
		}
	}
	return nil
}
