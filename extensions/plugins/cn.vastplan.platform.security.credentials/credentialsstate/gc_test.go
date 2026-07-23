package credentialsstate

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestChunkGCContractsRejectUnknownAndInvalidState(t *testing.T) {
	now := time.Now().UTC()
	marker, err := NewChunkGCMarker(strings.Repeat("a", 64), 7, now)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(marker)
	if parsed, err := ParseChunkGCMarker(raw); err != nil || parsed.BlobRevision != 7 {
		t.Fatalf("marker round trip 失败: %+v %v", parsed, err)
	}
	stateRaw, _ := json.Marshal(NewChunkGCState(now))
	if parsed, err := ParseChunkGCState(stateRaw); err != nil || parsed.Phase != GCPhaseMark {
		t.Fatalf("state round trip 失败: %+v %v", parsed, err)
	}
	invalid := [][]byte{
		[]byte(`{"format":"credentials.chunk-gc-marker.v1","digest":"bad","blobRevision":1,"firstObservedAt":"2026-07-23T00:00:00Z"}`),
		[]byte(`{"format":"credentials.chunk-gc-state.v1","phase":"idle","marked":0,"deleted":0}`),
		[]byte(`{"format":"credentials.chunk-gc-state.v1","phase":"mark","cursor":"gc.marker.bad","cycleStartedAt":"2026-07-23T00:00:00Z","marked":0,"deleted":0}`),
	}
	if _, err := ParseChunkGCMarker(invalid[0]); err == nil {
		t.Fatal("无效 marker 必须拒绝")
	}
	for _, candidate := range invalid[1:] {
		if _, err := ParseChunkGCState(candidate); err == nil {
			t.Fatalf("无效 state 必须拒绝: %s", candidate)
		}
	}
}
