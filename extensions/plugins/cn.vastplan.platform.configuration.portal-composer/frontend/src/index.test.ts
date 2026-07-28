import { describe, expect, it, vi } from "vitest";
import { PortalControlClient, PortalControlError, type PortalApplicationComposition } from "@vastplan/ui-primitives";
import { buildApplicationComposition, createApplicationPage, portalCompositionSchema } from "./index";
import { createActivationPage, createBindingPage, createProfilePage, profileSchema } from "./governance-workspaces";

describe("Portal application composition", () => {
  it("never exposes or submits platform-managed fields", () => {
    const properties = portalCompositionSchema.schema.properties as Record<string, unknown>;
    expect(properties.renderAdapter).toBeUndefined();

    const composition = buildApplicationComposition({
      name: "operations",
      route: "/operations",
      renderAdapter: "cn.vastplan.foundation.frontend.render.adapter",
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
    expect(composition).not.toHaveProperty("renderAdapter");
  });

  it("registers every governance domain through Workbench contracts", () => {
    const client = new PortalControlClient({ fetch: async () => response([]) });
    const profilePage = createProfilePage(client);
    const bindingPage = createBindingPage(client);
    const pages = [profilePage, createApplicationPage(client), bindingPage, createActivationPage(client)];

    expect(pages.map((page) => page.path)).toEqual([
      "/settings/portals/profiles", "/settings/portals", "/settings/portals/bindings", "/settings/portals/activations",
    ]);
    expect(pages.every((page) => page.collection.selection === "single")).toBe(true);
    expect(pages.every((page) => (page.overlays?.length ?? 0) > 0)).toBe(true);
    expect(pages.flatMap((page) => page.collection.actions ?? []).filter((action) => action.visibleWhen !== undefined).length).toBeGreaterThan(8);
    expect(profilePage.collection.actions?.every((action) => action.placement === "record.row")).toBe(true);
    expect(bindingPage.collection.actions?.every((action) => action.placement === "record.row")).toBe(true);
    expect(createApplicationPage(client).pageActions?.map((action) => action.id)).toEqual(["application.create"]);
    expect(createActivationPage(client).pageActions?.map((action) => action.id)).toEqual(["activation.create"]);
  });

  it("offers Ant Design first in the governed Renderer choices", () => {
    const properties = profileSchema.schema.properties as Record<string, { oneOf?: Array<{ const: string }> }>;
    expect(properties.defaultRenderer?.oneOf?.map((choice) => choice.const)).toEqual(["antd", "arco", "mui"]);
    expect((properties.allowedRenderers as { items?: { oneOf?: Array<{ const: string }> } }).items?.oneOf?.map((choice) => choice.const)).toEqual(["antd", "arco", "mui"]);
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

  it("preserves stable Portal BFF error codes", async () => {
    const client = new PortalControlClient({ fetch: async () => response({ error: "forbidden" }, 403) });
    await expect(client.list()).rejects.toEqual(new PortalControlError(403, "forbidden"));
  });

  it("uses the dedicated immutable Activation endpoint with an expected-current CAS", async () => {
    const fetch = vi.fn().mockResolvedValueOnce(response({ token: "csrf-token" })).mockResolvedValueOnce(response({ id: 9, status: "Current" }));
    const client = new PortalControlClient({ fetch });
    const request = { portalId: "admin", applicationRevisionId: 7, profileRevisionId: 3, bindingRevisionId: 4, expectedCurrentId: 8, reason: "切换布局" };
    await client.activate(request);
    expect(fetch).toHaveBeenNthCalledWith(2, "/v1/portal-governance/activations", {
      method: "POST", credentials: "include", headers: { "Content-Type": "application/json", "X-VastPlan-CSRF": "csrf-token" }, body: JSON.stringify(request),
    });
  });
});
