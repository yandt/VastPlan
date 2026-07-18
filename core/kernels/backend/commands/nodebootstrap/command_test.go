package nodebootstrapcommand

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
)

type fakeExecutor struct {
	target nodebootstrap.Target
	script []byte
	err    error
}

func (f *fakeExecutor) Execute(_ context.Context, target nodebootstrap.Target, script []byte) error {
	f.target = target
	f.script = append([]byte(nil), script...)
	return f.err
}

func TestRunHelpAndPreflight(t *testing.T) {
	var output bytes.Buffer
	if err := Run(context.Background(), []string{"-help"}, &output, &output); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help 应返回标准 flag 错误: %v", err)
	}
	if err := Run(context.Background(), nil, &output, &output); err == nil || !strings.Contains(err.Error(), "-request") {
		t.Fatalf("缺少引导边界参数必须失败: %v", err)
	}
}

func TestExecuteRendersAndRunsFixedBootstrap(t *testing.T) {
	request := commandRequest(t)
	executor := &fakeExecutor{}
	var output bytes.Buffer
	if err := execute(context.Background(), request, executor, time.Minute, &output); err != nil {
		t.Fatal(err)
	}
	if executor.target.Address != request.Target.Address || !bytes.Contains(executor.script, []byte("systemctl enable --now vastplan-node-agent.service")) {
		t.Fatalf("未执行固定 systemd 引导: target=%+v", executor.target)
	}
	if strings.Contains(string(executor.script), "artifact-secret") {
		t.Fatal("引导脚本不能包含秘密明文")
	}
	if !strings.Contains(output.String(), `"status":"systemd_active"`) || !strings.Contains(output.String(), `"nodeId":"node-a"`) {
		t.Fatalf("完成输出无效: %s", output.String())
	}
}

func TestLoadRequestRejectsUnknownAndTrailingJSON(t *testing.T) {
	request := commandRequest(t)
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{
		"unknown":  append(raw[:len(raw)-1], []byte(`,"command":"reboot"}`)...),
		"trailing": append(append([]byte(nil), raw...), []byte(` {}`)...),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "request.json")
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadRequest(path); err == nil {
				t.Fatal("额外字段或第二个 JSON 文档必须被拒绝")
			}
		})
	}
}

func commandRequest(t *testing.T) nodebootstrap.Request {
	t.Helper()
	dir := t.TempDir()
	node := nodebootstrap.NodeAgent{
		ID: "node-a", Tenant: "acme", Deployment: "prod", Labels: "region=cn",
		NATSURL: "tls://nats.internal:4222",
		NATSCA:  nodebootstrap.SecretsRoot + "/nats-ca.pem", NATSCert: nodebootstrap.SecretsRoot + "/node.crt", NATSKey: nodebootstrap.SecretsRoot + "/node.key", NATSSeed: nodebootstrap.SecretsRoot + "/node.seed",
		TransportSeed: nodebootstrap.SecretsRoot + "/transport.seed", TransportTrust: nodebootstrap.SecretsRoot + "/transport-trust.json", TransportPublicKey: "UBN2AENL65VCM6XLPUDC4FGKH4EMJN2DKU2TVBDF34PRQTEG32FHOZ5G",
		RepositoryURL: "https://artifacts.internal", RepositoryTrust: nodebootstrap.SecretsRoot + "/artifact-trust.json",
	}
	destinations := []string{node.NATSCA, node.NATSCert, node.NATSKey, node.NATSSeed, node.TransportSeed, node.TransportTrust, node.RepositoryTrust, nodebootstrap.ArtifactTokenFile}
	files := make([]nodebootstrap.SecretFile, 0, len(destinations))
	for i, destination := range destinations {
		source := filepath.Join(dir, "secret-"+string(rune('a'+i)))
		content := "secret-value"
		if destination == nodebootstrap.ArtifactTokenFile {
			content = "VASTPLAN_ARTIFACT_READ_TOKEN=artifact-secret-1234\n"
		}
		if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		files = append(files, nodebootstrap.SecretFile{Source: source, Destination: destination, Mode: 0o440})
	}
	return nodebootstrap.Request{
		Target:  nodebootstrap.Target{Address: "node-a.internal", User: "bootstrap"},
		Release: nodebootstrap.Release{Version: "1.0.0", URL: "https://releases.internal/backend-kernel-linux-amd64", SHA256: strings.Repeat("a", 64)},
		Node:    node, SecretFiles: files,
	}
}
