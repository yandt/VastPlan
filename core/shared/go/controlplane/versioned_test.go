package controlplane

import (
	"context"
	"encoding/json"
	"testing"
)

type validationValue struct {
	Revision uint64 `json:"revision"`
	Value    string `json:"value"`
}

func validationCodec(validate func(*validationValue, validationValue) error) versionedCodec[validationValue] {
	return versionedCodec[validationValue]{
		parse: func(raw []byte) (validationValue, error) {
			var value validationValue
			err := json.Unmarshal(raw, &value)
			return value, err
		},
		revision:  func(value validationValue) uint64 { return value.Revision },
		digest:    func(value validationValue) string { return value.Value },
		validate:  validate,
		noun:      "测试值",
		monotonic: true,
	}
}

func TestVersionedValidationSharesTheCASRead(t *testing.T) {
	_, buckets := startControlplaneNATS(t)
	ctx := context.Background()
	key := "validation"
	initial := []byte(`{"revision":1,"value":"initial"}`)
	if _, _, err := applyVersioned(ctx, buckets.Deployments, key, initial, validationCodec(nil)); err != nil {
		t.Fatal(err)
	}

	candidate := []byte(`{"revision":2,"value":"candidate"}`)
	concurrent := []byte(`{"revision":2,"value":"concurrent"}`)
	_, _, err := applyVersioned(ctx, buckets.Deployments, key, candidate, validationCodec(func(current *validationValue, next validationValue) error {
		if current == nil || current.Revision != 1 || next.Value != "candidate" {
			t.Fatalf("validator 未看到 CAS 使用的当前值: current=%+v next=%+v", current, next)
		}
		entry, getErr := buckets.Deployments.Get(ctx, key)
		if getErr != nil {
			t.Fatal(getErr)
		}
		_, updateErr := buckets.Deployments.Update(ctx, key, concurrent, entry.Revision())
		return updateErr
	}))
	if err == nil {
		t.Fatal("validator 后发生并发更新时，外层写入必须 CAS 失败")
	}
	entry, getErr := buckets.Deployments.Get(ctx, key)
	if getErr != nil {
		t.Fatal(getErr)
	}
	var actual validationValue
	if json.Unmarshal(entry.Value(), &actual) != nil || actual.Value != "concurrent" {
		t.Fatalf("CAS 失败不得覆盖并发写入: %+v", actual)
	}
}
