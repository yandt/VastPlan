package controlplane

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nkeys"
)

func TestConnectWithConfig_FailsClosedOnPartialSecurity(t *testing.T) {
	if _, err := ConnectWithConfig(ConnectionConfig{URL: "nats://127.0.0.1:4222"}); err == nil {
		t.Fatal("未显式声明开发模式时不得回退明文 NATS")
	}
	if _, err := ConnectWithConfig(ConnectionConfig{
		URL: "tls://127.0.0.1:4222", CAFile: "ca.pem", CertFile: "client.pem",
		KeyFile: "client.key", SeedFile: "node.seed", Insecure: true,
	}); err == nil {
		t.Fatal("开发模式不得与半套安全参数混用")
	}
}

func TestNATSSecurity_mTLSNKeyAndRoleSubjectACL(t *testing.T) {
	files := generateTestCertificates(t)
	roles := []SecurityRole{RoleBootstrap, RoleController, RoleNode, RoleRuntime}
	identities := make([]NKeyIdentity, 0, len(roles))
	seedFiles := map[SecurityRole]string{}
	for _, role := range roles {
		publicKey, seed := generateNKey(t)
		identities = append(identities, NKeyIdentity{Role: role, PublicKey: publicKey})
		seedFiles[role] = writeTestFile(t, string(role)+".seed", append(seed, '\n'), 0o600)
	}
	systemPublic, _ := generateNKey(t)
	config, err := RenderNATSServerConfig(ServerSecurityConfig{
		Listen: "127.0.0.1:-1", StoreDir: filepath.Join(t.TempDir(), "jetstream"),
		TLSCertFile: files.serverCert, TLSKeyFile: files.serverKey, TLSCAFile: files.ca,
		SystemPublicKey: systemPublic, Identities: identities,
	})
	if err != nil {
		t.Fatal(err)
	}
	configFile := writeTestFile(t, "nats-server.conf", []byte(config), 0o600)
	options, err := natsserver.ProcessConfigFile(configFile)
	if err != nil {
		t.Fatalf("生成的 NATS 配置无法解析: %v\n%s", err, config)
	}
	options.NoLog, options.NoSigs = true, true
	server, err := natsserver.NewServer(options)
	if err != nil {
		t.Fatal(err)
	}
	go server.Start()
	if !server.ReadyForConnections(10 * time.Second) {
		t.Fatal("mTLS NATS Server 未就绪")
	}
	t.Cleanup(func() { server.Shutdown(); server.WaitForShutdown() })
	address := server.Addr().(*net.TCPAddr)
	url := "tls://127.0.0.1:" + big.NewInt(int64(address.Port)).String()
	connect := func(role SecurityRole) *nats.Conn {
		t.Helper()
		nc, err := ConnectWithConfig(ConnectionConfig{
			URL: url, ClientName: string(role), CAFile: files.ca,
			CertFile: files.clientCert, KeyFile: files.clientKey, SeedFile: seedFiles[role],
		})
		if err != nil {
			t.Fatalf("%s 安全连接失败: %v", role, err)
		}
		t.Cleanup(nc.Close)
		return nc
	}
	bootstrap := connect(RoleBootstrap)
	controller := connect(RoleController)
	node := connect(RoleNode)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bootstrapJS, _ := jetstream.New(bootstrap)
	if _, err := EnsureBuckets(ctx, bootstrapJS, 1, jetstream.MemoryStorage); err != nil {
		t.Fatalf("bootstrap 应能创建控制面 buckets: %v", err)
	}
	controllerJS, _ := jetstream.New(controller)
	controllerBuckets, err := OpenBuckets(ctx, controllerJS)
	if err != nil {
		t.Fatalf("controller 应能打开 buckets: %v", err)
	}
	nodeJS, _ := jetstream.New(node)
	nodeBuckets, err := OpenBuckets(ctx, nodeJS)
	if err != nil {
		t.Fatalf("node 应能打开 buckets: %v", err)
	}
	if _, err := controllerBuckets.Assignments.Put(ctx, "tenant.unit", []byte("assignment")); err != nil {
		t.Fatalf("controller 应能发布 assignment KV: %v", err)
	}
	if entry, err := nodeBuckets.Assignments.Get(ctx, "tenant.unit"); err != nil || string(entry.Value()) != "assignment" {
		t.Fatalf("node 应能读取 assignment KV: entry=%v err=%v", entry, err)
	}
	if _, err := nodeBuckets.Actual.Put(ctx, "node-1", []byte("healthy")); err != nil {
		t.Fatalf("node 应能写 actual KV: %v", err)
	}
	if _, err := nodeBuckets.Deployments.Put(ctx, "forbidden", []byte("tampered")); err == nil {
		t.Fatal("node 不得写全局 deployment KV")
	}

	assignment := "$KV." + AssignmentsBucket + ".tenant.unit"
	assignmentMessages := make(chan []byte, 4)
	if _, err := bootstrap.Subscribe(assignment, func(message *nats.Msg) { assignmentMessages <- message.Data }); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := controller.Publish(assignment, []byte("allowed")); err != nil {
		t.Fatal(err)
	}
	if err := controller.Flush(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-assignmentMessages:
		if string(got) != "allowed" {
			t.Fatalf("controller assignment=%q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("controller 的授权 assignment 未送达")
	}

	if err := node.Publish(assignment, []byte("forbidden")); err != nil {
		t.Fatal(err)
	}
	_ = node.Flush()
	select {
	case got := <-assignmentMessages:
		t.Fatalf("node 越权发布 assignment 被送达: %q", got)
	case <-time.After(200 * time.Millisecond):
	}

	actual := "$KV." + ActualBucket + ".node-1"
	actualMessages := make(chan []byte, 1)
	_, _ = bootstrap.Subscribe(actual, func(message *nats.Msg) { actualMessages <- message.Data })
	_ = bootstrap.Flush()
	if err := node.Publish(actual, []byte("healthy")); err != nil {
		t.Fatal(err)
	}
	_ = node.Flush()
	select {
	case got := <-actualMessages:
		if string(got) != "healthy" {
			t.Fatalf("node actual=%q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("node 的授权 actual 未送达")
	}
}

func TestRoleACL_NodeCannotMutateGlobalIntent(t *testing.T) {
	acl, err := RoleACL(RoleNode)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(acl.PublishDeny, "\n")
	for _, bucket := range []string{DesiredBucket, DeploymentsBucket, AssignmentsBucket} {
		if !strings.Contains(joined, "$KV."+bucket+".>") {
			t.Fatalf("node publish deny 缺少 %s", bucket)
		}
	}
}

type testCertificates struct{ ca, serverCert, serverKey, clientCert, clientKey string }

func generateTestCertificates(t *testing.T) testCertificates {
	t.Helper()
	caPublic, caPrivate, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().Add(-time.Hour)
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "VastPlan Test CA"},
		NotBefore: now, NotAfter: now.Add(24 * time.Hour), IsCA: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caPublic, caPrivate)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(caDER)
	caFile := writeTestFile(t, "ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o644)
	serverCert, serverKey := issueCertificate(t, caCert, caPrivate, 2, "127.0.0.1", []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, []net.IP{net.ParseIP("127.0.0.1")})
	clientCert, clientKey := issueCertificate(t, caCert, caPrivate, 3, "vastplan-test-client", []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, nil)
	return testCertificates{
		ca:         caFile,
		serverCert: writeTestFile(t, "server.pem", serverCert, 0o644),
		serverKey:  writeTestFile(t, "server-key.pem", serverKey, 0o600),
		clientCert: writeTestFile(t, "client.pem", clientCert, 0o644),
		clientKey:  writeTestFile(t, "client-key.pem", clientKey, 0o600),
	}
}

func issueCertificate(t *testing.T, ca *x509.Certificate, caPrivate ed25519.PrivateKey, serial int64, commonName string, usages []x509.ExtKeyUsage, ips []net.IP) ([]byte, []byte) {
	t.Helper()
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: commonName},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(12 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: usages, IPAddresses: ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, publicKey, caPrivate)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
}

func generateNKey(t *testing.T) (string, []byte) {
	t.Helper()
	pair, err := nkeys.CreateUser()
	if err != nil {
		t.Fatal(err)
	}
	defer pair.Wipe()
	publicKey, _ := pair.PublicKey()
	seed, _ := pair.Seed()
	return publicKey, append([]byte(nil), seed...)
}

func writeTestFile(t *testing.T, name string, content []byte, mode os.FileMode) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(filename, content, mode); err != nil {
		t.Fatal(err)
	}
	return filename
}
