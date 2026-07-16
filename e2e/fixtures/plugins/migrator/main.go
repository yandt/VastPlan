// Package main 是实现 Backend 状态迁移三阶段协议的 v2 跨进程夹具。
package main

import (
	"context"
	"fmt"
	"os"

	"cdsoft.com.cn/VastPlan/sdk/go/plugin"
)

func main() {
	p := plugin.New("com.vastplan.fixture.migrator", "2.0.0", map[string]string{"backend": "^0.1 || ^1.0"})
	p.OnMigration(func(_ context.Context, phase plugin.MigrationPhase, request plugin.MigrationRequest) error {
		path := os.Getenv("VASTPLAN_MIGRATION_LOG")
		if path == "" {
			return fmt.Errorf("缺少 VASTPLAN_MIGRATION_LOG")
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := fmt.Fprintf(f, "%s %s %s@%d %s@%d\n", phase, request.TransactionID,
			request.From.Format, request.From.FormatVersion, request.To.Format, request.To.FormatVersion)
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
		if os.Getenv("VASTPLAN_MIGRATION_FAIL") == string(phase) {
			return fmt.Errorf("fixture rejects %s", phase)
		}
		return nil
	})
	if err := p.Serve(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
