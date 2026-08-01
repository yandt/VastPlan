import { describe, expect, it, vi } from "vitest";
import { PortalControlClient, PortalControlError, type Portal, type PortalConfiguration } from "@vastplan/ui-primitives";
import { buildPortalConfiguration, createPortalPage, portalConfigurationSchema } from "./index";
import { toPortalRow } from "./portal-model";

describe("Portal aggregate workspace", () => {
  it("registers one Portal page instead of four independent governance domains", () => {
    const page = createPortalPage(new PortalControlClient({ fetch: async () => response({ portals: [] }) }));
    expect(page.path).toBe("/settings/portals");
    expect(page.pageActions?.map((action) => action.id)).toEqual(["portal.create"]);
    expect(page.collection.actions?.map((action) => action.id)).toEqual(expect.arrayContaining([
      "portal.edit", "portal.newWorkingCopy", "portal.publish", "portal.release", "portal.history", "portal.compare", "portal.restore", "portal.releases",
    ]));
    expect(page.collection.actions?.filter((action) => action.id === "portal.edit" || action.id === "portal.newWorkingCopy"))
      .toEqual([{ id: "portal.edit", label: "编辑", icon: "edit", placement: "record.row", form: "edit", visibleWhen: { pointer: "/canEdit", equals: true } }, {
        id: "portal.newWorkingCopy", label: "编辑", icon: "edit", placement: "record.row", form: "new-working-copy", visibleWhen: { pointer: "/canCreateWorkingCopy", equals: true },
      }]);
    expect(page.overlays?.map((overlay) => overlay.id)).toEqual(["history", "compare", "releases", "audit", "configuration"]);
  });

  it("edits platform, application and services as one data-driven configuration", () => {
    const properties = portalConfigurationSchema.schema.properties as Record<string, unknown>;
    expect(Object.keys(properties)).toEqual(expect.arrayContaining(["defaultRenderer", "defaultTemplate", "applicationPlugins", "services"]));
    const updated = buildPortalConfiguration(configuration(), {
      route: "/new", defaultRenderer: "antd", allowedRenderers: ["antd"], defaultTemplate: "top-navigation",
      applicationPlugins: [{ id: "cn.example.dashboard", version: "1.2.3" }], services: [{ id: "backend" }],
    });
    expect(updated.application.route).toBe("/new");
    expect(updated.platform.renderAdapter.config.defaultRenderer).toBe("antd");
    expect(updated.platform.shell.config.defaultTemplate).toBe("top-navigation");
    expect(updated.platform.plugins).toContainEqual(updated.platform.accountCenter);
    expect(updated.services).toEqual([{ id: "backend" }]);
  });

  it("completes a missing account center from the trusted platform default", () => {
    const base = configuration();
    const platformDefault = configuration().platform;
    const accountCenter = { id: "cn.vastplan.foundation.frontend.identity.account-center", version: "0.1.2", channel: "stable" };
    platformDefault.accountCenter = accountCenter;
    platformDefault.plugins = [...platformDefault.plugins, accountCenter];
    delete (base.platform as Partial<typeof base.platform>).accountCenter;
    base.platform.plugins = base.platform.plugins.filter((plugin) => plugin.id !== accountCenter.id);

    const updated = buildPortalConfiguration(base, {}, platformDefault);
    expect(updated.platform.accountCenter).toEqual(accountCenter);
    expect(updated.platform.plugins.filter((plugin) => plugin.id === accountCenter.id)).toEqual([accountCenter]);
  });

  it("can prepare the first Portal from the trusted creation template", async () => {
    const template = configuration();
    const page = createPortalPage(new PortalControlClient({ fetch: async () => response({ portals: [], creationTemplate: template }) }));
    const form = page.forms?.find((candidate) => candidate.id === "create");
    const prepared = await form?.prepare?.([], new AbortController().signal);
    expect(prepared?.initialValue).toMatchObject({ portalId: "", route: "/", defaultRenderer: "antd" });
  });

  it("projects one Portal row from its working copy without a compatibility versions array", async () => {
    const workingCopy = { tenantId: "tenant-a", portalId: "operations", revision: 2, configuration: configuration(), digest: "a".repeat(64), createdAt: "2026-07-30T00:00:00Z", updatedAt: "2026-07-30T00:00:00Z" };
    const portal = { id: "operations", tenantId: "tenant-a", workingCopy, versionControl: { enabled: false, availability: "disabled", capabilities: [] }, releases: [], createdAt: workingCopy.createdAt, updatedAt: workingCopy.updatedAt };
    const page = createPortalPage(new PortalControlClient({ fetch: async () => response({ portals: [portal] }) }));
    const result = await page.load({ mode: "page", page: 1, pageSize: 20, filters: {} }, new AbortController().signal);
    expect(result).toMatchObject({ total: 1, items: [{ id: "operations", workingRevision: 2, status: "Draft", releaseAvailable: false }] });
  });

  it("defines the Portal status as a plain responsive two-column summary", async () => {
    const page = createPortalPage(new PortalControlClient({ fetch: async () => response({ portals: [] }) }));
    const summary = await page.loadSummary?.(new AbortController().signal);
    expect(summary).toMatchObject({
      appearance: "plain",
      columns: { xs: 1, sm: 1, md: 2, lg: 2, xl: 2 },
      metrics: [{ id: "portals" }, { id: "online" }],
    });
    expect(summary).not.toHaveProperty("title");
  });

  it("shows only negotiated version actions and hides all of them while unavailable", () => {
    const workingCopy = { tenantId: "tenant-a", portalId: "operations", revision: 2, configuration: configuration(), digest: "a".repeat(64), createdAt: "2026-07-30T00:00:00Z", updatedAt: "2026-07-30T00:00:00Z" };
    const portal: Portal = {
      id: "operations", tenantId: "tenant-a", workingCopy,
      versionControl: { enabled: true, availability: "available", capabilities: ["history", "read"] },
      releases: [], createdAt: workingCopy.createdAt, updatedAt: workingCopy.updatedAt,
    };
    expect(toPortalRow(portal)[0]).toMatchObject({ historyAvailable: true, diffAvailable: false, restoreAvailable: false });
    portal.versionControl = { enabled: true, availability: "unavailable", capabilities: ["history", "read", "diff", "restore"] };
    expect(toPortalRow(portal)[0]).toMatchObject({ historyAvailable: false, diffAvailable: false, restoreAvailable: false });
  });
});

describe("PortalControlClient", () => {
  it("saves a WorkingCopy through revision CAS with fresh CSRF", async () => {
    const fetch = vi.fn().mockResolvedValueOnce(response({ token: "csrf-token" })).mockResolvedValueOnce(response({ id: 7 }));
    const client = new PortalControlClient({ fetch });
    await client.savePortalWorkingCopy("operations", 7, configuration());
    expect(fetch).toHaveBeenNthCalledWith(2, "/v1/portals/operations/working-copy", {
      method: "PUT", credentials: "include", headers: { "Content-Type": "application/json", "X-VastPlan-CSRF": "csrf-token" }, body: JSON.stringify({ expectedRevision: 7, configuration: configuration() }),
    });
  });

  it("uses one exact Publication reference for release", async () => {
    const fetch = vi.fn().mockResolvedValueOnce(response({ token: "csrf-token" })).mockResolvedValueOnce(response({ id: 9, status: "Current" }));
    const client = new PortalControlClient({ fetch });
    const request = { publicationId: 7, expectedCurrentReleaseId: 8, reason: "上线完整候选" };
    await client.releasePortalPublication("operations", request);
    expect(fetch).toHaveBeenNthCalledWith(2, "/v1/portals/operations/releases", {
      method: "POST", credentials: "include", headers: { "Content-Type": "application/json", "X-VastPlan-CSRF": "csrf-token" }, body: JSON.stringify(request),
    });
  });

  it("addresses version history by opaque ID and compares through query parameters", async () => {
    const fetch = vi.fn().mockResolvedValue(response({ dirty: true }));
    const client = new PortalControlClient({ fetch });
    await client.comparePortalVersions("operations", "version:1", "version:2");
    expect(fetch).toHaveBeenCalledWith("/v1/portals/operations/compare?left=version%3A1&right=version%3A2", { method: "GET", credentials: "include" });
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
      workbench: { ...ref, uiContract: "^8.0.0" }, accountCenter: { ...ref }, plugins: [ref], security: { firstPartyOnly: true, requireIntegrity: true },
    },
    application: { version: 1, revision: 1, id: "operations", target: { kernel: "frontend" }, route: "/operations", plugins: [], config: {} },
    services: [{ id: "backend", logicalService: "backend", routingDomain: "platform", capabilities: [{ capability: "health", read: ["get"] }] }],
  };
}

function response(value: unknown, status = 200) { return { ok: status >= 200 && status < 300, status, async json() { return value; } }; }
