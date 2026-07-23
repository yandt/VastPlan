package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	sharedstatesdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/sharedstate"
)

const stateNamespace = "artifact.assessment.schedule"

type PlanStore interface {
	Load(context.Context, *contractv1.CallContext, string) (Plan, uint64, bool, error)
	Save(context.Context, *contractv1.CallContext, string, Plan, uint64) (uint64, error)
}

type sharedPlanStore struct{ client *sharedstatesdk.Client }

func newSharedPlanStore(host sdk.Host) (*sharedPlanStore, error) {
	client, err := sharedstatesdk.NewFenced(host, "service", stateNamespace)
	if err != nil {
		return nil, err
	}
	return &sharedPlanStore{client: client}, nil
}

func (s *sharedPlanStore) Load(ctx context.Context, call *contractv1.CallContext, key string) (Plan, uint64, bool, error) {
	entry, err := s.client.Get(ctx, call, key)
	if sharedstatesdk.IsNotFound(err) {
		return Plan{}, 0, false, nil
	}
	if err != nil {
		return Plan{}, 0, false, err
	}
	var plan Plan
	decoder := json.NewDecoder(bytes.NewReader(entry.Value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return Plan{}, 0, false, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || plan.validate() != nil {
		return Plan{}, 0, false, errors.New("Assessment Controller 持久计划损坏")
	}
	return plan, entry.Revision, true, nil
}

func (s *sharedPlanStore) Save(ctx context.Context, call *contractv1.CallContext, key string, plan Plan, expected uint64) (uint64, error) {
	if err := plan.validate(); err != nil {
		return 0, err
	}
	raw, err := json.Marshal(plan)
	if err != nil || len(raw) > artifactassessment.MaxRecordBytes+4096 {
		return 0, errors.New("Assessment Controller plan 序列化失败或超限")
	}
	var entry sharedstatesdk.Entry
	if expected == 0 {
		entry, err = s.client.Create(ctx, call, key, raw)
	} else {
		entry, err = s.client.Update(ctx, call, key, raw, expected)
	}
	return entry.Revision, err
}
