import { describe, expect, it, vi } from "vitest";
import { PortalControlClient, PortalControlError, type PortalApplicationComposition } from "@vastplan/portal-ui";
import { buildApplicationComposition, portalCompositionSchema } from "./index";

describe("Portal application composition", () => {
  it("never exposes or submits platform-managed fields", () => {
    const properties = portalCompositionSchema.schema.properties as Record<string, unknown>;
    expect(properties.designSystem).toBeUndefined();

    const composition = buildApplicationComposition({
      name: "operations",
      route: "/operations",
      designSystem: "com.vastplan.foundation.frontend.design-system.arco",
      plugins: [{ id: "com.example.application.dashboard", version: "1.2.3" }],
    });

    expect(composition).toEqual({
      version: 1,
      revision: 1,
      id: "operations",
      target: { kernel: "frontend" },
      route: "/operations",
      plugins: [{ id: "com.example.application.dashboard", version: "1.2.3" }],
      config: {},
    });
    expect(composition).not.toHaveProperty("designSystem");
  });
});

function response(value: unknown, status = 200) {
  return { ok: status >= 200 && status < 300, status, async json() { return value; } };
}

describe("PortalControlClient", () => {
  const composition: PortalApplicationComposition = {
    version: 1, revision: 1, id: "admin", target: { kernel: "frontend" }, route: "/", plugins: [], config: {},
  };

  it("binds mutations to a freshly obtained CSRF token", async () => {
    const fetch = vi.fn()
      .mockResolvedValueOnce(response({ token: "csrf-token" }))
      .mockResolvedValueOnce(response({ id: 7, status: "Draft" }));
    const client = new PortalControlClient({ fetch });

    await client.update(7, composition);

    expect(fetch).toHaveBeenNthCalledWith(1, "/v1/csrf", { method: "GET", credentials: "include" });
    expect(fetch).toHaveBeenNthCalledWith(2, "/v1/portal-drafts/7", {
      method: "PUT", credentials: "include",
      headers: { "Content-Type": "application/json", "X-VastPlan-CSRF": "csrf-token" },
      body: JSON.stringify(composition),
    });
  });

  it("preserves stable Edge error codes", async () => {
    const client = new PortalControlClient({ fetch: async () => response({ error: "forbidden" }, 403) });
    await expect(client.list()).rejects.toEqual(new PortalControlError(403, "forbidden"));
  });
});
