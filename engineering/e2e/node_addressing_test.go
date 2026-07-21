//go:build e2e

package e2e

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nkeys"

	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

func TestNodeAddressingInvokesSecureGoCapability(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	buildNodeAddressing(t, repositoryRoot)

	server, clientTLS, caFile, certFile, keyFile := startE2ENATSMTLS(t)
	admin, err := nats.Connect(server.ClientURL(), nats.Secure(clientTLS.Clone()))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	js, err := jetstream.New(admin)
	if err != nil {
		t.Fatal(err)
	}
	buckets, err := controlplane.EnsureBuckets(context.Background(), js, 1, jetstream.MemoryStorage)
	if err != nil {
		t.Fatal(err)
	}

	workerSeed, workerIdentity := createTransportIdentity(t, "go-worker", "worker-node")
	nodeSeed, nodeIdentity := createTransportIdentity(t, "node-portal", "portal-node")
	workerIdentity.ServiceRoles = []string{"*"}
	workerIdentity.LogicalServices = []string{"*"}
	workerIdentity.AllowGlobal = true
	nodeIdentity.Role = "edge"
	nodeIdentity.ServiceRoles = []string{"backend"}
	nodeIdentity.AllowDelegation = true
	document := addressing.TransportTrustDocument{Version: 1, Identities: []addressing.TransportIdentity{workerIdentity, nodeIdentity}}

	temporary := t.TempDir()
	trustFile := writeJSONFile(t, temporary, "transport-trust.json", document, 0o600)
	workerSeedFile := writeBytesFile(t, temporary, "worker.seed", workerSeed, 0o600)
	nodeSeedFile := writeBytesFile(t, temporary, "node.seed", nodeSeed, 0o600)
	workerSecurity, err := addressing.LoadTransportSecurity(workerSeedFile, trustFile)
	if err != nil {
		t.Fatal(err)
	}
	defer workerSecurity.Close()

	workerConnection, err := nats.Connect(server.ClientURL(), nats.Secure(clientTLS.Clone()))
	if err != nil {
		t.Fatal(err)
	}
	defer workerConnection.Close()
	worker, err := addressing.NewSecureRouter(workerConnection, buckets.Capabilities, workerIdentity.NodeID, t.Logf, workerSecurity)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	const capability = "demo.node-addressing-e2e"
	registration, err := worker.Register(context.Background(), addressing.RegisterOptions{
		Capability: capability, ExtensionPoint: "tool.package", ServiceRole: "backend",
		InstancePolicy: "active-active", StateModel: "external-shared", Visibility: "service", Routing: "queue",
		UnitID: "interop", InstanceID: "go-worker-a", Version: "1.0.0",
	}, func(_ context.Context, target *contractv1.CallTarget, callContext *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if target.GetCapability() != capability || callContext.GetTenantId() != "acme" || string(payload) != "from-node" {
			t.Fatalf("Node 请求未按 Wire v1 到达 Go: target=%+v context=%+v payload=%q", target, callContext, payload)
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte("from-go"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer registration.Close(context.Background())

	fixture := filepath.Join(repositoryRoot, "engineering", "e2e", "fixtures", "node-addressing-client.mjs")
	runtime := filepath.Join(repositoryRoot, "extensions", "sdk", "node", "addressing", "dist", "index.mjs")
	contracts := filepath.Join(repositoryRoot, "contracts", "proto")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", fixture, runtime, contracts, nodeSeedFile, trustFile, server.ClientURL(), capability, caFile, certFile, keyFile)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Node -> Go Addressing E2E 失败: %v\n%s", err, output)
	}
	var response struct {
		Status  int32  `json:"status"`
		Payload string `json:"payload"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("解析 Node E2E 输出: %v\n%s", err, output)
	}
	if response.Status != int32(contractv1.CallResult_STATUS_OK) || response.Payload != "from-go" {
		t.Fatalf("Node E2E 响应错误: %+v", response)
	}
}

func startE2ENATSMTLS(t *testing.T) (*natsserver.Server, *tls.Config, string, string, string) {
	t.Helper()
	directory := t.TempDir()
	caCert, caKey, caPEM := createE2ECA(t)
	serverCertPEM, serverKeyPEM := createE2ECertificate(t, caCert, caKey, "nats-server", true)
	clientCertPEM, clientKeyPEM := createE2ECertificate(t, caCert, caKey, "node-portal", false)
	serverCertificate, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	clientCertificate, err := tls.X509KeyPair(clientCertPEM, clientKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("解析 NATS E2E CA 失败")
	}
	server, err := natsserver.NewServer(&natsserver.Options{
		JetStream: true, StoreDir: filepath.Join(directory, "jetstream"), Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverCertificate}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool},
	})
	if err != nil {
		t.Fatal(err)
	}
	go server.Start()
	if !server.ReadyForConnections(10 * time.Second) {
		t.Fatal("mTLS NATS 未就绪")
	}
	t.Cleanup(func() { server.Shutdown(); server.WaitForShutdown() })
	caFile := writeBytesFile(t, directory, "ca.pem", caPEM, 0o600)
	certFile := writeBytesFile(t, directory, "client.pem", clientCertPEM, 0o600)
	keyFile := writeBytesFile(t, directory, "client-key.pem", clientKeyPEM, 0o600)
	return server, &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool, Certificates: []tls.Certificate{clientCertificate}, ServerName: "localhost"}, caFile, certFile, keyFile
}

func createE2ECA(t *testing.T) (*x509.Certificate, *rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "VastPlan E2E CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func createE2ECertificate(t *testing.T, ca *x509.Certificate, caKey *rsa.PrivateKey, commonName string, server bool) ([]byte, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	usage := x509.ExtKeyUsageClientAuth
	if server {
		usage = x509.ExtKeyUsageServerAuth
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: commonName}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
	if server {
		template.DNSNames = []string{"localhost"}
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER := x509.MarshalPKCS1PrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyDER})
}

func buildNodeAddressing(t *testing.T, repositoryRoot string) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js 未安装，跳过 Node Addressing E2E")
	}
	if _, err := exec.LookPath("pnpm"); err != nil {
		t.Skip("pnpm 未安装，跳过 Node Addressing E2E")
	}
	command := exec.Command("pnpm", "--filter", "@vastplan/addressing-node", "build")
	command.Dir = repositoryRoot
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("构建 Node Addressing: %v\n%s", err, output)
	}
}

func createTransportIdentity(t *testing.T, name, nodeID string) ([]byte, addressing.TransportIdentity) {
	t.Helper()
	pair, err := nkeys.CreateUser()
	if err != nil {
		t.Fatal(err)
	}
	defer pair.Wipe()
	seed, err := pair.Seed()
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := pair.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	seedCopy := append([]byte(nil), seed...)
	return seedCopy, addressing.TransportIdentity{
		Name: name, Role: "node", PublicKey: publicKey, TenantID: "acme", NodeID: nodeID,
		ServiceRoles: []string{"*"}, LogicalServices: []string{"*"}, AllowedCapabilities: []string{"*"},
	}
}

func writeJSONFile(t *testing.T, directory, name string, value any, mode os.FileMode) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return writeBytesFile(t, directory, name, raw, mode)
}

func writeBytesFile(t *testing.T, directory, name string, value []byte, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(strings.TrimSpace(string(value))), mode); err != nil {
		t.Fatal(err)
	}
	return path
}
