package nodebootstrap

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestRequestValidateRequiresProductionTrustMaterial(t *testing.T) {
	request := validRequest()
	if err := request.Validate(); err != nil {
		t.Fatalf("有效生产引导请求被拒绝: %v", err)
	}

	request.Node.NATSURL = "nats://127.0.0.1:4222"
	if err := request.Validate(); err == nil || !strings.Contains(err.Error(), "tls://") {
		t.Fatalf("明文 NATS 必须被拒绝: %v", err)
	}
	request = validRequest()
	request.Target.Address = "host;reboot"
	if err := request.Validate(); err == nil {
		t.Fatal("可注入的 SSH 地址必须被拒绝")
	}
	request = validRequest()
	request.SecretFiles[0].Destination = SecretsRoot + "/ca.pem;reboot"
	if err := request.Validate(); err == nil || !strings.Contains(err.Error(), "安全的单层文件名") {
		t.Fatalf("可注入的远端文件名必须被拒绝: %v", err)
	}
	request = validRequest()
	request.SecretFiles = request.SecretFiles[:len(request.SecretFiles)-1]
	if err := request.Validate(); err == nil || !strings.Contains(err.Error(), ArtifactTokenFile) {
		t.Fatalf("缺少制品令牌环境文件必须被拒绝: %v", err)
	}
	request = validRequest()
	for len(request.SecretFiles) <= maxBootstrapSecretFiles {
		request.SecretFiles = append(request.SecretFiles, SecretFile{Source: "/secure/bootstrap/extra", Destination: SecretsRoot + "/extra", Mode: 0o440})
	}
	if err := request.Validate(); err == nil || !strings.Contains(err.Error(), "不能超过") {
		t.Fatalf("过多秘密文件必须被拒绝: %v", err)
	}
}

func TestRenderInstallScriptIsFixedAndSystemdHardened(t *testing.T) {
	request := validRequest()
	payloads := make([]SecretPayload, 0, len(request.SecretFiles))
	for _, file := range request.SecretFiles {
		content := []byte("sensitive-" + file.Destination)
		if file.Destination == ArtifactTokenFile {
			content = []byte("VASTPLAN_ARTIFACT_READ_TOKEN=artifact-secret-1234\n")
		}
		payloads = append(payloads, SecretPayload{Destination: file.Destination, Mode: file.Mode, Content: content})
	}
	script, err := RenderInstallScript(request, payloads)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(script)
	for _, expected := range []string{
		"sudo", // only the executor owns the sudo command; the script itself must not nest it.
		"groupadd --system vastplan", "usermod -a -G vastplan vastplan",
		"install -d -o root -g vastplan -m 0750 /etc/vastplan/secrets",
		"sha256sum --check --status", "systemctl enable --now vastplan-node-agent.service",
		base64.StdEncoding.EncodeToString([]byte("VASTPLAN_ARTIFACT_READ_TOKEN=artifact-secret-1234\n")),
	} {
		if expected == "sudo" {
			if strings.Contains(raw, expected) {
				t.Fatalf("引导脚本不能包含嵌套 sudo: %s", raw)
			}
			continue
		}
		if !strings.Contains(raw, expected) {
			t.Fatalf("引导脚本缺少 %q", expected)
		}
	}
	if strings.Contains(raw, "sensitive-") {
		t.Fatal("秘密明文不能出现在 SSH 脚本中")
	}
	unit, err := RenderSystemdUnit(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"NoNewPrivileges=true", "ProtectSystem=strict", "ReadWritePaths=/var/lib/vastplan", `"-third-party-plugin-policy" "require-isolation"`, `"-deployment" "prod"`} {
		if !strings.Contains(unit, expected) {
			t.Fatalf("systemd unit 缺少 %q:\n%s", expected, unit)
		}
	}
}

func validRequest() Request {
	node := NodeAgent{
		ID: "node-a", Tenant: "acme", Deployment: "prod", Labels: "region=cn,tier=platform",
		NATSURL: "tls://nats.internal:4222",
		NATSCA:  SecretsRoot + "/nats-ca.pem", NATSCert: SecretsRoot + "/node.crt", NATSKey: SecretsRoot + "/node.key", NATSSeed: SecretsRoot + "/node.seed",
		TransportSeed: SecretsRoot + "/transport.seed", TransportTrust: SecretsRoot + "/transport-trust.json", TransportPublicKey: "UBN2AENL65VCM6XLPUDC4FGKH4EMJN2DKU2TVBDF34PRQTEG32FHOZ5G",
		RepositoryURL: "https://artifacts.internal", RepositoryCA: SecretsRoot + "/artifact-ca.pem", RepositoryTrust: SecretsRoot + "/artifact-trust.json",
		CapacityCPU: 2000, CapacityMemory: 4 << 30,
	}
	destinations := []string{node.NATSCA, node.NATSCert, node.NATSKey, node.NATSSeed, node.TransportSeed, node.TransportTrust, node.RepositoryCA, node.RepositoryTrust, ArtifactTokenFile}
	files := make([]SecretFile, 0, len(destinations))
	for i, destination := range destinations {
		files = append(files, SecretFile{Source: "/secure/bootstrap/file-" + string(rune('a'+i)), Destination: destination, Mode: 0o440})
	}
	return Request{
		Target:  Target{Address: "node-a.internal", Port: 22, User: "bootstrap"},
		Release: Release{Version: "1.0.0", URL: "https://releases.internal/backend-kernel-linux-amd64", SHA256: strings.Repeat("a", 64)},
		Node:    node, SecretFiles: files,
	}
}
