import { afterEach, describe, expect, it } from "vitest";
import { headers, type Msg, type NatsConnection, type PublishOptions, type Subscription, type SubscriptionOptions } from "@nats-io/transport-node";
import { NodeAddressingClient } from "./addressing-client.js";
import { rpcSubject } from "./capability-directory.js";
import { AddressingProtocolCodec } from "./protocol-codec.js";
import { NodeTransportSecurity, transportHeaders } from "./transport-security.js";
import { createTestTransportIdentity, type TestTransportIdentity } from "./testing.js";
import type { CallContext, CapabilityAnnouncement, CapabilityDirectoryPort } from "./types.js";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";

const contracts = resolve(fileURLToPath(new URL("../../../../../contracts/proto/", import.meta.url)));
const allocated: TestTransportIdentity[] = [];
afterEach(() => { for (const item of allocated.splice(0)) item.pair.clear(); });

describe("NodeAddressingClient", () => {
  it("uses the signed Protobuf request-reply wire and validates the response identity", async () => {
    const caller = allocate("portal-host", "portal-node");
    const backend = allocate("backend", "backend-node");
    const document = { version: 1, identities: [caller.identity, backend.identity] };
    const security = NodeTransportSecurity.fromBytes(caller.seed, document);
    const backendSecurity = NodeTransportSecurity.fromBytes(backend.seed, document);
    const codec = new AddressingProtocolCodec(contracts);
    const record = announcement(backend.identity);
    let observedOperation = "";
    const connection = fakeConnection((subject, payload, options, reply) => {
      const request = codec.decodeRequest(payload);
      observedOperation = request.target?.operation ?? "";
      const response = codec.encodeResponse(request.request_id!, { status: 1 }, new TextEncoder().encode("ok"));
      const signed = backendSecurity.sign(options.reply!, response);
      const responseHeaders = headers();
      responseHeaders.set(transportHeaders.publicKey, signed.publicKey);
      responseHeaders.set(transportHeaders.timestamp, signed.timestamp);
      responseHeaders.set(transportHeaders.nonce, signed.nonce);
      responseHeaders.set(transportHeaders.signature, signed.signature);
      queueMicrotask(() => reply(null, { subject: options.reply!, data: response, headers: responseHeaders } as Msg));
      expect(subject).toBe(record.subject);
    });
    const client = new NodeAddressingClient({ connection, directory: directory(record), security, codec });
    const response = await client.invoke({ extension_point: "tool.package", capability: record.capability, operation: "list", logical_service: record.logical_service!, routing_domain: record.routing_domain! }, context(), new TextEncoder().encode("{}"));
    expect(observedOperation).toBe("list");
    expect(new TextDecoder().decode(response.payload)).toBe("ok");
    security.close();
    backendSecurity.close();
  });

  it("publishes cancellation and releases the pending reply subscription", async () => {
    const caller = allocate("portal-host", "portal-node");
    const security = NodeTransportSecurity.fromBytes(caller.seed, { version: 1, identities: [caller.identity] });
    const codec = new AddressingProtocolCodec(contracts);
    const record = announcement(caller.identity);
    const published: string[] = [];
    const connection = fakeConnection((subject) => { published.push(subject); });
    const client = new NodeAddressingClient({ connection, directory: directory(record), security, codec });
    const controller = new AbortController();
    const pending = client.invoke({ extension_point: "tool.package", capability: record.capability }, context(), new Uint8Array(), controller.signal);
    controller.abort(new Error("stop"));
    await expect(pending).rejects.toThrow("stop");
    expect(published).toContain("vp.rpc.cancel.v1");
    security.close();
  });

  it("rejects a trusted response identity that is not bound to the selected instance", async () => {
    const caller = allocate("portal-host", "portal-node");
    const backend = allocate("backend", "backend-node");
    const intruder = allocate("other-service", "other-node");
    const document = { version: 1, identities: [caller.identity, backend.identity, intruder.identity] };
    const security = NodeTransportSecurity.fromBytes(caller.seed, document);
    const intruderSecurity = NodeTransportSecurity.fromBytes(intruder.seed, document);
    const codec = new AddressingProtocolCodec(contracts);
    const record = announcement(backend.identity);
    const connection = fakeConnection((_subject, payload, options, reply) => {
      const request = codec.decodeRequest(payload);
      const response = codec.encodeResponse(request.request_id!, { status: 1 }, new Uint8Array());
      const signed = intruderSecurity.sign(options.reply!, response);
      const responseHeaders = headers();
      responseHeaders.set(transportHeaders.publicKey, signed.publicKey);
      responseHeaders.set(transportHeaders.timestamp, signed.timestamp);
      responseHeaders.set(transportHeaders.nonce, signed.nonce);
      responseHeaders.set(transportHeaders.signature, signed.signature);
      queueMicrotask(() => reply(null, { subject: options.reply!, data: response, headers: responseHeaders } as Msg));
    });
    const client = new NodeAddressingClient({ connection, directory: directory(record), security, codec });
    await expect(client.invoke({ extension_point: "tool.package", capability: record.capability }, context(), new Uint8Array())).rejects.toThrow(/未绑定/);
    security.close();
    intruderSecurity.close();
  });
});

type Reply = (error: Error | null, message: Msg) => void;

function fakeConnection(onPublish: (subject: string, payload: Uint8Array, options: PublishOptions, reply: Reply) => void): NatsConnection {
  let reply: Reply = () => undefined;
  return {
    subscribe(_subject: string, options?: SubscriptionOptions) {
      reply = options?.callback as Reply;
      return { unsubscribe() {} } as Subscription;
    },
    publish(subject: string, payload?: Uint8Array | string, options: PublishOptions = {}) {
      onPublish(subject, typeof payload === "string" ? new TextEncoder().encode(payload) : payload ?? new Uint8Array(), options, reply);
    },
  } as unknown as NatsConnection;
}

function directory(record: CapabilityAnnouncement): CapabilityDirectoryPort {
  return { instances: () => [record] };
}

function allocate(name: string, node: string): TestTransportIdentity {
  const item = createTestTransportIdentity(name, node);
  allocated.push(item);
  return item;
}

function announcement(identity: TestTransportIdentity["identity"]): CapabilityAnnouncement {
  return {
    schema_version: 1, capability: "platform.settings", extension_point: "tool.package", service_role: "backend",
    logical_service: "platform.settings", routing_domain: "platform", visibility: "cluster",
    instance_id: "settings-a", node_id: identity.nodeId!, unit_id: "unit-a",
    subject: rpcSubject("platform.settings", "platform.settings", "platform"), health: "healthy", readiness: "ready", updated_at: new Date().toISOString(),
    transport_public_key: identity.publicKey,
  };
}

function context(): CallContext {
  return { caller: { kind: 1, id: "alice" }, principal: { user_id: "alice", tenant_id: "tenant-a" }, scene: "portal.bff", tenant_id: "tenant-a" };
}
