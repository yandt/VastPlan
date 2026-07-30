import { describe, expect, it, vi } from "vitest";
import { PortalControlClient, PortalControlError, type PortalConfiguration } from "@vastplan/ui-primitives";
import { buildPortalConfiguration, createPortalPage, portalConfigurationSchema } from "./index";

describe("Portal aggregate workspace", () => {
  it("registers one Portal page instead of four independent governance domains", () => {
    const page = createPortalPage(new PortalControlClient({ fetch: async () => response({ portals: [] }) }));
    expect(page.path).toBe("/settings/portals");
    expect(page.pageActions?.map((action) => action.id)).toEqual(["portal.create"]);
    expect(page.collection.actions?.map((action) => action.id)).toEqual(expect.arrayContaining([
      "portal.edit", "portal.newVersion", "portal.publish", "portal.release", "portal.versions", "portal.releases",
    ]));
    expect(page.overlays?.map((overlay) => overlay.id)).toEqual(["versions", "releases", "audit", "configuration"]);
  });

  it("edits platform, application and services as one data-driven configuration", () => {
    const properties = portalConfigurationSchema.schema.properties as Record<string, unknown>;
    expect(Object.keys(properties)).toEqual(expect.arrayContaining(["defaultRenderer", "defaultTemplate", "applicationPlugins", "services"]));
    const updated = buildPortalConfiguration(configuration(), {
      route: "/new", defaultRenderer: "arco", allowedRenderers: ["antd", "arco"], defaultTemplate: "top-navigation",
      applicationPlugins: [{ id: "cn.example.dashboard", version: "1.2.3" }], services: [{ id: "backend" }],
    });
    expect(updated.application.route).toBe("/new");
    expect(updated.platform.renderAdapter.config.defaultRenderer).toBe("arco");
    expect(updated.platform.shell.config.defaultTemplate).toBe("top-navigation");
    expect(updated.services).toEqual([{ id: "backend" }]);
  });
});

describe("PortalControlClient", () => {
  it("updates a PortalVersion through the aggregate route with fresh CSRF", async () => {
    const fetch = vi.fn().mockResolvedValueOnce(response({ token: "csrf-token" })).mockResolvedValueOnce(response({ id: 7 }));
    const client = new PortalControlClient({ fetch });
    await client.updatePortalVersion("operations", 7, configuration());
    expect(fetch).toHaveBeenNthCalledWith(2, "/v1/portals/operations/versions/7", {
      method: "PUT", credentials: "include", headers: { "Content-Type": "application/json", "X-VastPlan-CSRF": "csrf-token" }, body: JSON.stringify({ configuration: configuration() }),
    });
  });

  it("uses one exact PortalVersion reference for release", async () => {
    const fetch = vi.fn().mockResolvedValueOnce(response({ token: "csrf-token" })).mockResolvedValueOnce(response({ id: 9, status: "Current" }));
    const client = new PortalControlClient({ fetch });
    const request = { portalVersionId: 7, expectedCurrentReleaseId: 8, reason: "上线完整版本" };
    await client.releasePortalVersion("operations", request);
    expect(fetch).toHaveBeenNthCalledWith(2, "/v1/portals/operations/releases", {
      method: "POST", credentials: "include", headers: { "Content-Type": "application/json", "X-VastPlan-CSRF": "csrf-token" }, body: JSON.stringify(request),
    });
  });

  it("preserves stable BFF error codes", async () => {
    const client = new PortalControlClient({ fetch: async () => response({ error: "forbidden" }, 403) });
    await expect(client.governance()).rejects.toEqual(new PortalControlError(403, "forbidden"));
  });
});

function configuration(): PortalConfiguration {
  const ref = { id: "cn.vastplan.foundation.frontend.runtime.engine.react", version: "1.0.0", channel: "stable" };
  return {
    platform: {
      version: 1, revision: 1, id: "operations.platform", target: { kernel: "frontend" },
      runtimeEngine: { ...ref, engineContract: "^1.0.0", family: "react" },
      renderAdapter: { ...ref, uiContract: "^8.0.0", config: { defaultRenderer: "antd", allowedRenderers: ["antd"], userSelectable: true } },
      shell: { ...ref, uiContract: "^8.0.0", config: { defaultTemplate: "standard", allowedTemplates: ["standard", "top-navigation"], userSelectable: true, templateOptions: { standard: {} } } },
      workbench: { ...ref, uiContract: "^8.0.0" }, plugins: [ref], security: { firstPartyOnly: true, requireIntegrity: true },
    },
    application: { version: 1, revision: 1, id: "operations", target: { kernel: "frontend" }, route: "/operations", plugins: [], config: {} },
    services: [{ id: "backend", logicalService: "backend", routingDomain: "platform", capabilities: [{ capability: "health", read: ["get"] }] }],
  };
}

function response(value: unknown, status = 200) { return { ok: status >= 200 && status < 300, status, async json() { return value; } }; }
