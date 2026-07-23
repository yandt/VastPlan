package nodebootstrapbroker

import (
	"context"
	"strings"
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/core/shared/go/operationfence"
)

type testMaterial []byte

func (m testMaterial) Bytes() []byte { return m }

type testCredentials struct{ values map[string][]byte }

func (b testCredentials) WithCredential(_ context.Context, scope kernelspi.Scope, ref kernelspi.CredentialRef, use func(kernelspi.CredentialMaterial) error) error {
	if scope.Namespace != "node-bootstrap" || scope.TenantID != "tenant-a" {
		return context.Canceled
	}
	return use(testMaterial(b.values[ref.Name]))
}

type testExecutor struct {
	identity, knownHosts, script []byte
}

func (e *testExecutor) Execute(_ context.Context, _ nodebootstrap.Target, script, identity, knownHosts []byte) error {
	e.script = append([]byte(nil), script...)
	e.identity = append([]byte(nil), identity...)
	e.knownHosts = append([]byte(nil), knownHosts...)
	return nil
}

func TestBrokerResolvesCredentialsOnlyInsideTrustedExecution(t *testing.T) {
	plan := brokerPlan()
	values := map[string][]byte{plan.SSHIdentityCredential: []byte("identity"), plan.SSHKnownHostsCredential: []byte("known-host")}
	for _, file := range plan.SecretFiles {
		values[file.Credential] = []byte("secret-material")
		if file.Destination == nodebootstrap.ArtifactTokenFile {
			values[file.Credential] = []byte("VASTPLAN_ARTIFACT_READ_TOKEN=artifact-token-1234\n")
		}
	}
	executor := &testExecutor{}
	broker, err := New(testCredentials{values: values}, executor)
	if err != nil {
		t.Fatal(err)
	}
	fence := operationfence.Fence{SchemaVersion: 1, LogicalService: "platform.deployment", UnitID: "platform-deployment", Epoch: 3, Token: "token-3", OperationID: "node-bootstrap/job-a"}
	result, err := broker.Bootstrap(context.Background(), nodebootstrap.Scope{TenantID: "tenant-a", PluginID: nodebootstrap.DeploymentManagerPluginID}, fence, plan)
	if err != nil || !result.SystemdActive || result.NodeID != "node-a" {
		t.Fatalf("可信引导失败: %+v %v", result, err)
	}
	if string(executor.identity) != "identity" || string(executor.knownHosts) != "known-host" || !strings.Contains(string(executor.script), "systemctl enable --now") || !strings.Contains(string(executor.script), "bootstrap.epoch") {
		t.Fatalf("执行器未收到受控 material 或固定脚本")
	}
}

func brokerPlan() nodebootstrap.Plan {
	node := nodebootstrap.NodeAgent{
		ID: "node-a", Tenant: "tenant-a", Deployment: "production",
		NATSURL: "tls://nats.internal:4222", NATSCA: nodebootstrap.SecretsRoot + "/nats-ca.pem", NATSCert: nodebootstrap.SecretsRoot + "/node.crt", NATSKey: nodebootstrap.SecretsRoot + "/node.key", NATSSeed: nodebootstrap.SecretsRoot + "/node.seed",
		TransportSeed: nodebootstrap.SecretsRoot + "/transport.seed", TransportTrust: nodebootstrap.SecretsRoot + "/transport-trust.json", TransportPublicKey: "UBN2AENL65VCM6XLPUDC4FGKH4EMJN2DKU2TVBDF34PRQTEG32FHOZ5G", RepositoryURL: "https://artifacts.internal", RepositoryTrust: nodebootstrap.SecretsRoot + "/artifact-trust.json",
	}
	destinations := []string{node.NATSCA, node.NATSCert, node.NATSKey, node.NATSSeed, node.TransportSeed, node.TransportTrust, node.RepositoryTrust, nodebootstrap.ArtifactTokenFile}
	plan := nodebootstrap.Plan{Target: nodebootstrap.Target{Address: "node-a.internal", User: "bootstrap"}, Release: nodebootstrap.Release{Version: "1.0.0", URL: "https://releases.internal/backend", SHA256: strings.Repeat("a", 64)}, Node: node, SSHIdentityCredential: "ssh.identity", SSHKnownHostsCredential: "ssh.known-hosts"}
	for i, destination := range destinations {
		plan.SecretFiles = append(plan.SecretFiles, nodebootstrap.CredentialSecretFile{Credential: "material-" + string(rune('a'+i)), Destination: destination, Mode: 0o440})
	}
	return plan
}
