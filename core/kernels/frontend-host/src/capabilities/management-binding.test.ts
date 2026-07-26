import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { managementAllows, managementBindingDigest, parseManagementBinding } from "./management-binding";

const fixturePath = resolve(fileURLToPath(new URL("../../../../../contracts/testdata/management-binding-v1.json", import.meta.url)));

describe("Management Binding", () => {
  it("matches the shared Go/Node canonical digest and validates grants", () => {
    const fixture = JSON.parse(readFileSync(fixturePath, "utf8")) as { binding: unknown; digest: string };
    const binding = parseManagementBinding(fixture.binding);
    expect(managementBindingDigest(binding)).toBe(fixture.digest);
    expect(managementAllows(binding.services[0]!, "platform.settings", "list", false)).toBe(true);
    expect(managementAllows(binding.services[0]!, "platform.settings", "put", false)).toBe(false);
    expect(managementAllows(binding.services[0]!, "platform.settings", "put", true)).toBe(true);
    expect(managementAllows(binding.services[0]!, "product.agent.run", "invoke", true)).toBe(false);
  });

  it("rejects duplicate service routes and overlapping read/write operations", () => {
    const fixture = JSON.parse(readFileSync(fixturePath, "utf8")) as { binding: Record<string, unknown> };
    const services = fixture.binding.services as unknown[];
    expect(() => parseManagementBinding({ ...fixture.binding, services: [...services, services[0]] })).toThrow(/重复/);
    const service = services[0] as Record<string, unknown>;
    const grant = (service.capabilities as Record<string, unknown>[])[0]!;
    expect(() => parseManagementBinding({ ...fixture.binding, services: [{ ...service, capabilities: [{ ...grant, write: ["list"] }] }] })).toThrow(/operation/);
  });

  it("accepts only exact and unique Management API contract references", () => {
    const fixture = JSON.parse(readFileSync(fixturePath, "utf8")) as { binding: Record<string, unknown> };
    const services = fixture.binding.services as Record<string, unknown>[];
    const api = {
      id: "primary",
      contractId: "platform.api-exposure.management-api",
      contractVersion: "1.0.0",
      contractDigest: "740b9847429ae4e42fc742f50043f7615633c1520d66bbd1f9548ae2dc7d9e19",
    };
    const binding = parseManagementBinding({ ...fixture.binding, services: [{ ...services[0], apis: [api] }] });
    expect(binding.services[0]?.apis).toEqual([api]);
    expect(() => parseManagementBinding({ ...fixture.binding, services: [{ ...services[0], apis: [api, api] }] })).toThrow(/重复/);
    expect(() => parseManagementBinding({ ...fixture.binding, services: [{ ...services[0], apis: [{ ...api, contractVersion: "latest" }] }] })).toThrow(/契约格式/);
  });
});
