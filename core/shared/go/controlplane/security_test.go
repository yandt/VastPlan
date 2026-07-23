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
	roles := []SecurityRole{RoleBootstrap, RoleCatalogPublisher, RoleController, RoleNode, RoleManager, RoleRuntime}
	identities := make([]NKeyIdentity, 0, len(roles))
	seedFiles := map[SecurityRole]string{}
	for _, role := range roles {
		publicKey, seed := generateNKey(t)
		nodeID, tenant, deployment := "", "", ""
		if role == RoleNode || role == RoleManager {
			nodeID = "node-1"
			if role == RoleManager {
				nodeID = "manager-1"
			}
			tenant, deployment = "acme", "prod"
		}
		catalogID := ""
		if role == RoleCatalogPublisher {
			catalogID = "backend-production"
		}
		identities = append(identities, NKeyIdentity{Role: role, PublicKey: publicKey, TenantID: tenant, Deployment: deployment, NodeID: nodeID, CatalogID: catalogID})
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
	catalogPublisher := connect(RoleCatalogPublisher)
	controller := connect(RoleController)
	node := connect(RoleNode)
	manager := connect(RoleManager)
	runtime := connect(RoleRuntime)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	bootstrapJS, _ := jetstream.New(bootstrap)
	bootstrapBuckets, err := EnsureBuckets(ctx, bootstrapJS, 1, jetstream.MemoryStorage)
	if err != nil {
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
	if _, err := bootstrapBuckets.Deployments.Put(ctx, DeploymentKey("acme", "prod"), []byte("deployment")); err != nil {
		t.Fatalf("bootstrap 应能发布 deployment KV: %v", err)
	}
	if entry, err := nodeBuckets.Deployments.Get(ctx, DeploymentKey("acme", "prod")); err != nil || string(entry.Value()) != "deployment" {
		t.Fatalf("可信节点内核应能读取 deployment 与配置目录 sidecar: entry=%v err=%v", entry, err)
	}
	authorityKey := ConfigurationAuthorityKey("acme", strings.Repeat("a", 64))
	if _, err := nodeBuckets.ConfigurationAuthorities.Put(ctx, authorityKey, []byte("hashed-ticket")); err != nil {
		t.Fatalf("可信节点内核应能写本 tenant 的一次性配置授权: %v", err)
	}
	if _, err := nodeBuckets.Actual.Put(ctx, ActualKey("acme", "prod", "node-1"), []byte("healthy")); err != nil {
		t.Fatalf("node 应能写 actual KV: %v", err)
	}
	leaseKey := NodeKey("acme", "prod", "node-1")
	if _, err := nodeBuckets.Nodes.Put(ctx, leaseKey, []byte("signed-lease")); err != nil {
		t.Fatalf("node 应能写自身作用域 lease: %v", err)
	}
	managerJS, _ := jetstream.New(manager)
	managerBuckets, err := OpenBuckets(ctx, managerJS)
	if err != nil {
		t.Fatalf("manager-node 应能打开 buckets: %v", err)
	}
	if entry, err := managerBuckets.Nodes.Get(ctx, leaseKey); err != nil || string(entry.Value()) != "signed-lease" {
		t.Fatalf("manager-node 应能读取节点 lease: entry=%v err=%v", entry, err)
	}
	if entry, err := managerBuckets.ConfigurationAuthorities.Get(ctx, authorityKey); err != nil || string(entry.Value()) != "hashed-ticket" {
		t.Fatalf("同 tenant 的可信内核应能消费配置授权: entry=%v err=%v", entry, err)
	}
	catalogKey := BackendPlatformCatalogKey("backend-production")
	if _, err := bootstrapBuckets.BackendPlatformCatalogs.Put(ctx, catalogKey, []byte("catalog-snapshot")); err != nil {
		t.Fatalf("bootstrap 应能发布 Backend Platform Catalog Seed: %v", err)
	}
	if entry, err := managerBuckets.BackendPlatformCatalogs.Get(ctx, catalogKey); err != nil || string(entry.Value()) != "catalog-snapshot" {
		t.Fatalf("manager-node 应能读取 Backend Platform Catalog 快照: entry=%v err=%v", entry, err)
	}
	catalogPublisherJS, _ := jetstream.New(catalogPublisher)
	catalogPublisherKV, err := catalogPublisherJS.KeyValue(ctx, BackendPlatformCatalogsBucket)
	if err != nil {
		t.Fatalf("catalog-publisher 应能打开专用 Catalog bucket: %v", err)
	}
	if _, err := catalogPublisherKV.Put(ctx, catalogKey, []byte("candidate-snapshot")); err != nil {
		t.Fatalf("catalog-publisher 应能写专用 Catalog bucket: %v", err)
	}
	otherCatalogDeniedCtx, otherCatalogDeniedCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer otherCatalogDeniedCancel()
	if _, err := catalogPublisherKV.Put(otherCatalogDeniedCtx, BackendPlatformCatalogKey("other-catalog"), []byte("forbidden")); err == nil {
		t.Fatal("catalog-publisher 不得写其他 Catalog ID")
	}
	catalogPublisherDeniedCtx, catalogPublisherDeniedCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer catalogPublisherDeniedCancel()
	if _, err := catalogPublisherJS.KeyValue(catalogPublisherDeniedCtx, DeploymentsBucket); err == nil {
		t.Fatal("catalog-publisher 不得打开 Deployment bucket")
	}
	managerDeniedCtx, managerDeniedCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer managerDeniedCancel()
	if _, err := managerBuckets.BackendPlatformCatalogs.Put(managerDeniedCtx, "forbidden", []byte("tampered")); err == nil {
		t.Fatal("manager-node 不得使用运行身份写 Backend Platform Catalog")
	}
	otherAuthorityCtx, otherAuthorityCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer otherAuthorityCancel()
	if _, err := nodeBuckets.ConfigurationAuthorities.Put(otherAuthorityCtx, ConfigurationAuthorityKey("other", strings.Repeat("b", 64)), []byte("forbidden")); err == nil {
		t.Fatal("节点不得为其他 tenant 写配置授权")
	}
	runtimeJS, _ := jetstream.New(runtime)
	runtimeMetrics, err := runtimeJS.KeyValue(ctx, AutoscalingBucket)
	if err != nil {
		t.Fatalf("runtime 应能打开自动伸缩指标 bucket: %v", err)
	}
	metric := AutoscalingMetric{Tenant: "acme", Deployment: "prod", Unit: "api", Metric: "queue.depth", Value: 42}
	if err := PublishAutoscalingMetric(ctx, runtimeMetrics, metric); err != nil {
		t.Fatalf("runtime 应能发布自动伸缩指标: %v", err)
	}
	if got, err := ReadAutoscalingMetric(ctx, controllerBuckets.Autoscaling, "acme", "prod", "api", "queue.depth"); err != nil || got.Value != 42 {
		t.Fatalf("controller 应能读取自动伸缩指标: got=%+v err=%v", got, err)
	}
	nodeDeniedCtx, nodeDeniedCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer nodeDeniedCancel()
	if _, err := nodeBuckets.Deployments.Put(nodeDeniedCtx, "forbidden", []byte("tampered")); err == nil {
		t.Fatal("node 不得写全局 deployment KV")
	}
	controllerDeniedCtx, controllerDeniedCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer controllerDeniedCancel()
	if _, err := controllerBuckets.Autoscaling.Put(controllerDeniedCtx, "forbidden", []byte("metric")); err == nil {
		t.Fatal("controller 不得伪造自动伸缩指标")
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

	actual := "$KV." + ActualBucket + "." + ActualKey("acme", "prod", "node-1")
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
	acl, err := RoleACLForIdentity(NKeyIdentity{Role: RoleNode, TenantID: "acme", Deployment: "prod", NodeID: "node-1"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(acl.PublishDeny, "\n")
	for _, bucket := range []string{DesiredBucket, DeploymentsBucket, AssignmentsBucket} {
		if !strings.Contains(joined, "$KV."+bucket+".>") {
			t.Fatalf("node publish deny 缺少 %s", bucket)
		}
	}
	for _, subject := range acl.PublishAllow {
		if strings.Contains(subject, "$JS.API.CONSUMER.DELETE") {
			t.Fatalf("node ACL 不得删除任意 consumer: %s", subject)
		}
	}
}

func TestRoleACL_ManagerNodeCanReadLeasesWithoutGlobalWrites(t *testing.T) {
	manager, err := RoleACLForIdentity(NKeyIdentity{Role: RoleManager, TenantID: "acme", Deployment: "prod", NodeID: "manager-1"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(manager.PublishAllow, "\n")
	if !strings.Contains(joined, "$JS.API.DIRECT.GET.KV_"+NodesBucket) {
		t.Fatal("manager-node 必须能读取节点 lease 以执行可信就绪观察")
	}
	for _, subject := range manager.PublishAllow {
		if strings.HasPrefix(subject, "$KV."+DeploymentsBucket+".") || strings.HasPrefix(subject, "$KV."+AssignmentsBucket+".") || strings.HasPrefix(subject, "$KV."+BackendPlatformCatalogsBucket+".") {
			t.Fatalf("manager-node 不得获得全局期望态写权限: %s", subject)
		}
	}
}

func TestRoleACL_CatalogPublisherCanOnlyMutateCatalogBucket(t *testing.T) {
	publisher, err := RoleACLForIdentity(NKeyIdentity{Role: RoleCatalogPublisher, CatalogID: "backend-production"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(publisher.PublishAllow, "\n")
	exactSubject := "$KV." + BackendPlatformCatalogsBucket + "." + BackendPlatformCatalogKey("backend-production")
	if !strings.Contains(joined, exactSubject) {
		t.Fatal("catalog-publisher 必须能写 Backend Platform Catalog bucket")
	}
	if strings.Contains(joined, "$KV."+BackendPlatformCatalogsBucket+".>") {
		t.Fatal("catalog-publisher 不得获得 Catalog bucket 通配写权限")
	}
	for _, forbidden := range []string{DeploymentsBucket, DesiredBucket, AssignmentsBucket, ConfigurationAuthoritiesBucket, EventsStream} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("catalog-publisher 获得了越界 Subject: %s", forbidden)
		}
	}
	if _, err := RoleACL(RoleCatalogPublisher); err == nil {
		t.Fatal("未绑定 Catalog ID 不得生成 publisher ACL")
	}
}

func TestNATSSecurityRejectsDuplicateGlobalNodeID(t *testing.T) {
	first, _ := generateNKey(t)
	second, _ := generateNKey(t)
	system, _ := generateNKey(t)
	_, err := RenderNATSServerConfig(ServerSecurityConfig{
		StoreDir: "/tmp/nats", TLSCertFile: "/tmp/server.crt", TLSKeyFile: "/tmp/server.key", TLSCAFile: "/tmp/ca.crt", SystemPublicKey: system,
		Identities: []NKeyIdentity{{Role: RoleNode, TenantID: "acme", Deployment: "prod", NodeID: "node-a", PublicKey: first}, {Role: RoleManager, TenantID: "acme", Deployment: "prod", NodeID: "node-a", PublicKey: second}},
	})
	if err == nil || !strings.Contains(err.Error(), "全局唯一") {
		t.Fatalf("重复 node id 必须在生成 NATS 配置时拒绝: %v", err)
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
