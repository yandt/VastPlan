import { describe, expect, it } from "vitest";
import { managementServicesFor, type DesignSystemAdapter, type FrontendPluginContext, type ShellCompositionAdapter, type ShellLayoutAdapter } from "@vastplan/portal-ui";
import { PortalAssemblyError, PortalRuntime, type FrontendPluginLoader, type PluginRef } from "./portal-runtime";

const arcoRef: PluginRef = { id: "com.vastplan.foundation.frontend.design-system.arco", version: "1.0.0" };
const muiRef: PluginRef = { id: "com.vastplan.foundation.frontend.design-system.mui", version: "1.0.0" };
const composerRef: PluginRef = { id: "com.vastplan.platform.configuration.portal-composer", version: "1.0.0" };
const compositionRef: PluginRef = { id: "com.vastplan.foundation.frontend.composition.standard", version: "1.0.0" };
const layoutRef: PluginRef = { id: "com.vastplan.foundation.frontend.layout.standard", version: "1.0.0" };

const composition: ShellCompositionAdapter = { id: "ui.shell-composition", uiContract: "2.0.0", compose: ({ pages }) => ({ pages, navigation: { primary: [], settings: [], secondary: [] }, shellSlots: {}, pageSlots: {} }) };
const layout: ShellLayoutAdapter = { id: "ui.shell-layout", uiContract: "2.0.0", Shell: () => null };

function designSystem(framework: string): DesignSystemAdapter {
  return {
  id: "ui.design-system",
  framework,
  uiContract: "2.0.0",
  capabilities: ["layout", "menu", "overlay", "form", "data", "feedback", "theme"],
  Provider: () => null,
  };
}

const adapter = designSystem("arco");

function loader(overrides: Record<string, unknown> = {}): FrontendPluginLoader {
  return {
    async load(ref) {
      const base = {
        provenance: { signed: true, firstParty: true, integrity: "sha256:test" },
        localization: { defaultLocale: "zh-CN", messages: { "zh-CN": { "test.label": "测试" }, "en-US": { "test.label": "Test" } } },
      };
      if (ref.id === arcoRef.id || ref.id === muiRef.id) {
        return { ...base, designSystem: designSystem(ref.id === arcoRef.id ? "arco" : "mui"), ...(overrides[ref.id] as object) };
      }
      if (ref.id === compositionRef.id) return { ...base, composition, ...(overrides[ref.id] as object) };
      if (ref.id === layoutRef.id) return { ...base, layout, ...(overrides[ref.id] as object) };
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
	revision: 1, id: "admin", tenantId: "acme", route: "/", designSystem: { ...arcoRef, uiContract: "^2.0.0" }, composition: { ...compositionRef, uiContract: "^2.0.0" }, layout: { ...layoutRef, uiContract: "^2.0.0" }, plugins: [arcoRef, compositionRef, layoutRef, composerRef],
	management: { tenantId: "acme", portalId: "admin", platformProfile: { id: "portal-default", revision: 1, digest: "a".repeat(64) }, services: [{ id: "settings", logicalService: "platform.settings", routingDomain: "platform", capabilities: [{ capability: "platform.settings", read: ["list"] }] }] },
	resolution: {
		platformCatalog: { id: "portal-platform", revision: 1, digest: "c".repeat(64) },
		platformProfile: { id: "portal-default", revision: 1, digest: "a".repeat(64) },
		applicationComposition: { id: "admin", revision: 1, digest: "b".repeat(64) },
		managementBindingDigest: "d".repeat(64),
    pluginOrigins: { [arcoRef.id]: "platform-profile" as const, [compositionRef.id]: "platform-profile" as const, [layoutRef.id]: "platform-profile" as const, [composerRef.id]: "platform-profile" as const },
  },
};

describe("PortalRuntime", () => {
  it("only assembles one signed first-party design system and framework-neutral plugins", async () => {
    const prepared = await new PortalRuntime(loader()).prepare(portal);
    expect(prepared.designSystem.framework).toBe("arco");
    expect(prepared.pages).toHaveLength(1);
    expect(prepared.pages[0]).toMatchObject({ path: "/settings/portals", pluginID: composerRef.id });
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
      designSystem: { ...muiRef, uiContract: "^2.0.0" },
      plugins: [muiRef, compositionRef, layoutRef, composerRef],
      resolution: {
        ...portal.resolution,
        pluginOrigins: { [muiRef.id]: "platform-profile" as const, [compositionRef.id]: "platform-profile" as const, [layoutRef.id]: "platform-profile" as const, [composerRef.id]: "platform-profile" as const },
      },
    };
    const prepared = await new PortalRuntime(loader()).prepare(muiPortal);
    expect(prepared.designSystem.framework).toBe("mui");
    expect(prepared.pages[0]).toMatchObject({ path: "/settings/portals", pluginID: composerRef.id });
  });

  it("fails closed for an untrusted design system", async () => {
    const runtime = new PortalRuntime(loader({ [arcoRef.id]: { provenance: { signed: false, firstParty: true, integrity: "sha256:test" } } }));
    await expect(runtime.prepare(portal)).rejects.toMatchObject({ code: "UNTRUSTED_REMOTE" } satisfies Partial<PortalAssemblyError>);
  });

  it("rejects a second design system contribution", async () => {
    const runtime = new PortalRuntime(loader({ [composerRef.id]: { designSystem: adapter } }));
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
    const invalid = { ...portal, composition: { ...portal.composition, config: [] as unknown as Record<string, unknown> } };
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
