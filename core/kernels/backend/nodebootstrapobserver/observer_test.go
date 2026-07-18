package nodebootstrapobserver

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
)

const testTransportPublicKey = "UBN2AENL65VCM6XLPUDC4FGKH4EMJN2DKU2TVBDF34PRQTEG32FHOZ5G"

func TestObserverWaitsForMissingLeaseAndAcceptsExactIdentity(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	expectation := nodebootstrap.ReadinessExpectation{TenantID: "tenant-a", NodeID: "node-a", Deployment: "prod", TransportPublicKey: testTransportPublicKey}
	observer := &Observer{
		now: func() time.Time { return now }, maxAge: defaultMaxLeaseAge,
		lookup: func(context.Context, nodebootstrap.ReadinessExpectation) ([]byte, error) {
			return nil, jetstream.ErrKeyNotFound
		},
		verify: func(controlplane.NodeRecord) (addressing.TransportIdentity, error) {
			return addressing.TransportIdentity{}, nil
		},
	}
	got, err := observer.Observe(context.Background(), expectation)
	if err != nil || got.Status != nodebootstrap.ReadinessWaiting || got.Reason != "lease_missing" {
		t.Fatalf("缺失 lease 应等待: %+v %v", got, err)
	}

	record := controlplane.NodeRecord{SchemaVersion: 3, NodeID: "node-a", TenantID: "tenant-a", Deployment: "prod", UpdatedAt: now, TransportPublicKey: testTransportPublicKey, TransportTimestamp: "signed", TransportNonce: "nonce", TransportSignature: "signature"}
	raw, _ := json.Marshal(record)
	observer.lookup = func(context.Context, nodebootstrap.ReadinessExpectation) ([]byte, error) { return raw, nil }
	observer.verify = func(controlplane.NodeRecord) (addressing.TransportIdentity, error) {
		return addressing.TransportIdentity{Role: "node", NodeID: "node-a", TenantID: "tenant-a", PublicKey: testTransportPublicKey}, nil
	}
	got, err = observer.Observe(context.Background(), expectation)
	if err != nil || got.Status != nodebootstrap.ReadinessReady {
		t.Fatalf("精确匹配的签名 lease 应 Ready: %+v %v", got, err)
	}
}

func TestObserverRejectsSignatureAndIdentityMismatch(t *testing.T) {
	now := time.Now().UTC()
	record := controlplane.NodeRecord{SchemaVersion: 3, NodeID: "node-a", TenantID: "tenant-b", Deployment: "prod", UpdatedAt: now, TransportPublicKey: testTransportPublicKey}
	raw, _ := json.Marshal(record)
	observer := &Observer{
		now: func() time.Time { return now }, maxAge: defaultMaxLeaseAge,
		lookup: func(context.Context, nodebootstrap.ReadinessExpectation) ([]byte, error) { return raw, nil },
		verify: func(controlplane.NodeRecord) (addressing.TransportIdentity, error) {
			return addressing.TransportIdentity{}, errors.New("bad signature")
		},
	}
	expectation := nodebootstrap.ReadinessExpectation{TenantID: "tenant-a", NodeID: "node-a", Deployment: "prod", TransportPublicKey: testTransportPublicKey}
	got, err := observer.Observe(context.Background(), expectation)
	if err != nil || got.Status != nodebootstrap.ReadinessRejected || got.Reason != "lease_signature_invalid" {
		t.Fatalf("无效签名必须拒绝: %+v %v", got, err)
	}
}
