//go:build e2e

package e2e

import (
	"context"
	"crypto/tls"
	"path/filepath"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

type portalAddressingFixture struct {
	serverURL      string
	caFile         string
	clientCertFile string
	clientKeyFile  string
	portalSeedFile string
	trustFile      string
	worker         *addressing.Router
	clientTLS      *tls.Config
}

func startPortalAddressingFixture(t *testing.T) *portalAddressingFixture {
	t.Helper()
	natsServer, clientTLS, caFile, clientCertFile, clientKeyFile := startE2ENATSMTLS(t)
	admin, err := nats.Connect(natsServer.ClientURL(), nats.Secure(clientTLS.Clone()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	js, err := jetstream.New(admin)
	if err != nil {
		t.Fatal(err)
	}
	buckets, err := controlplane.EnsureBuckets(context.Background(), js, 1, jetstream.MemoryStorage)
	if err != nil {
		t.Fatal(err)
	}
	workerSeed, workerIdentity := createTransportIdentity(t, "portal-composer", "composer-node")
	portalSeed, portalIdentity := createTransportIdentity(t, "portal-host", "portal-node")
	workerIdentity.ServiceRoles, workerIdentity.LogicalServices, workerIdentity.AllowGlobal = []string{"*"}, []string{"*"}, true
	portalIdentity.Role, portalIdentity.ServiceRoles, portalIdentity.AllowDelegation = "edge", []string{"backend"}, true
	temporary := t.TempDir()
	trustFile := writeJSONFile(t, temporary, "transport-trust.json", addressing.TransportTrustDocument{
		Version: 1, Identities: []addressing.TransportIdentity{workerIdentity, portalIdentity},
	}, 0o600)
	workerSeedFile := writeBytesFile(t, temporary, "worker.seed", workerSeed, 0o600)
	portalSeedFile := writeBytesFile(t, temporary, "portal.seed", portalSeed, 0o600)
	workerSecurity, err := addressing.LoadTransportSecurity(workerSeedFile, trustFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(workerSecurity.Close)
	workerConnection, err := nats.Connect(natsServer.ClientURL(), nats.Secure(clientTLS.Clone()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(workerConnection.Close)
	worker, err := addressing.NewSecureRouter(workerConnection, buckets.Capabilities, workerIdentity.NodeID, t.Logf, workerSecurity)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	return &portalAddressingFixture{
		serverURL: natsServer.ClientURL(), caFile: caFile, clientCertFile: clientCertFile, clientKeyFile: clientKeyFile,
		portalSeedFile: portalSeedFile, trustFile: trustFile, worker: worker, clientTLS: clientTLS,
	}
}

func (f *portalAddressingFixture) register(t *testing.T, handler addressing.InvokeHandler) {
	t.Helper()
	registration, err := f.worker.Register(context.Background(), addressing.RegisterOptions{
		Capability: "platform.portal-composer", ExtensionPoint: "tool.package", ServiceRole: "backend", RoutingDomain: "platform",
		InstancePolicy: "active-active", StateModel: "external-shared", Visibility: "service", Routing: "queue",
		UnitID: "portal-composer", InstanceID: "portal-composer-a", Version: "1.0.0",
	}, handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registration.Close(context.Background()) })
}

func (f *portalAddressingFixture) portalArguments(root string) []string {
	return []string{
		"--nats-servers", f.serverURL, "--addressing-contracts", filepath.Join(root, "contracts", "proto"),
		"--transport-seed", f.portalSeedFile, "--transport-trust", f.trustFile,
		"--nats-tls-ca", f.caFile, "--nats-tls-cert", f.clientCertFile, "--nats-tls-key", f.clientKeyFile,
	}
}

func successfulCapability(payload []byte) (*contractv1.CallResult, []byte, error) {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, nil
}
