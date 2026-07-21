package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nats-io/nkeys"

	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
)

const (
	platformNodeTransportSeed = "platform-node.transport.seed"
	managedNodeTransportSeed  = "managed-node.transport.seed"
	portalHostTransportSeed   = "portal-host.transport.seed"
	transportTrustDocument    = "transport-trust.json"
)

func writeDevelopmentTransportIdentities(secretsDir string) error {
	node, nodeSeed, err := developmentTransportIdentity("local-platform-node", "node", "local-platform-node")
	if err != nil {
		return err
	}
	portal, portalSeed, err := developmentTransportIdentity("portal-host", "edge", "portal-host")
	if err != nil {
		return err
	}
	portal.AllowDelegation = true
	managed, managedSeed, err := developmentTransportIdentity("local-managed-node", "node", "local-managed-node")
	if err != nil {
		return err
	}
	document, err := json.MarshalIndent(addressing.TransportTrustDocument{Version: 1, Identities: []addressing.TransportIdentity{node, managed, portal}}, "", "  ")
	if err != nil {
		return err
	}
	for name, content := range map[string][]byte{
		platformNodeTransportSeed: append(nodeSeed, '\n'),
		managedNodeTransportSeed:  append(managedSeed, '\n'),
		portalHostTransportSeed:   append(portalSeed, '\n'),
		transportTrustDocument:    append(document, '\n'),
	} {
		if err := os.WriteFile(filepath.Join(secretsDir, name), content, 0o600); err != nil {
			return fmt.Errorf("写入开发传输身份 %s: %w", name, err)
		}
	}
	return nil
}

func developmentTransportIdentity(name, role, nodeID string) (addressing.TransportIdentity, []byte, error) {
	pair, err := nkeys.CreateUser()
	if err != nil {
		return addressing.TransportIdentity{}, nil, err
	}
	defer pair.Wipe()
	publicKey, err := pair.PublicKey()
	if err != nil {
		return addressing.TransportIdentity{}, nil, err
	}
	seed, err := pair.Seed()
	if err != nil {
		return addressing.TransportIdentity{}, nil, err
	}
	return addressing.TransportIdentity{
		Name: name, Role: role, PublicKey: publicKey, TenantID: "local", NodeID: nodeID,
		ServiceRoles: []string{"backend"}, LogicalServices: []string{"*"}, AllowedCapabilities: []string{"*"}, AllowGlobal: true,
	}, append([]byte(nil), seed...), nil
}
