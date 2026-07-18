package portaledgecommand

import (
	"context"
	"strings"
	"testing"
)

func TestRunRejectsMissingRequiredSecurityInputs(t *testing.T) {
	err := Run(context.Background(), nil, "1.0.0", func(string, ...any) {})
	if err == nil || !strings.Contains(err.Error(), "必须配置 TLS") {
		t.Fatalf("缺少安全启动参数必须 fail-closed: %v", err)
	}
}

func TestRunRejectsUnsignedModeWithTrustStore(t *testing.T) {
	err := Run(context.Background(), []string{
		"-tls-cert", "cert", "-tls-key", "key", "-session-file", "sessions", "-composer-version", "1.0.0", "-composer-state-file", "state",
		"-allow-unsigned-local", "-trust-store", "trust.json",
	}, "1.0.0", func(string, ...any) {})
	if err == nil || !strings.Contains(err.Error(), "不能同时使用") {
		t.Fatalf("冲突的制品信任参数必须拒绝: %v", err)
	}
}
