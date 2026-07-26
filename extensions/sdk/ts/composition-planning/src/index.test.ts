import { describe, expect, it } from "vitest";
import { backendApplicationIntentDigest, normalizeBackendApplicationIntent, type BackendApplicationIntent } from "./index";

const intent: BackendApplicationIntent = {
  version: 1,
  revision: 3,
  id: "agent-services",
  target: { kernel: "backend" },
  metadata: { tenant: "acme", name: "agent-services" },
  services: [
    {
      id: "worker", serviceClass: "application.backend",
      rootPlugins: [{ pluginId: "cn.vastplan.product.agent.worker", constraint: "^1.2.0", channel: "stable", features: ["trace", "audit"] }],
      operations: { replicas: 2 },
    },
    {
      id: "api", serviceClass: "application.backend",
      rootPlugins: [{ pluginId: "cn.vastplan.product.agent.api", constraint: "=1.0.0", channel: "stable" }],
      pluginConfig: { "cn.vastplan.product.agent.api": { limit: 10 } },
      operations: { replicas: 1 },
    },
  ],
};

describe("Backend Application Intent", () => {
  it("normalizes services, roots, features and config maps deterministically", async () => {
    const normalized = normalizeBackendApplicationIntent(intent);
    expect(normalized.services.map((service) => service.id)).toEqual(["api", "worker"]);
    expect(normalized.services[1]?.rootPlugins[0]?.features).toEqual(["audit", "trace"]);
    expect(await backendApplicationIntentDigest(intent)).toBe("a003aa32b1f344ae2adf463c478be507ee3af5c1fc969ba8caad7542b0fe8589");
  });

  it("rejects duplicate roots before submitting to the server", () => {
    const duplicate = structuredClone(intent);
    duplicate.services[0]!.rootPlugins.push(duplicate.services[0]!.rootPlugins[0]!);
    expect(() => normalizeBackendApplicationIntent(duplicate)).toThrow("duplicate root plugin");
  });

  it("accepts only the exact and compatible application policies", () => {
    const advanced = structuredClone(intent);
    advanced.services[0]!.rootPlugins[0]!.constraint = ">=1.2.0 <2.0.0";
    expect(() => normalizeBackendApplicationIntent(advanced)).toThrow("must be =x.y.z or ^x.y.z");
  });
});
