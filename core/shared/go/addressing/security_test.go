package addressing

import (
	"reflect"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

func newTestTransportSecurity(t *testing.T) (*TransportSecurity, TransportIdentity) {
	t.Helper()
	pair, err := nkeys.CreateUser()
	if err != nil {
		t.Fatal(err)
	}
	seed, err := pair.Seed()
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := pair.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	identity := TransportIdentity{
		Name: "node-a", Role: "node", PublicKey: publicKey, TenantID: "tenant-a", NodeID: "node-a",
		ServiceRoles: []string{"*"}, LogicalServices: []string{"*"}, AllowedCapabilities: []string{"*"}, AllowGlobal: true,
	}
	security, err := newTransportSecurity(seed, TransportTrustDocument{
		Version: 1, Identities: []TransportIdentity{identity},
	})
	if err != nil {
		t.Fatal(err)
	}
	return security, identity
}

func TestTransportSecuritySignsAndDetectsReplay(t *testing.T) {
	security, identity := newTestTransportSecurity(t)
	defer security.Close()

	message := nats.NewMsg("vp.rpc.v1.task.run")
	message.Data = []byte("payload")
	if err := security.signMessage(message); err != nil {
		t.Fatal(err)
	}
	got, err := security.verifyMessage(message)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, identity) {
		t.Fatalf("verified identity = %+v, want %+v", got, identity)
	}
	if _, err := security.verifyMessage(message); err == nil {
		t.Fatal("replayed transport message was accepted")
	}
	if _, err := security.verifyNoReplay(message.Subject, message.Data, transportHeaderValues(message.Header)); err != nil {
		t.Fatalf("JetStream redelivery should bypass nonce replay check: %v", err)
	}
}

func TestTransportSecurityAttestsNodeLeaseIdentity(t *testing.T) {
	security, identity := newTestTransportSecurity(t)
	defer security.Close()
	record := controlplane.NodeRecord{SchemaVersion: 3, NodeID: "node-a", TenantID: "tenant-a", Deployment: "prod", UpdatedAt: time.Now().UTC()}
	signed, err := security.AttestNodeLease(record)
	if err != nil || signed.TransportPublicKey != identity.PublicKey || signed.TransportSignature == "" {
		t.Fatalf("节点 lease 签名失败: %+v %v", signed, err)
	}
	verified, err := security.VerifyNodeLease(signed)
	if err != nil || verified.NodeID != "node-a" || verified.TenantID != "tenant-a" {
		t.Fatalf("节点 lease 验签失败: %+v %v", verified, err)
	}
	signed.Deployment = "other"
	if _, err := security.VerifyNodeLease(signed); err == nil {
		t.Fatal("篡改 deployment 后必须验签失败")
	}
}

func TestCapabilityVisibilityAuthorization(t *testing.T) {
	identity := TransportIdentity{
		Name: "database-node", NodeID: "node-a", ServiceRoles: []string{"backend"},
		LogicalServices: []string{"platform.database"}, AllowedCapabilities: []string{"platform.database"},
	}
	base := Announcement{Capability: "platform.database", NodeID: "node-a", ServiceRole: "backend", LogicalService: "platform.database"}
	for _, visibility := range []string{"local", "service", "cluster"} {
		record := base
		record.Visibility = visibility
		if err := authorizeCapability(identity, record); err != nil {
			t.Fatalf("%s 应获授权: %v", visibility, err)
		}
	}
	global := base
	global.Visibility = "global"
	if err := authorizeCapability(identity, global); err == nil {
		t.Fatal("未显式 allowGlobal 的身份不得调用 global capability")
	}
	identity.AllowGlobal = true
	if err := authorizeCapability(identity, global); err != nil {
		t.Fatalf("显式 global 授权应通过: %v", err)
	}
	identity.AllowedCapabilities = []string{"other"}
	if err := authorizeCapability(identity, global); err == nil {
		t.Fatal("visibility 授权不能绕过 capability allowlist")
	}
}

func TestAuthenticatedTransportContextKeepsIdentityInHostProvenance(t *testing.T) {
	trusted, err := authenticatedTransportTrustedContext(TransportIdentity{
		Name: "service-a", Role: "backend", TenantID: "acme", AllowDelegation: true,
	}, &contractv1.CallContext{TenantId: "acme", Metadata: map[string]string{"com.example.flag": "on"}})
	if err != nil {
		t.Fatal(err)
	}
	provenance := trusted.Provenance()
	if provenance.TransportIdentity != "service-a" || provenance.TransportRole != "backend" {
		t.Fatalf("传输 provenance 丢失: %+v", provenance)
	}
	if _, exists := trusted.Wire().Metadata["vastplan.transport.identity"]; exists {
		t.Fatal("transport identity 不得回写 Wire metadata")
	}
}
