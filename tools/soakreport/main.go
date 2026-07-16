// soakreport 校验 Backend 发布候选的长期稳定性报告。
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

type report struct {
	Commit                   string  `json:"commit"`
	RequestedDurationSeconds float64 `json:"requested_duration_seconds"`
	ElapsedDurationSeconds   float64 `json:"elapsed_duration_seconds"`
	Duration                 string  `json:"duration"`
	Calls                    uint64  `json:"calls"`
	Restarts                 uint64  `json:"restarts"`
	MaxSessionPending        int     `json:"max_session_pending"`
	BaselineGoroutines       int     `json:"baseline_goroutines"`
	FinalGoroutines          int     `json:"final_goroutines"`
	MaxGoroutines            int     `json:"max_goroutines"`
	BaselineFDs              int     `json:"baseline_fds"`
	FinalFDs                 int     `json:"final_fds"`
	MaxFDs                   int     `json:"max_fds"`
}

func main() {
	filename := flag.String("file", "", "backend-soak.json")
	minimum := flag.Duration("minimum", 24*time.Hour, "最短稳定负载时长")
	commit := flag.String("commit", "", "预期被测提交 SHA")
	flag.Parse()
	if *filename == "" || *commit == "" || flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	raw, err := os.ReadFile(*filename)
	if err != nil {
		fatal(err)
	}
	var current report
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&current); err != nil {
		fatal(fmt.Errorf("解析 soak 报告: %w", err))
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		fatal(errors.New("soak 报告只能包含一个 JSON 对象"))
	}
	if err := validateReport(current, *minimum, *commit); err != nil {
		fatal(err)
	}
	fmt.Printf("24h soak 报告有效：commit=%s calls=%d restarts=%d elapsed=%.0fs\n",
		current.Commit, current.Calls, current.Restarts, current.ElapsedDurationSeconds)
}

func validateReport(current report, minimum time.Duration, expectedCommit string) error {
	if minimum <= 0 {
		return errors.New("最短时长必须大于零")
	}
	if current.Commit == "" || current.Commit != expectedCommit {
		return fmt.Errorf("soak commit 不匹配: got=%q want=%q", current.Commit, expectedCommit)
	}
	minimumSeconds := minimum.Seconds()
	if current.RequestedDurationSeconds < minimumSeconds {
		return fmt.Errorf("请求时长不足: got=%.0fs want>=%.0fs", current.RequestedDurationSeconds, minimumSeconds)
	}
	if current.ElapsedDurationSeconds < minimumSeconds {
		return fmt.Errorf("实际时长不足: got=%.0fs want>=%.0fs", current.ElapsedDurationSeconds, minimumSeconds)
	}
	if current.Calls == 0 || current.Restarts == 0 || current.Calls < current.Restarts*5000 {
		return fmt.Errorf("稳定性负载不完整: calls=%d restarts=%d", current.Calls, current.Restarts)
	}
	if current.MaxSessionPending < 0 || current.MaxSessionPending > 1 {
		return fmt.Errorf("session pending 越界: %d", current.MaxSessionPending)
	}
	if current.BaselineGoroutines < 1 || current.FinalGoroutines > current.BaselineGoroutines+8 {
		return fmt.Errorf("goroutine 未收敛: baseline=%d final=%d", current.BaselineGoroutines, current.FinalGoroutines)
	}
	if current.MaxGoroutines < current.BaselineGoroutines {
		return fmt.Errorf("goroutine 峰值无效: baseline=%d max=%d",
			current.BaselineGoroutines, current.MaxGoroutines)
	}
	if current.BaselineFDs < 0 || current.FinalFDs < 0 || current.FinalFDs > current.BaselineFDs+8 {
		return fmt.Errorf("文件句柄未收敛: baseline=%d final=%d", current.BaselineFDs, current.FinalFDs)
	}
	if current.MaxFDs < current.BaselineFDs {
		return fmt.Errorf("文件句柄峰值无效: baseline=%d max=%d",
			current.BaselineFDs, current.MaxFDs)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
