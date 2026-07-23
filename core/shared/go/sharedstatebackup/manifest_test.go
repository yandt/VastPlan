package sharedstatebackup

import (
	"encoding/json"
	"testing"
	"time"
)

func TestManifestSignatureRejectsTamperingAndUntrustedKey(t *testing.T) {
	manifest := Manifest{
		Format: ManifestFormat, CreatedAt: time.Now().UTC(), Bucket: "VASTPLAN_SHARED_STATE_V1", Stream: "KV_VASTPLAN_SHARED_STATE_V1",
		Snapshot:     SnapshotDescriptor{SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Bytes: 10},
		Logical:      LogicalSummary{Digest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		StreamConfig: json.RawMessage(`{"name":"KV_VASTPLAN_SHARED_STATE_V1","subjects":["$KV.VASTPLAN_SHARED_STATE_V1.>"]}`),
		StreamState:  json.RawMessage(`{"messages":0,"bytes":0}`),
	}
	raw, err := MarshalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	private, trust, _ := GenerateSigningKey("backup-1")
	signature, err := SignManifest(raw, "backup-1", private)
	if err != nil || VerifyManifest(raw, signature, trust) != nil {
		t.Fatalf("signature round trip: %v", err)
	}
	tampered := append([]byte(nil), raw...)
	tampered[len(tampered)-2] ^= 1
	if err := VerifyManifest(tampered, signature, trust); err == nil {
		t.Fatal("被修改的 manifest 不得通过签名验证")
	}
	_, otherTrust, _ := GenerateSigningKey("backup-1")
	if err := VerifyManifest(raw, signature, otherTrust); err == nil {
		t.Fatal("同 key ID 的非受信公钥不得通过签名验证")
	}
}
