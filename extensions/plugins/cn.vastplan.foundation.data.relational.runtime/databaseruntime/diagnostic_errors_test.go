package databaseruntime

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

func TestRuntimeDiagnosticLogKeepsDriverEvidenceWithoutSecrets(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)

	err := NewRuntimeError(databasev1.ErrorAuthenticationFailed, false, &pgconn.PgError{
		Code: "28P01", Message: "password authentication failed for user app at db.internal password=do-not-leak",
	})
	logRuntimeDiagnostic(&contractv1.CallContext{Trace: &contractv1.Trace{TraceId: "trace-safe", SpanId: "span-safe"}}, databasev1.OperationProbe, "postgresql", "probe", err)

	logged := output.String()
	for _, expected := range []string{"database.runtime.authentication_failed", "28P01", "trace-safe", "span-safe", "postgresql_server_rejected"} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("脱敏日志缺少诊断证据 %q: %s", expected, logged)
		}
	}
	for _, secret := range []string{"db.internal", "user app", "do-not-leak", "password="} {
		if strings.Contains(logged, secret) {
			t.Fatalf("脱敏日志泄露了 %q: %s", secret, logged)
		}
	}
}
