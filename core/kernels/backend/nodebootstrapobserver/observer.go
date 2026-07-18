// Package nodebootstrapobserver validates the authenticated Node Lease that
// proves a systemd-activated Node Agent has joined the intended control plane.
package nodebootstrapobserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
)

const defaultMaxLeaseAge = 45 * time.Second

type Observer struct {
	lookup func(context.Context, nodebootstrap.ReadinessExpectation) ([]byte, error)
	verify func(controlplane.NodeRecord) (addressing.TransportIdentity, error)
	now    func() time.Time
	maxAge time.Duration
}

func New(kv jetstream.KeyValue, security *addressing.TransportSecurity) (*Observer, error) {
	if kv == nil || security == nil {
		return nil, errors.New("节点就绪观察器必须配置 Node Lease KV 与传输验证器")
	}
	return &Observer{
		lookup: func(ctx context.Context, expectation nodebootstrap.ReadinessExpectation) ([]byte, error) {
			entry, err := kv.Get(ctx, controlplane.NodeKey(expectation.TenantID, expectation.Deployment, expectation.NodeID))
			if err != nil {
				return nil, err
			}
			return append([]byte(nil), entry.Value()...), nil
		},
		verify: security.VerifyNodeLease,
		now:    func() time.Time { return time.Now().UTC() }, maxAge: defaultMaxLeaseAge,
	}, nil
}

func (o *Observer) Observe(ctx context.Context, expectation nodebootstrap.ReadinessExpectation) (nodebootstrap.ReadinessObservation, error) {
	if ctx == nil || o == nil || o.lookup == nil || o.verify == nil || o.now == nil {
		return nodebootstrap.ReadinessObservation{}, errors.New("节点就绪观察器未完整配置")
	}
	if err := expectation.Validate(); err != nil {
		return nodebootstrap.ReadinessObservation{}, err
	}
	now := o.now().UTC()
	result := nodebootstrap.ReadinessObservation{Status: nodebootstrap.ReadinessWaiting, ObservedAt: now.Format(time.RFC3339Nano)}
	raw, err := o.lookup(ctx, expectation)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		result.Reason = "lease_missing"
		return result, nil
	}
	if err != nil {
		return nodebootstrap.ReadinessObservation{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var record controlplane.NodeRecord
	if err := decoder.Decode(&record); err != nil {
		return rejected(result, "lease_invalid"), nil
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return rejected(result, "lease_invalid"), nil
	}
	if err := record.ValidateBasic(); err != nil {
		return rejected(result, "lease_invalid"), nil
	}
	if record.UpdatedAt.After(now.Add(5 * time.Second)) {
		return rejected(result, "lease_clock_skew"), nil
	}
	if now.Sub(record.UpdatedAt) > o.maxAge {
		result.Reason = "lease_stale"
		return result, nil
	}
	identity, err := o.verify(record)
	if err != nil {
		return rejected(result, "lease_signature_invalid"), nil
	}
	if record.NodeID != expectation.NodeID || record.TenantID != expectation.TenantID || record.Deployment != expectation.Deployment ||
		record.TransportPublicKey != expectation.TransportPublicKey || identity.PublicKey != expectation.TransportPublicKey ||
		identity.NodeID != expectation.NodeID || identity.TenantID != expectation.TenantID {
		return rejected(result, "identity_mismatch"), nil
	}
	result.Status = nodebootstrap.ReadinessReady
	return result, nil
}

func rejected(result nodebootstrap.ReadinessObservation, reason string) nodebootstrap.ReadinessObservation {
	result.Status = nodebootstrap.ReadinessRejected
	result.Reason = reason
	return result
}
