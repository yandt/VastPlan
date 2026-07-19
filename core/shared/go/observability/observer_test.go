package observability_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/observability"
)

func TestBeginCallDerivesSpanAndRecordsMetrics(t *testing.T) {
	metrics := observability.NewMemoryMetrics()
	observer := observability.New(nil, metrics)
	original := &contractv1.CallContext{Trace: &contractv1.Trace{TraceId: "trace-1", SpanId: "parent-1"}}
	derived, finish := observer.BeginCall(context.Background(), original, "test.invoke", nil)
	if derived.Trace.TraceId != "trace-1" || derived.Trace.GetParentSpanId() != "parent-1" || derived.Trace.SpanId == "" {
		t.Fatalf("span 派生错误: %+v", derived.Trace)
	}
	if original.Trace.ParentSpanId != nil {
		t.Fatal("不得修改调用方 CallContext")
	}
	finish("ok", nil)
	snapshot := metrics.Snapshot()
	if snapshot.Counters["kernel_calls_total|operation=test.invoke|status=ok"] != 1 {
		t.Fatalf("调用指标未记录: %+v", snapshot)
	}
}

func TestBeginCallLogsSuccessfulCallAtDebugAndFailureAtWarn(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	observer := observability.New(logger, nil)

	_, success := observer.BeginCall(context.Background(), nil, "test.success", nil)
	success("STATUS_OK", nil)
	if got := logs.String(); !strings.Contains(got, "level=DEBUG") || !strings.Contains(got, "operation=test.success") {
		t.Fatalf("成功调用应写为 DEBUG: %s", got)
	}

	logs.Reset()
	_, failure := observer.BeginCall(context.Background(), nil, "test.failure", nil)
	failure("STATUS_ERROR", nil)
	if got := logs.String(); !strings.Contains(got, "level=WARN") || !strings.Contains(got, "operation=test.failure") {
		t.Fatalf("失败调用应写为 WARN: %s", got)
	}
}
