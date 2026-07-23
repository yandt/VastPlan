package configurationauthority

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	sharedauthority "cdsoft.com.cn/VastPlan/core/shared/go/configurationauthority"
	sharedcontrolplane "cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

type catalogReader []pluginconfiguration.Catalog

func (r catalogReader) List(_ context.Context, tenant string) ([]pluginconfiguration.Catalog, error) {
	if tenant != "acme" {
		return []pluginconfiguration.Catalog{}, nil
	}
	return append([]pluginconfiguration.Catalog(nil), r...), nil
}

func TestAuthorityBindsCatalogFieldAndConsumesExactlyOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	server := startJetStream(t)
	nc, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, _ := jetstream.New(nc)
	buckets, err := sharedcontrolplane.EnsureBuckets(ctx, js, 1, jetstream.MemoryStorage)
	if err != nil {
		t.Fatal(err)
	}
	catalog := configuredCatalog(t)
	now := time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)
	store := Store{KV: buckets.ConfigurationAuthorities, Catalogs: catalogReader{catalog}, Now: func() time.Time { return now }}
	request := sharedauthority.IssueRequest{
		ConfigurationID: catalog.Items[0].ID, CatalogDigest: catalog.Digest,
		CandidateID: "pcfg_" + strings.Repeat("a", 32), FieldID: "token",
	}
	issued, err := store.Issue(ctx, "acme", request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(issued.Token, catalog.Items[0].PluginID) || !strings.HasPrefix(issued.Token, sharedauthority.TokenPrefix) {
		t.Fatalf("授权票据必须随机且不泄漏 owner: %q", issued.Token)
	}
	claims, err := store.Consume(ctx, "acme", issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Owner != catalog.Items[0].PluginID || claims.Purpose != "remote.token" || claims.FieldID != "token" || claims.CandidateID != request.CandidateID || claims.ArtifactSHA256 != catalog.Items[0].Artifact.SHA256 {
		t.Fatalf("授权未绑定活动目录事实: %+v", claims)
	}
	if _, err := store.Consume(ctx, "acme", issued.Token); !errors.Is(err, sharedauthority.ErrAlreadyConsumed) {
		t.Fatalf("配置授权必须只能消费一次: %v", err)
	}
}

func TestAuthorityRejectsUnknownFieldWrongTenantAndExpiry(t *testing.T) {
	ctx := context.Background()
	server := startJetStream(t)
	nc, _ := nats.Connect(server.ClientURL())
	defer nc.Close()
	js, _ := jetstream.New(nc)
	buckets, _ := sharedcontrolplane.EnsureBuckets(ctx, js, 1, jetstream.MemoryStorage)
	catalog := configuredCatalog(t)
	now := time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)
	store := Store{KV: buckets.ConfigurationAuthorities, Catalogs: catalogReader{catalog}, Now: func() time.Time { return now }}
	base := sharedauthority.IssueRequest{ConfigurationID: catalog.Items[0].ID, CatalogDigest: catalog.Digest, CandidateID: "pcfg_" + strings.Repeat("b", 32), FieldID: "unknown"}
	if _, err := store.Issue(ctx, "acme", base); !errors.Is(err, sharedauthority.ErrInvalid) {
		t.Fatalf("不得签发清单未声明字段: %v", err)
	}
	base.FieldID = "token"
	issued, err := store.Issue(ctx, "acme", base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Consume(ctx, "other", issued.Token); !errors.Is(err, sharedauthority.ErrNotFound) {
		t.Fatalf("跨 tenant 不得消费授权: %v", err)
	}
	now = now.Add(sharedauthority.DefaultTTL)
	if _, err := store.Consume(ctx, "acme", issued.Token); !errors.Is(err, sharedauthority.ErrExpired) {
		t.Fatalf("过期授权必须拒绝: %v", err)
	}
}

func TestConcurrentConsumeHasSingleWinner(t *testing.T) {
	ctx := context.Background()
	server := startJetStream(t)
	nc, _ := nats.Connect(server.ClientURL())
	defer nc.Close()
	js, _ := jetstream.New(nc)
	buckets, _ := sharedcontrolplane.EnsureBuckets(ctx, js, 1, jetstream.MemoryStorage)
	catalog := configuredCatalog(t)
	store := Store{KV: buckets.ConfigurationAuthorities, Catalogs: catalogReader{catalog}}
	issued, err := store.Issue(ctx, "acme", sharedauthority.IssueRequest{ConfigurationID: catalog.Items[0].ID, CatalogDigest: catalog.Digest, CandidateID: "pcfg_" + strings.Repeat("c", 32), FieldID: "token"})
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	wins := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := store.Consume(ctx, "acme", issued.Token)
			wins <- err
		}()
	}
	wait.Wait()
	close(wins)
	successes := 0
	for err := range wins {
		if err == nil {
			successes++
		} else if !errors.Is(err, sharedauthority.ErrAlreadyConsumed) {
			t.Fatalf("并发消费返回意外错误: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("并发消费必须恰好一个成功，实际 %d", successes)
	}
}

func configuredCatalog(t *testing.T) pluginconfiguration.Catalog {
	t.Helper()
	const pluginID = "com.example.configured"
	manifest := []byte(fmt.Sprintf(`{
		"id":%q,"name":"Configured","description":"configured","version":"1.0.0","publisher":"example",
		"engines":{"backend":"^1.0"},
		"capabilities":{"kernelServices":["kernel.config.credential-ref"]},
		"configuration":{"scope":"service","applyMode":"restart","schema":{"type":"object","additionalProperties":false,"properties":{}},"managedCredentials":[{"id":"token","title":"Token","purpose":"remote.token","required":true}]},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}
	}`, pluginID))
	ref := pluginv1.ArtifactRef{PluginID: pluginID, Version: "1.0.0", Channel: "stable"}
	deployment := deploymentv2.Deployment{
		Version: 2, Revision: 1, Metadata: deploymentv1.Metadata{Name: "services", Tenant: "acme"},
		Resolution: deploymentv2.Resolution{PluginOrigins: map[string]string{pluginID: deploymentv2.OriginApplication}},
		Units: []deploymentv2.ServiceUnit{{ID: "api", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{{ID: pluginID, Version: "1.0.0", Channel: "stable"}}, Config: map[string]any{"plugins": map[string]any{pluginID: map[string]any{}}},
		}},
	}
	catalog, err := pluginconfiguration.Build(deployment, map[pluginv1.ArtifactRef]pluginv1.Artifact{ref: {PluginID: pluginID, Version: "1.0.0", Channel: "stable", SHA256: strings.Repeat("d", 64), Manifest: manifest}})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func startJetStream(t *testing.T) *natsserver.Server {
	t.Helper()
	server, err := natsserver.NewServer(&natsserver.Options{JetStream: true, StoreDir: filepath.Join(t.TempDir(), "jetstream"), Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	server.Start()
	if !server.ReadyForConnections(5 * time.Second) {
		server.Shutdown()
		t.Fatal("NATS 未就绪")
	}
	t.Cleanup(func() { server.Shutdown(); server.WaitForShutdown() })
	return server
}
