package recordstorev1_test

import (
	"testing"

	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
)

func TestParseRequestsStrictly(t *testing.T) {
	valid := []byte(`{"storage":{"connection":{"resourceId":"orders.primary","revision":2}},"model":{"id":"example.order","schemaVersion":1,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"record":{"id":"order-1"},"idempotencyKey":"create:order-1"}`)
	if _, err := recordstorev1.ParseRequest(recordstorev1.OperationCreate, valid); err != nil {
		t.Fatalf("合法 Create 应通过: %v", err)
	}
	invalid := []byte(`{"storage":{},"model":{"id":"example.order","schemaVersion":1,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"record":{"id":"order-1"},"idempotencyKey":"short","businessAction":"publish"}`)
	if _, err := recordstorev1.ParseRequest(recordstorev1.OperationCreate, invalid); err == nil {
		t.Fatal("未知业务字段必须拒绝")
	}
}

func TestBatchMutationShapeIsGoverned(t *testing.T) {
	raw := []byte(`{"storage":{},"model":{"id":"platform.setting","schemaVersion":1,"sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"mutations":[{"kind":"update","key":{"id":"one"},"idempotencyKey":"update:one"}]}`)
	if _, err := recordstorev1.ParseRequest(recordstorev1.OperationBatch, raw); err == nil {
		t.Fatal("update mutation 缺少 values/revision 必须拒绝")
	}
}
