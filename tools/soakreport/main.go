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

	internalsoak "cdsoft.com.cn/VastPlan/internal/soakreport"
)

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
	var current internalsoak.Report
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&current); err != nil {
		fatal(fmt.Errorf("解析 soak 报告: %w", err))
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		fatal(errors.New("soak 报告只能包含一个 JSON 对象"))
	}
	if err := internalsoak.Validate(current, *minimum, *commit); err != nil {
		fatal(err)
	}
	fmt.Printf("24h soak 报告有效：commit=%s calls=%d restarts=%d elapsed=%.0fs\n",
		current.Commit, current.Calls, current.Restarts, current.ElapsedDurationSeconds)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
