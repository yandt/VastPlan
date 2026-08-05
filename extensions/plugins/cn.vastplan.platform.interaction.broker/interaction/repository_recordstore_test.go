package interaction

import (
	"encoding/json"
	"testing"
	"time"

	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/interactionapi"
)

func TestRecordRepositoryRoundTripsGeneratedModelShape(t *testing.T) {
	now := time.Date(2026, 8, 5, 1, 2, 3, 0, time.UTC)
	request := testRequest(now)
	record := storedRecord{Record: interactionapi.Record{
		Request: request, State: interactionapi.StateCreated, CreatedAt: now, UpdatedAt: now,
	}, RequestHash: "1111111111111111111111111111111111111111111111111111111111111111"}
	wire, err := encodeCreate(record)
	if err != nil {
		t.Fatal(err)
	}
	if _, hasTrustedTenant := wire["tenantId"]; hasTrustedTenant {
		t.Fatal("Repository 不得在 payload 自报可信 tenant scope")
	}
	wire["tenantId"] = mustRaw(request.TenantID)
	wire["revision"] = json.RawMessage(`"1"`)
	wire["createdAt"] = mustRaw(now.Format(time.RFC3339Nano))
	wire["updatedAt"] = mustRaw(now.Format(time.RFC3339Nano))
	decoded, err := decodeStored(wire)
	if err != nil || decoded.Revision != 1 || decoded.Request.ID != request.ID || decoded.State != interactionapi.StateCreated {
		t.Fatalf("Record Store 往返失败: record=%+v err=%v", decoded, err)
	}

	corrupt := cloneWire(wire)
	corrupt["tenantId"] = mustRaw("tenant-b")
	if _, err := decodeStored(corrupt); err == nil {
		t.Fatal("Repository 必须拒绝数据库返回的跨租户不一致记录")
	}
}

func cloneWire(source recordstorev1.Record) recordstorev1.Record {
	result := make(recordstorev1.Record, len(source))
	for key, value := range source {
		result[key] = append(json.RawMessage(nil), value...)
	}
	return result
}
