//go:build e2e && soak

package e2e

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/internal/soakreport"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/observability"
)

func TestBackendKernelSoak(t *testing.T) {
	duration := 24 * time.Hour
	if raw := os.Getenv("VASTPLAN_SOAK_DURATION"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			t.Fatal(err)
		}
		duration = parsed
	}
	bin := buildPlugin(t, "./e2e/fixtures/plugins/legacy-v1")
	host := newHost(t, "1.0.0")
	host.Observer = observability.New(slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	allowAllPermissions(t, host)
	ctx, cancel := context.WithTimeout(context.Background(), duration+2*time.Minute)
	defer cancel()
	process, err := host.Launch(ctx, bin)
	if err != nil {
		t.Fatal(err)
	}
	baselineG, baselineFD := runtime.NumGoroutine(), openFDs()
	maxG, maxFD := baselineG, baselineFD
	started := time.Now()
	var calls, restarts uint64
	maxPending := 0
	for time.Since(started) < duration {
		response, err := host.Invoke(ctx, toolTarget("fixture.legacy-v1", "echo"), testCallContext(), []byte(`{"soak":true}`))
		if err != nil || response.Result.GetStatus() != contractv1.CallResult_STATUS_OK {
			t.Fatalf("soak 调用失败 call=%d err=%v result=%+v", calls, err, response.GetResult())
		}
		calls++
		if calls%5000 == 0 {
			if err := host.Close(process); err != nil {
				t.Fatal(err)
			}
			process, err = host.Launch(ctx, bin)
			if err != nil {
				t.Fatal(err)
			}
			restarts++
		}
		if calls%100 == 0 {
			g, fd := runtime.NumGoroutine(), openFDs()
			if g > maxG {
				maxG = g
			}
			if fd > maxFD {
				maxFD = fd
			}
			snapshot := host.DiagnosticSnapshot()
			for _, session := range snapshot.Sessions {
				if session.Pending > maxPending {
					maxPending = session.Pending
				}
				if session.Pending > 1 {
					t.Fatalf("pending 持续增长: %+v", session)
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := host.Close(process); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	elapsed := time.Since(started)
	report := soakreport.Report{
		Commit: os.Getenv("VASTPLAN_SOAK_COMMIT"), RequestedDurationSeconds: duration.Seconds(),
		ElapsedDurationSeconds: elapsed.Seconds(), Duration: elapsed.String(), Calls: calls, Restarts: restarts,
		MaxSessionPending: maxPending, BaselineGoroutines: baselineG, FinalGoroutines: runtime.NumGoroutine(),
		MaxGoroutines: maxG, BaselineFDs: baselineFD, FinalFDs: openFDs(), MaxFDs: maxFD,
	}
	if path := os.Getenv("VASTPLAN_SOAK_REPORT"); path != "" {
		raw, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("soak report: %+v", report)
	if os.Getenv("VASTPLAN_SOAK_REQUIRE_FD") == "true" && report.BaselineFDs < 0 {
		t.Fatal("发布 soak 必须能够读取文件句柄计数")
	}
	if report.FinalGoroutines > report.BaselineGoroutines+8 {
		t.Fatalf("goroutine 未收敛: %+v", report)
	}
	if report.BaselineFDs >= 0 && report.FinalFDs > report.BaselineFDs+8 {
		t.Fatalf("文件句柄未收敛: %+v", report)
	}
}

func openFDs() int {
	var entries []os.DirEntry
	var err error
	for _, path := range []string{"/proc/self/fd", "/dev/fd"} {
		entries, err = os.ReadDir(path)
		if err == nil {
			break
		}
	}
	if err != nil {
		return -1
	}
	count := 0
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err == nil {
			count++
		}
	}
	return count
}
