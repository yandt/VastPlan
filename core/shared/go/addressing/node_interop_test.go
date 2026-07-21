package addressing

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nats-io/nkeys"
)

func TestNodeTransportEnvelopeGoldenMatchesGoNKeyWire(t *testing.T) {
	var fixture struct {
		PublicKey, Subject, Timestamp, Nonce, PayloadBase64URL, Signature string
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "testdata", "addressing-v1-transport-envelope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(fixture.PayloadBase64URL)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := base64.RawURLEncoding.DecodeString(fixture.Signature)
	if err != nil {
		t.Fatal(err)
	}
	publicPair, err := nkeys.FromPublicKey(fixture.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	defer publicPair.Wipe()
	if err := publicPair.Verify(transportSigningBytes(fixture.Subject, fixture.Timestamp, fixture.Nonce, payload), signature); err != nil {
		t.Fatalf("Node NKey golden 无法由 Go 验证: %v", err)
	}
}
