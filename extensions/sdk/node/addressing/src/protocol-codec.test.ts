import { fileURLToPath } from "node:url";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { AddressingProtocolCodec } from "./protocol-codec.js";

const contracts = resolve(fileURLToPath(new URL("../../../../../contracts/proto/", import.meta.url)));

describe("AddressingProtocolCodec", () => {
  it("round-trips the existing Addressing v1 unary wire", () => {
    const codec = new AddressingProtocolCodec(contracts);
    const context = { caller: { kind: 1, id: "alice" }, principal: { user_id: "alice", tenant_id: "tenant-a", system_roles: ["portal.compose"] }, scene: "portal.bff", tenant_id: "tenant-a" };
    const target = { extension_point: "tool.package", capability: "platform.portal-composer", operation: "list" };
    const request = codec.encodeRequest("request-1", target, context, new TextEncoder().encode("{}"));
    expect(codec.decodeRequest(request)).toMatchObject({ request_id: "request-1", target, context });
    const response = codec.encodeResponse("request-1", { status: 1 }, new TextEncoder().encode("[]"));
    expect(codec.decodeResponse(response)).toMatchObject({ request_id: "request-1", result: { status: 1 } });
    expect(codec.contextSize(context)).toBeGreaterThan(0);
  });
});
