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
      rootPlugins: [{ ref: { pluginId: "cn.vastplan.product.agent.worker", version: "1.2.0", channel: "stable" }, features: ["trace", "audit"] }],
      operations: { replicas: 2 },
    },
    {
      id: "api", serviceClass: "application.backend",
      rootPlugins: [{ ref: { pluginId: "cn.vastplan.product.agent.api", version: "1.0.0", channel: "stable" } }],
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
    expect(await backendApplicationIntentDigest(intent)).toBe("c5dfab4c35ae3b65e496eea0ddf658ddb40d8da4d1aa43c99c39d8fea9db42a9");
  });

  it("rejects duplicate roots before submitting to the server", () => {
    const duplicate = structuredClone(intent);
    duplicate.services[0]!.rootPlugins.push(duplicate.services[0]!.rootPlugins[0]!);
    expect(() => normalizeBackendApplicationIntent(duplicate)).toThrow("duplicate root plugin");
  });
});
