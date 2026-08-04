package sharedstatesqlv1_test

import (
	"testing"

	sharedstatesqlv1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstatesql/v1"
)

func TestSQLSharedStateHostWireIsStrict(t *testing.T) {
	valid := []byte(`{"scope":{"kind":"tenant","tenantId":"tenant-a","pluginId":"cn.vastplan.example","runtimeScope":"service-a","namespace":"settings"},"key":"active","valueBase64":"e30="}`)
	if _, err := sharedstatesqlv1.ParseRequest(sharedstatesqlv1.OperationCreate, valid); err != nil {
		t.Fatal(err)
	}
	unknown := append(valid[:len(valid)-1], []byte(`,"mode":"development"}`)...)
	if _, err := sharedstatesqlv1.ParseRequest(sharedstatesqlv1.OperationCreate, unknown); err == nil {
		t.Fatal("未知模式字段必须拒绝")
	}
	if _, err := sharedstatesqlv1.ParseRequest(sharedstatesqlv1.OperationUpdate, valid); err == nil {
		t.Fatal("Update 必须携带 expectedRevision")
	}
}
