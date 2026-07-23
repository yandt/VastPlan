import { afterEach, describe, expect, it } from "vitest";
import { capabilityKey, CapabilityDirectoryIndex, rpcSubject } from "./capability-directory.js";
import { canonicalAnnouncementBytes, NodeTransportSecurity } from "./transport-security.js";
import { createTestTransportIdentity, type TestTransportIdentity } from "./testing.js";
import type { CapabilityAnnouncement } from "./types.js";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";

const allocated: TestTransportIdentity[] = [];
afterEach(() => { for (const item of allocated.splice(0)) item.pair.clear(); });

describe("NodeTransportSecurity", () => {
  it("signs, verifies and rejects replayed NKey envelopes", () => {
    const self = allocate("portal-host", "portal-node");
    const security = NodeTransportSecurity.fromBytes(self.seed, trust([self]), () => 1_000_000);
    const payload = new TextEncoder().encode("payload");
    const signed = security.sign("vp.test", payload);
    expect(security.verify("vp.test", payload, signed)).toMatchObject({ name: "portal-host" });
    expect(() => security.verify("vp.test", payload, signed)).toThrow(/重放/);
    expect(() => security.verify("vp.other", payload, { ...signed, nonce: "new-nonce" })).toThrow(/校验失败/);
    security.close();
  });

  it("verifies the cross-language Go/Node NKey envelope golden", () => {
    const path = resolve(fileURLToPath(new URL("../../../../../contracts/testdata/addressing-v1-transport-envelope.json", import.meta.url)));
    const fixture = JSON.parse(readFileSync(path, "utf8")) as { publicKey: string; subject: string; timestamp: string; nonce: string; payloadBase64URL: string; signature: string };
    const self = allocate("golden-verifier", "verifier-node");
    const identity = { name: "golden", role: "frontend", publicKey: fixture.publicKey, nodeId: "golden-node", serviceRoles: [], logicalServices: [], allowedCapabilities: ["*"], allowGlobal: true, allowDelegation: true };
    const security = NodeTransportSecurity.fromBytes(self.seed, { version: 1, identities: [self.identity, identity] }, () => Number(fixture.timestamp));
    expect(security.verify(fixture.subject, Buffer.from(fixture.payloadBase64URL, "base64url"), { publicKey: fixture.publicKey, timestamp: fixture.timestamp, nonce: fixture.nonce, signature: fixture.signature })).toMatchObject({ name: "golden" });
    security.close();
  });

  it("accepts only signed, shape-bound and authorized directory records", () => {
    const caller = allocate("portal-host", "portal-node");
    const backend = allocate("backend-a", "backend-node");
    const callerSecurity = NodeTransportSecurity.fromBytes(caller.seed, trust([caller, backend]), () => 1_000_000);
    const backendSecurity = NodeTransportSecurity.fromBytes(backend.seed, trust([caller, backend]), () => 1_000_000);
    const record = announcement(backend.identity);
    const key = capabilityKey(record.capability, record.instance_id);
    const signature = backendSecurity.sign(key, canonicalAnnouncementBytes(record));
    const signed = { ...record, transport_public_key: signature.publicKey, transport_timestamp: signature.timestamp, transport_nonce: signature.nonce, transport_signature: signature.signature };
    const directory = new CapabilityDirectoryIndex(callerSecurity, () => Date.parse("2026-07-21T00:00:01Z"));
    directory.apply(key, "PUT", new TextEncoder().encode(JSON.stringify(signed)));
    expect(directory.instances({ capability: record.capability })).toMatchObject([{ instance_id: "settings-a" }]);
    expect(() => directory.apply(`${key}.tampered`, "PUT", new TextEncoder().encode(JSON.stringify(signed)))).toThrow(/key/);

    const leaderRecord: CapabilityAnnouncement = {
      ...record, capability: "platform.deployment", logical_service: "platform.deployment",
      instance_policy: "leader", state_model: "external-shared", routing: "leader",
      instance_id: "deployment-a", subject: rpcSubject("platform.deployment", "platform.deployment", "platform"), fencing_token: "7",
    };
    const leaderKey = capabilityKey(leaderRecord.capability, leaderRecord.instance_id);
    const leaderSignature = backendSecurity.sign(leaderKey, canonicalAnnouncementBytes(leaderRecord));
    const signedLeader = { ...leaderRecord, transport_public_key: leaderSignature.publicKey, transport_timestamp: leaderSignature.timestamp, transport_nonce: leaderSignature.nonce, transport_signature: leaderSignature.signature };
    directory.apply(leaderKey, "PUT", new TextEncoder().encode(JSON.stringify(signedLeader)));
    expect(directory.instances({ capability: leaderRecord.capability })).toMatchObject([{ instance_id: "deployment-a", state_model: "external-shared" }]);

    directory.apply(key, "DEL");
    expect(directory.instances({ capability: record.capability })).toEqual([]);
    callerSecurity.close();
    backendSecurity.close();
  });
});

function allocate(name: string, node: string): TestTransportIdentity {
  const item = createTestTransportIdentity(name, node);
  allocated.push(item);
  return item;
}

function trust(items: readonly TestTransportIdentity[]) {
  return { version: 1, identities: items.map((item) => item.identity) };
}

function announcement(identity: TestTransportIdentity["identity"]): CapabilityAnnouncement {
  return {
    schema_version: 1, capability: "platform.settings", extension_point: "tool.package", service_role: "backend",
    logical_service: "platform.settings", routing_domain: "platform", instance_policy: "active-active", state_model: "external-shared", visibility: "cluster", routing: "queue",
    instance_id: "settings-a", node_id: identity.nodeId!, unit_id: "unit-a",
    subject: rpcSubject("platform.settings", "platform.settings", "platform"), health: "healthy", readiness: "ready",
    lease_expires_at: "2026-07-21T00:00:30Z", updated_at: "2026-07-21T00:00:00Z",
  };
}
