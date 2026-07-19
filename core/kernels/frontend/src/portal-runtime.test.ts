import { describe, expect, it } from "vitest";
import { managementServicesFor, type UIRenderAdapter, type FrontendPluginContext, type StructureCompositionAdapter, type StructureLayoutAdapter, type UIWorkbenchAdapter } from "@vastplan/ui-primitives";
import { PortalAssemblyError, PortalRuntime, type FrontendPluginLoader, type PluginRef } from "./portal-runtime";

const arcoRef: PluginRef = { id: "cn.vastplan.foundation.frontend.render.adapter.arco", version: "1.0.0" };
const muiRef: PluginRef = { id: "cn.vastplan.foundation.frontend.render.adapter.mui", version: "1.0.0" };
const composerRef: PluginRef = { id: "cn.vastplan.platform.configuration.portal-composer", version: "1.0.0" };
const compositionRef: PluginRef = { id: "cn.vastplan.foundation.frontend.structure.composition.standard", version: "1.0.0" };
const layoutRef: PluginRef = { id: "cn.vastplan.foundation.frontend.structure.layout.standard", version: "1.0.0" };
const workbenchRef: PluginRef = { id: "cn.vastplan.foundation.frontend.workflow.workbench", version: "1.0.0" };

const structureComposition: StructureCompositionAdapter = { id: "ui.structure.composition", uiContract: "3.0.0", compose: ({ pages }) => ({ pages, navigation: { primary: [], settings: [], secondary: [] }, shellSlots: {}, pageSlots: {} }) };
const structureLayout: StructureLayoutAdapter = { id: "ui.structure.layout", uiContract: "3.0.0", Shell: () => null };
const workbench: UIWorkbenchAdapter = { id: "ui.workflow.workbench", uiContract: "3.0.0", CollectionPage: () => null };

function renderAdapter(framework: string): UIRenderAdapter {
  return {
  id: "ui.render.adapter",
  framework,
  uiContract: "3.0.0",
  capabilities: ["layout", "menu", "overlay", "form", "data", "feedback", "theme"],
  themes: [{ id: "light", mode: "light" }],
  defaultTheme: "light",
  Provider: () => null,
  };
}

const adapter = renderAdapter("arco");

function loader(overrides: Record<string, unknown> = {}): FrontendPluginLoader {
  return {
    async load(ref) {
      const base = {
        provenance: { signed: true, firstParty: true, integrity: "sha256:test" },
        localization: { defaultLocale: "zh-CN", messages: { "zh-CN": { "test.label": "测试" }, "en-US": { "test.label": "Test" } } },
      };
      if (ref.id === arcoRef.id || ref.id === muiRef.id) {
        return { ...base, renderAdapter: renderAdapter(ref.id === arcoRef.id ? "arco" : "mui"), ...(overrides[ref.id] as object) };
      }
      if (ref.id === compositionRef.id) return { ...base, structureComposition, ...(overrides[ref.id] as object) };
      if (ref.id === layoutRef.id) return { ...base, structureLayout, ...(overrides[ref.id] as object) };
      if (ref.id === workbenchRef.id) return { ...base, workbench, ...(overrides[ref.id] as object) };
      return {
        ...base,
        async register(context) {
          context.addPage({ id: "portal-composer", path: "/settings/portals", title: "门户组合", navigation: { id: "portal-composer", label: "门户组合", zone: "settings" }, slots: [{ id: "center", slot: "page.header.center", component: () => null }, { id: "actions", slot: "page.header.end", component: () => null }, { id: "body", slot: "page.body.main", component: () => null }] });
        },
        ...(overrides[ref.id] as object),
      };
    },
  };
}

const portal = {
	revision: 1, id: "admin", tenantId: "acme", route: "/", renderAdapter: { ...arcoRef, uiContract: "^3.0.0" }, structureComposition: { ...compositionRef, uiContract: "^3.0.0" }, structureLayout: { ...layoutRef, uiContract: "^3.0.0" }, workbench: { ...workbenchRef, uiContract: "^3.0.0" }, plugins: [arcoRef, compositionRef, layoutRef, workbenchRef, composerRef],
	management: { tenantId: "acme", portalId: "admin", platformProfile: { id: "portal-default", revision: 1, digest: "a".repeat(64) }, services: [{ id: "settings", logicalService: "platform.settings", routingDomain: "platform", capabilities: [{ capability: "platform.settings", read: ["list"] }] }] },
	resolution: {
		platformCatalog: { id: "portal-platform", revision: 1, digest: "c".repeat(64) },
		platformProfile: { id: "portal-default", revision: 1, digest: "a".repeat(64) },
		applicationComposition: { id: "admin", revision: 1, digest: "b".repeat(64) },
		managementBindingDigest: "d".repeat(64),
    pluginOrigins: { [arcoRef.id]: "platform-profile" as const, [compositionRef.id]: "platform-profile" as const, [layoutRef.id]: "platform-profile" as const, [workbenchRef.id]: "platform-profile" as const, [composerRef.id]: "platform-profile" as const },
  },
};

describe("PortalRuntime", () => {
  it("only assembles one signed first-party design system and framework-neutral plugins", async () => {
    const prepared = await new PortalRuntime(loader()).prepare(portal);
    expect(prepared.renderAdapter.framework).toBe("arco");
    expect(prepared.pages).toHaveLength(1);
    expect(prepared.pages[0]).toMatchObject({ path: "/settings/portals", pluginID: composerRef.id });
  });

  it("rejects a Profile theme not declared by the selected design system", async () => {
    await expect(new PortalRuntime(loader()).prepare({ ...portal, renderAdapter: { ...portal.renderAdapter, config: { theme: "untrusted-css" } } }))
      .rejects.toMatchObject({ code: "DESIGN_SYSTEM_THEME_INVALID" } satisfies Partial<PortalAssemblyError>);
  });

  it("registers a governed collection page through the selected Workbench", async () => {
    const prepared = await new PortalRuntime(loader({
      [composerRef.id]: {
        register(context: FrontendPluginContext) {
          context.addCollectionPage({
            id: "revisions", path: "/revisions", title: "Revisions", navigation: { id: "revisions", label: "Revisions", zone: "settings" },
            collection: { id: "revisions", title: "Revisions", view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [{ key: "id", label: "ID" }] },
            async load() { return { items: [], total: 0 }; },
          });
        },
      },
    })).prepare(portal);
    expect(prepared.workbench.id).toBe("ui.workflow.workbench");
    expect(prepared.pages).toMatchObject([{ id: "revisions", slots: [{ id: "workbench.collection", slot: "page.body.main" }] }]);
  });

  it("gives plugin registration a host-owned generation lifecycle signal", async () => {
    const controller = new AbortController();
    let received: FrontendPluginContext["lifecycle"] | undefined;
    await new PortalRuntime(loader({
      [composerRef.id]: {
        register(context: FrontendPluginContext) {
          received = context.lifecycle;
          context.addPage({ id: "home", path: "/home", title: "首页", slots: [{ id: "body", slot: "page.body.main", component: () => null }] });
        },
      },
    })).prepare(portal, { generation: "candidate-7", signal: controller.signal, reason: "replace" });
    expect(received).toMatchObject({ pluginID: composerRef.id, generation: "candidate-7", reason: "replace", signal: controller.signal });
    expect(Object.isFrozen(received)).toBe(true);
  });

  it("binds language resources and registration messages to the real plugin id", async () => {
    const prepared = await new PortalRuntime(loader({
      [composerRef.id]: {
        localization: { defaultLocale: "zh-CN", messages: { "zh-CN": { "page.title": "门户" }, "en-US": { "page.title": "Portal" } } },
        register(context: FrontendPluginContext) {
          const title = context.i18n.message("page.title", "门户");
          context.addPage({ id: "localized", path: "/localized", title, slots: [{ id: "body", slot: "page.body.main", component: () => null }] });
        },
      },
    })).prepare({ ...portal, localization: { defaultLocale: "zh-CN", supportedLocales: ["zh-CN", "en-US"] } });
    expect(prepared.pages[0].title).toMatchObject({ namespace: composerRef.id, key: "page.title" });
    expect(prepared.messageCatalogs[composerRef.id].messages["en-US"]["page.title"]).toBe("Portal");
  });

  it("rejects oversized or malformed plugin language resources", async () => {
    await expect(new PortalRuntime(loader({ [composerRef.id]: { localization: { defaultLocale: "zh-CN", messages: { bad_locale: { title: "x" } } } } })).prepare(portal))
      .rejects.toMatchObject({ code: "LOCALIZATION_INVALID" } satisfies Partial<PortalAssemblyError>);
    await expect(new PortalRuntime(loader({ [composerRef.id]: { localization: undefined } })).prepare(portal))
      .rejects.toMatchObject({ code: "LOCALIZATION_REQUIRED" } satisfies Partial<PortalAssemblyError>);
    await expect(new PortalRuntime(loader({ [composerRef.id]: { localization: { defaultLocale: "zh-CN", messages: { "zh-CN": { title: "仅中文" } } } } })).prepare(portal))
      .rejects.toMatchObject({ code: "LOCALIZATION_FIRST_PARTY_INCOMPLETE" } satisfies Partial<PortalAssemblyError>);
    await expect(new PortalRuntime(loader({ [composerRef.id]: { localization: { defaultLocale: "en-US", messages: { "en-US": { title: "one" }, "en-us": { title: "two" } } } } })).prepare(portal))
      .rejects.toMatchObject({ code: "LOCALIZATION_INVALID" } satisfies Partial<PortalAssemblyError>);
  });

  it("mounts functional page paths below the selected Portal route", async () => {
    const prepared = await new PortalRuntime(loader()).prepare({ ...portal, route: "/operations" });
    expect(prepared.pages[0]).toMatchObject({ path: "/operations/settings/portals", pluginID: composerRef.id });
  });

  it("rejects functional page paths that can escape or ambiguously encode the Portal route", async () => {
    const runtime = new PortalRuntime(loader({
      [composerRef.id]: {
        register(context: FrontendPluginContext) {
          context.addPage({ id: "escape", path: "/../settings", title: "Escape", slots: [{ id: "body", slot: "page.body.main", component: () => null }] });
        },
      },
    }));
    await expect(runtime.prepare({ ...portal, route: "/operations" })).rejects.toMatchObject({ code: "PAGE_REJECTED" } satisfies Partial<PortalAssemblyError>);
  });

  it("assembles the same functional plugin against a second UI framework", async () => {
    const muiPortal = {
      ...portal,
      renderAdapter: { ...muiRef, uiContract: "^3.0.0" },
      plugins: [muiRef, compositionRef, layoutRef, workbenchRef, composerRef],
      resolution: {
        ...portal.resolution,
        pluginOrigins: { [muiRef.id]: "platform-profile" as const, [compositionRef.id]: "platform-profile" as const, [layoutRef.id]: "platform-profile" as const, [workbenchRef.id]: "platform-profile" as const, [composerRef.id]: "platform-profile" as const },
      },
    };
    const prepared = await new PortalRuntime(loader()).prepare(muiPortal);
    expect(prepared.renderAdapter.framework).toBe("mui");
    expect(prepared.pages[0]).toMatchObject({ path: "/settings/portals", pluginID: composerRef.id });
  });

  it("fails closed for an untrusted design system", async () => {
    const runtime = new PortalRuntime(loader({ [arcoRef.id]: { provenance: { signed: false, firstParty: true, integrity: "sha256:test" } } }));
    await expect(runtime.prepare(portal)).rejects.toMatchObject({ code: "UNTRUSTED_REMOTE" } satisfies Partial<PortalAssemblyError>);
  });

  it("rejects a second design system contribution", async () => {
    const runtime = new PortalRuntime(loader({ [composerRef.id]: { renderAdapter: adapter } }));
    await expect(runtime.prepare(portal)).rejects.toMatchObject({ code: "SECOND_SHELL_FOUNDATION" } satisfies Partial<PortalAssemblyError>);
  });

  it("accepts global Shell contributions only from platform-profile plugins", async () => {
    const Brand = () => null;
    const prepared = await new PortalRuntime(loader({
      [composerRef.id]: {
        register(context: FrontendPluginContext) {
          context.addShellContribution({ id: "brand", slot: "shell.navigation.start", component: Brand });
          context.addPage({ id: "home", path: "/home", title: "首页", slots: [{ id: "body", slot: "page.body.main", component: () => null }] });
        },
      },
    })).prepare(portal);
    expect(prepared.shellContributions).toMatchObject([{ id: "brand", pluginID: composerRef.id, slot: "shell.navigation.start" }]);

    const applicationPortal = {
      ...portal,
      resolution: { ...portal.resolution, pluginOrigins: { ...portal.resolution.pluginOrigins, [composerRef.id]: "application" as const } },
    };
    await expect(new PortalRuntime(loader({
      [composerRef.id]: { register(context: FrontendPluginContext) { context.addShellContribution({ id: "brand", slot: "shell.navigation.start", component: Brand }); } },
    })).prepare(applicationPortal)).rejects.toMatchObject({ code: "SHELL_CONTRIBUTION_ORIGIN" } satisfies Partial<PortalAssemblyError>);
  });

  it("rejects a design system selected by the application input", async () => {
    const invalid = { ...portal, resolution: { ...portal.resolution, pluginOrigins: { ...portal.resolution.pluginOrigins, [arcoRef.id]: "application" as const } } };
    await expect(new PortalRuntime(loader()).prepare(invalid)).rejects.toMatchObject({ code: "ORIGIN_LOCK_INVALID" } satisfies Partial<PortalAssemblyError>);
  });

  it("rejects non-object Shell configuration before plugin registration", async () => {
    const invalid = { ...portal, structureComposition: { ...portal.structureComposition, config: [] as unknown as Record<string, unknown> } };
    await expect(new PortalRuntime(loader()).prepare(invalid)).rejects.toMatchObject({ code: "PORTAL_INVALID" } satisfies Partial<PortalAssemblyError>);
  });

  it("exposes every explicitly bound service to functional plugins without exposing routing input", async () => {
    const multiService = {
      ...portal,
      management: {
        ...portal.management,
        services: [
          portal.management.services[0],
          { id: "settings-dr", label: "灾备设置", logicalService: "platform.settings.dr", routingDomain: "platform-dr", capabilities: [{ capability: "platform.settings", read: ["list"] }] },
        ],
      },
    };
    const runtime = new PortalRuntime(loader({
      [composerRef.id]: {
        async register(context: FrontendPluginContext) {
          for (const service of managementServicesFor(context.portal, "platform.settings")) {
            context.addPage({ id: `settings-${service.id}`, path: `/settings/${service.id}`, title: service.label ?? service.id, slots: [{ id: "body", slot: "page.body.main", component: () => null }] });
          }
        },
      },
    }));
    const prepared = await runtime.prepare(multiService);
    expect(prepared.pages.map((page) => page.id)).toEqual(["settings-settings", "settings-settings-dr"]);
    expect(Object.isFrozen(prepared.portal.management.services)).toBe(true);
    expect(Object.isFrozen(prepared.portal.management.services[0].capabilities)).toBe(true);
  });

  it("rejects duplicated management operations in the browser trust boundary", async () => {
    const invalid = {
      ...portal,
      management: {
        ...portal.management,
        services: [{ ...portal.management.services[0], capabilities: [{ capability: "platform.settings", read: ["list"], write: ["list"] }] }],
      },
    };
    await expect(new PortalRuntime(loader()).prepare(invalid)).rejects.toMatchObject({ code: "MANAGEMENT_GRANT_INVALID" } satisfies Partial<PortalAssemblyError>);
  });
});
