package addressing

import (
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
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
		Name: "node-a", Role: "node", PublicKey: publicKey, NodeID: "node-a",
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
	if got != identity {
		t.Fatalf("verified identity = %+v, want %+v", got, identity)
	}
	if _, err := security.verifyMessage(message); err == nil {
		t.Fatal("replayed transport message was accepted")
	}
	if _, err := security.verifyNoReplay(message.Subject, message.Data, transportHeaderValues(message.Header)); err != nil {
		t.Fatalf("JetStream redelivery should bypass nonce replay check: %v", err)
	}
}
