import { describe, expect, it } from "vitest";
import type { TrustedCapabilityInvoker } from "./capability-invoker";
import { AddressingPortalComposerClient } from "./portal-composer-client";

describe("AddressingPortalComposerClient", () => {
  it("accepts generated operations and rejects values outside the signed contract", async () => {
    const observed: string[] = [];
    const invoker: TrustedCapabilityInvoker = {
      async invoke(_principal, _route, operation) {
        observed.push(operation);
        return new Uint8Array();
      },
    };
    const client = new AddressingPortalComposerClient(invoker);
    const principal = { id: "alice", tenantId: "tenant-a", roles: [] };

    await client.call(principal, "deleteProfileDraft", new Uint8Array());
    await expect(client.call(principal, "futureOperation" as never, new Uint8Array())).rejects.toThrow("签名 Capability Contract");
    expect(observed).toEqual(["deleteProfileDraft"]);
  });
});
