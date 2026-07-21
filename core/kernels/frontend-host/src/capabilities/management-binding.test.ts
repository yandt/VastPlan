import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { managementAllows, managementBindingDigest, parseManagementBinding } from "./management-binding";
import { platformOperationAllowed } from "./platform-capability-policy";

const fixturePath = resolve(fileURLToPath(new URL("../../../../../contracts/testdata/management-binding-v1.json", import.meta.url)));

describe("Management Binding", () => {
  it("matches the shared Go/Node canonical digest and validates grants", () => {
    const fixture = JSON.parse(readFileSync(fixturePath, "utf8")) as { binding: unknown; digest: string };
    const binding = parseManagementBinding(fixture.binding);
    expect(managementBindingDigest(binding)).toBe(fixture.digest);
    expect(managementAllows(binding.services[0]!, "platform.settings", "list", false)).toBe(true);
    expect(managementAllows(binding.services[0]!, "platform.settings", "put", false)).toBe(false);
    expect(platformOperationAllowed(binding.services[0]!, "platform.settings", "put", true)).toBe(true);
    expect(platformOperationAllowed(binding.services[0]!, "product.agent.run", "invoke", true)).toBe(false);
  });

  it("rejects duplicate service routes and overlapping read/write operations", () => {
    const fixture = JSON.parse(readFileSync(fixturePath, "utf8")) as { binding: Record<string, unknown> };
    const services = fixture.binding.services as unknown[];
    expect(() => parseManagementBinding({ ...fixture.binding, services: [...services, services[0]] })).toThrow(/重复/);
    const service = services[0] as Record<string, unknown>;
    const grant = (service.capabilities as Record<string, unknown>[])[0]!;
    expect(() => parseManagementBinding({ ...fixture.binding, services: [{ ...service, capabilities: [{ ...grant, write: ["list"] }] }] })).toThrow(/operation/);
  });
});
