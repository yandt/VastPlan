package sharedstatev1

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRequestsAreIdentityFreeAndStrict(t *testing.T) {
	raw := []byte(`{"scope":"tenant","namespace":"settings","key":"active","value":"e30"}`)
	parsed, err := ParseRequest(OperationCreate, raw)
	if err != nil || parsed.(*WriteRequest).ExpectedRevision != 0 {
		t.Fatalf("create request: %+v err=%v", parsed, err)
	}
	forged := []byte(`{"scope":"tenant","namespace":"settings","key":"active","value":"e30","tenantId":"forged"}`)
	if _, err := ParseRequest(OperationCreate, forged); err == nil {
		t.Fatal("Shared State 请求不得携带 tenant/plugin/runtime identity")
	}
}

func TestResponseStrictParsing(t *testing.T) {
	entry := Entry{Protocol: Protocol, Key: "active", Value: EncodeValue([]byte(`{}`)), Revision: 1, UpdatedAt: time.Now().UTC()}
	raw, _ := json.Marshal(entry)
	if _, err := ParseEntry(raw); err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	_ = json.Unmarshal(raw, &object)
	object["pluginId"] = "forged"
	raw, _ = json.Marshal(object)
	if _, err := ParseEntry(raw); err == nil {
		t.Fatal("Shared State 响应必须拒绝未知字段")
	}
}
