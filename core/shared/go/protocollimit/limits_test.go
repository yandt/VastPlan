package protocollimit_test

import (
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/protocollimit"
)

func TestLimitsNormalize_ZeroValueUsesSafeDefaults(t *testing.T) {
	got := (protocollimit.Limits{}).Normalize()
	if got.MaxPayloadBytes != protocollimit.DefaultMaxPayloadBytes ||
		got.MaxPendingRequests != protocollimit.DefaultMaxPendingRequests ||
		got.MaxCallDepth != protocollimit.DefaultMaxCallDepth ||
		got.DefaultDeadline != protocollimit.DefaultDeadline {
		t.Fatalf("零值没有收敛到统一默认：%+v", got)
	}
}

func TestLimitsNormalize_PreservesExplicitOverrides(t *testing.T) {
	want := protocollimit.Limits{
		MaxPayloadBytes: 99, MaxStreamFrameBytes: 88, MaxMetadataBytes: 77,
		MaxConcurrentCalls: 6, MaxPendingRequests: 5, MaxCallDepth: 4,
		DefaultDeadline: 4 * time.Second, DrainTimeout: 3 * time.Second,
	}
	if got := want.Normalize(); got != want {
		t.Fatalf("显式配置不应被覆盖：got=%+v want=%+v", got, want)
	}
}

func TestLimitsPayloadAndEnvelopeBoundaries(t *testing.T) {
	limits := protocollimit.Limits{MaxPayloadBytes: 8, MaxStreamFrameBytes: 4}.Normalize()
	if !limits.PayloadAllowed(make([]byte, 8)) || limits.PayloadAllowed(make([]byte, 9)) {
		t.Fatal("一元 payload 边界判断错误")
	}
	if !limits.StreamFrameAllowed(make([]byte, 4)) || limits.StreamFrameAllowed(make([]byte, 5)) {
		t.Fatal("流帧边界判断错误")
	}
	if limits.MaxMessageBytes() <= limits.MaxPayloadBytes {
		t.Fatal("gRPC 消息上限必须包含 protobuf 信封余量")
	}
	limits.MaxMetadataBytes = 3
	if !limits.MetadataAllowed(3) || limits.MetadataAllowed(4) {
		t.Fatal("metadata 边界判断错误")
	}
}
