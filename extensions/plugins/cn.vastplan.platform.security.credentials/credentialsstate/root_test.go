package credentialsstate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRootRoundTripAndStrictValidation(t *testing.T) {
	snapshot := []byte(`{"records":{}}`)
	root, err := NewRoot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	root.Chunks = []Chunk{{Digest: DigestHex(snapshot), Size: len(snapshot)}}
	raw, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseRoot(raw)
	if err != nil || parsed.Digest != root.Digest || parsed.Size != len(snapshot) {
		t.Fatalf("root round trip: parsed=%+v err=%v", parsed, err)
	}
	for _, invalid := range [][]byte{
		[]byte(`{"format":"credentials.snapshot.v1","digest":"bad","size":1,"chunks":[]}`),
		[]byte(`{"format":"credentials.snapshot.v1","digest":"` + strings.Repeat("a", 64) + `","size":2,"chunks":[{"digest":"` + strings.Repeat("b", 64) + `","size":1}]}`),
		append(raw, []byte(` {}`)...),
	} {
		if _, err := ParseRoot(invalid); err == nil {
			t.Fatalf("invalid root accepted: %s", invalid)
		}
	}
}
