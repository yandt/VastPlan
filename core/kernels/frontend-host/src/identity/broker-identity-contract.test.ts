import { describe, expect, it } from "vitest";
import { parseResultEnvelope } from "./broker-identity-contract";

describe("Broker identity contract", () => {
  it("accepts the restricted one-time enrollment step", () => {
    const value = parseResultEnvelope({
      result: {
        state: "challenge",
        step: { stepId: "e".repeat(32), kind: "enrollment", expiresAt: new Date(Date.now() + 60_000).toISOString() },
      },
    }, false);

    expect(value.result.step?.kind).toBe("enrollment");
  });

  it("continues rejecting unrecognized authentication steps", () => {
    expect(() => parseResultEnvelope({
      result: {
        state: "challenge",
        step: { stepId: "e".repeat(32), kind: "password-reset", expiresAt: new Date(Date.now() + 60_000).toISOString() },
      },
    }, false)).toThrow("Authentication Step 无效");
  });
});
