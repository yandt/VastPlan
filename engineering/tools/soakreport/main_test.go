package main

import (
	"strings"
	"testing"
	"time"

	internalsoak "cdsoft.com.cn/VastPlan/engineering/internal/soakreport"
)

func validReport() internalsoak.Report {
	return internalsoak.Report{
		Commit: "abc123", RequestedDurationSeconds: 24 * 60 * 60,
		ElapsedDurationSeconds: 24*60*60 + 1, Duration: "24h0m1s", Calls: 10000, Restarts: 2,
		MaxSessionPending: 1, BaselineGoroutines: 10, FinalGoroutines: 11, MaxGoroutines: 15,
		BaselineFDs: 20, FinalFDs: 21, MaxFDs: 25,
	}
}

func TestValidateReport(t *testing.T) {
	if err := internalsoak.Validate(validReport(), 24*time.Hour, "abc123"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateReportRejectsIncompleteEvidence(t *testing.T) {
	tests := []struct {
		name string
		edit func(*internalsoak.Report)
		want string
	}{
		{"提交不匹配", func(value *internalsoak.Report) { value.Commit = "other" }, "commit 不匹配"},
		{"请求时长不足", func(value *internalsoak.Report) { value.RequestedDurationSeconds-- }, "请求时长不足"},
		{"实际时长不足", func(value *internalsoak.Report) { value.ElapsedDurationSeconds = 60 }, "实际时长不足"},
		{"没有调用", func(value *internalsoak.Report) { value.Calls = 0 }, "负载不完整"},
		{"没有重启", func(value *internalsoak.Report) { value.Restarts = 0 }, "负载不完整"},
		{"重启计数不可信", func(value *internalsoak.Report) { value.Restarts = 3 }, "负载不完整"},
		{"pending 增长", func(value *internalsoak.Report) { value.MaxSessionPending = 2 }, "pending 越界"},
		{"pending 为负", func(value *internalsoak.Report) { value.MaxSessionPending = -1 }, "pending 越界"},
		{"goroutine 泄漏", func(value *internalsoak.Report) { value.FinalGoroutines = 19; value.MaxGoroutines = 19 }, "goroutine 未收敛"},
		{"FD 不可读", func(value *internalsoak.Report) { value.BaselineFDs = -1 }, "文件句柄未收敛"},
		{"FD 泄漏", func(value *internalsoak.Report) { value.FinalFDs = 29; value.MaxFDs = 29 }, "文件句柄未收敛"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current := validReport()
			test.edit(&current)
			err := internalsoak.Validate(current, 24*time.Hour, "abc123")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got=%v want substring=%q", err, test.want)
			}
		})
	}
}
