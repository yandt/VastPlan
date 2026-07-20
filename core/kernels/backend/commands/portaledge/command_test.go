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
		"-tls-cert", "cert", "-tls-key", "key", "-session-file", "sessions", "-composer-version", "1.0.0", "-composer-state-file", "state", "-portal-platform-catalog", "catalog.json", "-interaction-broker-version", "0.1.0", "-interaction-broker-state-file", "broker-state",
		"-portal-assets", "assets",
		"-frontend-delivery-origin", "delivery-origin",
		"-allow-unsigned-local", "-trust-store", "trust.json",
	}, "1.0.0", func(string, ...any) {})
	if err == nil || !strings.Contains(err.Error(), "不能同时使用") {
		t.Fatalf("冲突的制品信任参数必须拒绝: %v", err)
	}
}

func TestPortalRemoteArtifactSourceRequiresCompleteConfiguration(t *testing.T) {
	if source, trust, err := buildPortalRemoteArtifactSource("", "", "", ""); err != nil || source != nil || trust != nil {
		t.Fatalf("未配置远端源应保持兼容: source=%v trust=%v err=%v", source, trust, err)
	}
	if _, _, err := buildPortalRemoteArtifactSource("https://repository.local", "", "token", ""); err == nil || !strings.Contains(err.Error(), "trust") {
		t.Fatalf("远端源缺少信任必须拒绝: %v", err)
	}
	if _, _, err := buildPortalRemoteArtifactSource("", "trust.json", "", ""); err == nil || !strings.Contains(err.Error(), "-repository-url") {
		t.Fatalf("孤立远端参数必须拒绝: %v", err)
	}
}

func TestPlatformRouterOptionsRequireProductionTransportIdentity(t *testing.T) {
	if err := (platformRouterOptions{URL: "tls://nats:4222", NodeID: "portal-edge"}).validate(); err == nil || !strings.Contains(err.Error(), "传输身份") {
		t.Fatalf("生产平台管理调用缺少传输身份必须拒绝: %v", err)
	}
	if err := (platformRouterOptions{URL: "nats://127.0.0.1:4222", NodeID: "portal-edge", AllowInsecure: true}).validate(); err != nil {
		t.Fatalf("显式本地不安全模式应允许测试: %v", err)
	}
}
