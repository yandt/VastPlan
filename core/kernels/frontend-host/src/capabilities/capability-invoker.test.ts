import type { NodeAddressingClient } from "@vastplan/addressing-node";
import { describe, expect, it } from "vitest";
import { AddressingCapabilityInvoker, CapabilityApplicationError } from "./capability-invoker";

describe("AddressingCapabilityInvoker", () => {
  it("projects only the verified Principal into a fresh trusted call context", async () => {
    let observed: { target: unknown; context: unknown } | undefined;
    const addressing = { async invoke(target: unknown, context: unknown) {
      observed = { target, context };
      return { result: { status: 1 }, payload: new TextEncoder().encode("ok") };
    } } satisfies Pick<NodeAddressingClient, "invoke">;
    const invoker = new AddressingCapabilityInvoker(addressing);
    const result = await invoker.invoke({ id: "alice", tenantId: "acme", roles: ["portal.compose"] }, {
      capability: "platform.portal-composer", logicalService: "platform.portal", routingDomain: "platform",
    }, "list", new Uint8Array());
    expect(new TextDecoder().decode(result)).toBe("ok");
    expect(observed?.target).toMatchObject({ capability: "platform.portal-composer", operation: "list", logical_service: "platform.portal", routing_domain: "platform" });
    expect(observed?.context).toMatchObject({ caller: { kind: 1, id: "alice" }, tenant_id: "acme", principal: { user_id: "alice", tenant_id: "acme", system_roles: ["portal.compose"] } });
  });

  it("preserves only stable application error codes", async () => {
    const addressing = { async invoke() { return { result: { status: 2, error: { code: "permission.denied", message: "denied", retryable: false } }, payload: new Uint8Array() }; } } satisfies Pick<NodeAddressingClient, "invoke">;
    const invoker = new AddressingCapabilityInvoker(addressing);
    await expect(invoker.invoke({ id: "alice", tenantId: "acme", roles: [] }, { capability: "demo", routingDomain: "platform" }, "read", new Uint8Array())).rejects.toEqual(expect.objectContaining<Partial<CapabilityApplicationError>>({ code: "permission.denied" }));
  });
});
