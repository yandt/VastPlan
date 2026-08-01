import { describe, expect, it } from "vitest";
import { CapabilityApplicationError } from "../capabilities/capability-invoker";
import { capabilityFailureRecord } from "./capability-failure";

describe("capability failure diagnostics", () => {
  it("keeps routing facts and a correlation digest without logging upstream detail", () => {
    const secret = "vault endpoint=https://secret.invalid token=do-not-log";
    const record = capabilityFailureRecord({ operation: "list", capability: "platform.credentials", logicalService: "platform.credentials" }, new CapabilityApplicationError("platform.credentials.unavailable", secret));
    expect(record).toMatchObject({
      operation: "list",
      capability: "platform.credentials",
      logical_service: "platform.credentials",
      code: "platform.credentials.unavailable",
      error_type: "CapabilityApplicationError",
    });
    expect(record.detail_digest).toMatch(/^[a-f0-9]{16}$/);
    expect(JSON.stringify(record)).not.toContain(secret);
    expect(JSON.stringify(record)).not.toContain("secret.invalid");
  });

  it("rejects unbounded error codes from diagnostics", () => {
    const record = capabilityFailureRecord({ operation: "invoke" }, { code: "invalid code with spaces", retryable: true });
    expect(record).toMatchObject({ code: "transport.failed", retryable: true });
  });
});
