import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { type FrontendPluginContext, type UIRenderAdapter, type UIShellAdapter, type UIShellLibrary, type UIWorkbenchAdapter } from "@vastplan/ui-primitives";
import { PortalAssemblyError, PortalRuntime, type FrontendPluginLoader, type PluginRef, type PortalSpec } from "./portal-runtime";

const engineRef: PluginRef = { id: "cn.vastplan.foundation.frontend.runtime.engine.react", version: "1.0.0" };
const adapterRef: PluginRef = { id: "cn.vastplan.foundation.frontend.render.adapter", version: "1.0.0" };
const arcoRendererRef: PluginRef = { id: "cn.vastplan.foundation.frontend.render.adapter.arco", version: "1.0.0" };
const muiRendererRef: PluginRef = { id: "cn.vastplan.foundation.frontend.render.adapter.mui", version: "1.0.0" };
const shellRef: PluginRef = { id: "cn.vastplan.foundation.frontend.structure.shell", version: "1.0.0" };
const standardShellRef: PluginRef = { id: "cn.vastplan.foundation.frontend.structure.layout.standard", version: "1.0.0" };
const topShellRef: PluginRef = { id: "cn.vastplan.foundation.frontend.structure.layout.top-navigation", version: "1.0.0" };
const workbenchRef: PluginRef = { id: "cn.vastplan.foundation.frontend.workflow.workbench", version: "1.0.0" };
const featureRef: PluginRef = { id: "cn.vastplan.platform.configuration.portal-composer", version: "1.0.0" };

const renderer = (id: string) => ({ id, label: id, framework: id, capabilities: ["layout", "menu", "overlay", "form", "data", "feedback", "theme"] as const, themeTemplates: [{ id: "light", label: "Light", scheme: "light" as const }], defaultThemeTemplate: "light", iconThemes: [{ id: "canonical", label: "Canonical", source: "canonical" as const }], defaultIconTheme: "canonical", Provider: ({ children }: { children: ReactNode }) => children, localization: { defaultLocale: "zh-CN", messages: { "zh-CN": { label: "测试" }, "en-US": { label: "Test" } } } });
const adapter: UIRenderAdapter = {
  id: "ui.render.adapter", uiContract: "4.0.0", defaultRenderer: "arco",
  renderers: [
    { id: "arco", label: "Arco", framework: "arco", module: arcoRendererRef },
    { id: "mui", label: "MUI", framework: "mui", module: muiRendererRef },
  ],
};
const shell: UIShellAdapter = { id: "ui.structure.shell", uiContract: "4.0.0", templates: [{ id: "standard", label: "Standard", module: standardShellRef }, { id: "top-navigation", label: "Top", module: topShellRef }], defaultTemplate: "standard", compose: ({ pages }) => ({ pages, navigation: { primary: [], settings: [], secondary: [] }, shellSlots: {}, pageSlots: {} }) };
const shellLibrary = (id: string): UIShellLibrary => ({ id, shell: "ui.structure.shell", uiContract: "4.0.0", Shell: () => null });
const workbench: UIWorkbenchAdapter = { id: "ui.workflow.workbench", uiContract: "4.0.0", CollectionPage: () => null, CollectionPageActions: () => null, FormPage: () => null };

const portal: PortalSpec = {
  revision: 1, id: "admin", tenantId: "acme", route: "/",
  runtimeEngine: { ...engineRef, family: "react", engineContract: "^1.0.0" },
  renderAdapter: { ...adapterRef, uiContract: "^4.0.0", config: { defaultRenderer: "arco", allowedRenderers: ["arco", "mui"], userSelectable: true } },
  shell: { ...shellRef, uiContract: "^4.0.0", config: { defaultTemplate: "standard", allowedTemplates: ["standard", "top-navigation"], userSelectable: true } },
  workbench: { ...workbenchRef, uiContract: "^4.0.0" }, plugins: [engineRef, adapterRef, arcoRendererRef, muiRendererRef, shellRef, standardShellRef, topShellRef, workbenchRef, featureRef],
  management: { tenantId: "acme", portalId: "admin", platformProfile: { id: "portal-default", revision: 1, digest: "a".repeat(64) }, services: [{ id: "settings", logicalService: "platform.settings", routingDomain: "platform", capabilities: [{ capability: "platform.settings", read: ["list"] }] }] },
  resolution: { platformCatalog: { id: "catalog", revision: 1, digest: "c".repeat(64) }, platformProfile: { id: "portal-default", revision: 1, digest: "a".repeat(64) }, applicationComposition: { id: "admin", revision: 1, digest: "b".repeat(64) }, managementBindingDigest: "d".repeat(64), pluginOrigins: { [engineRef.id]: "platform-profile", [adapterRef.id]: "platform-profile", [arcoRendererRef.id]: "platform-profile", [muiRendererRef.id]: "platform-profile", [shellRef.id]: "platform-profile", [standardShellRef.id]: "platform-profile", [topShellRef.id]: "platform-profile", [workbenchRef.id]: "platform-profile", [featureRef.id]: "platform-profile" } },
};

function loader(overrides: Record<string, object> = {}): FrontendPluginLoader {
  const base = { provenance: { signed: true, firstParty: true, integrity: "sha256:test" }, localization: { defaultLocale: "zh-CN", messages: { "zh-CN": { label: "测试" }, "en-US": { label: "Test" } } } };
  return { async load(ref) {
    if (ref.id === engineRef.id) return { ...base, runtimeEngine: { id: "ui.runtime.engine" as const, family: "react", engineContract: "1.0.0", capabilities: ["csr", "generation"] as const }, ...overrides[ref.id] };
    if (ref.id === adapterRef.id) return { ...base, renderAdapter: adapter, ...overrides[ref.id] };
    if (ref.id === arcoRendererRef.id) return { ...base, renderer: renderer("arco"), ...overrides[ref.id] };
    if (ref.id === muiRendererRef.id) return { ...base, renderer: renderer("mui"), ...overrides[ref.id] };
    if (ref.id === shellRef.id) return { ...base, shell, ...overrides[ref.id] };
    if (ref.id === standardShellRef.id) return { ...base, shellLibrary: shellLibrary("standard"), ...overrides[ref.id] };
    if (ref.id === topShellRef.id) return { ...base, shellLibrary: shellLibrary("top-navigation"), ...overrides[ref.id] };
    if (ref.id === workbenchRef.id) return { ...base, workbench, ...overrides[ref.id] };
    return { ...base, register(context: FrontendPluginContext) { context.addPage({ id: "home", path: "/home", title: "Home", navigation: { id: "home", label: "Home", zone: "primary" }, slots: [{ id: "body", slot: "page.body.main", component: () => null }] }); }, ...overrides[ref.id] };
  } };
}

describe("PortalRuntime shell", () => {
  it("assembles one signed shell and the functional page", async () => {
    const prepared = await new PortalRuntime(loader()).prepare(portal);
    expect(prepared.runtimeEngine.family).toBe("react");
    expect(prepared.shell.id).toBe("ui.structure.shell");
    expect(prepared.shellLibrary.id).toBe("standard");
    expect(prepared.pages).toMatchObject([{ id: "home", path: "/home" }]);
  });

  it("selects only an allowed user Renderer and keeps the Adapter singleton", async () => {
    const prepared = await new PortalRuntime(loader()).prepare(portal, { rendererID: "mui" });
    expect(prepared.renderAdapter.id).toBe("mui");
    await expect(new PortalRuntime(loader()).prepare({ ...portal, renderAdapter: { ...portal.renderAdapter, config: { ...portal.renderAdapter.config, allowedRenderers: ["arco"], defaultRenderer: "mui" } } }))
      .rejects.toMatchObject({ code: "DESIGN_SYSTEM_RENDERER_CATALOG_INVALID" } satisfies Partial<PortalAssemblyError>);
  });

  it("rejects an icon theme not declared by the selected Renderer", async () => {
    const invalid = { ...portal, renderAdapter: { ...portal.renderAdapter, config: { ...portal.renderAdapter.config, rendererOptions: { arco: { iconTheme: "missing" } } } } };
    await expect(new PortalRuntime(loader()).prepare(invalid))
      .rejects.toMatchObject({ code: "DESIGN_SYSTEM_ICON_THEME_INVALID" } satisfies Partial<PortalAssemblyError>);
  });

  it("loads only the selected Renderer module", async () => {
    const calls: string[] = [];
    const base = loader();
    const tracked: FrontendPluginLoader = { async load(ref) { calls.push(ref.id); return base.load(ref); } };
    await new PortalRuntime(tracked).prepare(portal);
    expect(calls).toContain(arcoRendererRef.id);
    expect(calls).not.toContain(muiRendererRef.id);
  });

  it("loads only the selected Shell Library module", async () => {
    const calls: string[] = [];
    const base = loader();
    const tracked: FrontendPluginLoader = { async load(ref) { calls.push(ref.id); return base.load(ref); } };
    const prepared = await new PortalRuntime(tracked).prepare(portal, { shellTemplateID: "top-navigation" });
    expect(prepared.shellLibrary.id).toBe("top-navigation");
    expect(calls).toContain(topShellRef.id);
    expect(calls).not.toContain(standardShellRef.id);
  });

  it("rejects undeclared shell templates before feature registration", async () => {
    await expect(new PortalRuntime(loader()).prepare({ ...portal, shell: { ...portal.shell, config: { ...portal.shell.config, allowedTemplates: ["missing"] } } }))
      .rejects.toMatchObject({ code: "SHELL_TEMPLATE_INVALID" } satisfies Partial<PortalAssemblyError>);
  });

  it("rejects duplicate or malformed Shell Library module refs", async () => {
    const duplicateModuleCatalog = {
      ...shell,
      templates: [shell.templates[0], { ...shell.templates[1], module: standardShellRef }],
    } satisfies UIShellAdapter;
    await expect(new PortalRuntime(loader({ [shellRef.id]: { shell: duplicateModuleCatalog } })).prepare(portal))
      .rejects.toMatchObject({ code: "SHELL_TEMPLATE_INVALID" } satisfies Partial<PortalAssemblyError>);
  });

  it("rejects a second shell contribution", async () => {
    await expect(new PortalRuntime(loader({ [featureRef.id]: { shell } })).prepare(portal))
      .rejects.toMatchObject({ code: "SECOND_SHELL_FOUNDATION" } satisfies Partial<PortalAssemblyError>);
  });

  it("releases loader resources when assembly fails", async () => {
    const dispose = vi.fn();
    const tracked = { ...loader({ [featureRef.id]: { shell } }), dispose };
    await expect(new PortalRuntime(tracked).prepare(portal)).rejects.toMatchObject({ code: "SECOND_SHELL_FOUNDATION" });
    expect(dispose).toHaveBeenCalledOnce();
  });

  it("keeps global shell contributions restricted to Profile plugins", async () => {
    const global = { ...portal, resolution: { ...portal.resolution, pluginOrigins: { ...portal.resolution.pluginOrigins, [featureRef.id]: "application" as const } } };
    await expect(new PortalRuntime(loader({ [featureRef.id]: { register(context: FrontendPluginContext) { context.addShellContribution({ id: "brand", slot: "shell.header.start", component: () => null }); } } })).prepare(global))
      .rejects.toMatchObject({ code: "SHELL_CONTRIBUTION_ORIGIN" } satisfies Partial<PortalAssemblyError>);
  });

  it("accepts governed Card/Cursor and standalone Form pages through the Workbench only", async () => {
    const prepared = await new PortalRuntime(loader({ [featureRef.id]: { register(context: FrontendPluginContext) {
      context.addCollectionPage({
        id: "cards", path: "/cards", title: "Cards",
        collection: { id: "cards", title: "Cards", view: "cards", query: { mode: "cursor", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [], card: { titleKey: "name" }, actions: [{ id: "publish", label: "Publish", icon: "publish", placement: "page.primary" }] },
        async load() { return { items: [] }; },
      });
      context.addFormPage({
        id: "profile", path: "/profile", title: "Profile",
        form: { id: "profile", schema: { id: "profile", schema: { type: "object", properties: { name: { type: "string" } } } }, workflow: { surface: "page", title: "Profile" }, async submit() {} },
      });
    } } })).prepare(portal);
    expect(prepared.pages.map((page) => page.id)).toEqual(["cards", "profile"]);
    expect(prepared.pages[0]?.slots.map((slot) => slot.slot)).toEqual(["page.header.end", "page.body.main"]);
  });

  it("projects session permissions into Workbench pages and actions", async () => {
    const securedPortal = { ...portal, experience: { permissions: ["platform.demo.read"] } };
    const prepared = await new PortalRuntime(loader({ [featureRef.id]: { register(context: FrontendPluginContext) {
      context.addCollectionPage({
        id: "visible", path: "/visible", title: "Visible", requiredAnyPermissions: ["platform.demo.read", "platform.demo.write"],
        collection: { id: "visible", title: "Visible", view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [{ key: "id", label: "ID" }], actions: [
          { id: "read", label: "Read", placement: "record.row", requiredPermissions: ["platform.demo.read"] },
          { id: "write", label: "Write", placement: "record.row", requiredPermissions: ["platform.demo.write"] },
        ] },
        async load() { return { items: [] }; },
      });
      context.addCollectionPage({
        id: "hidden", path: "/hidden", title: "Hidden", requiredPermissions: ["platform.demo.write"],
        collection: { id: "hidden", title: "Hidden", view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20] }, columns: [{ key: "id", label: "ID" }] },
        async load() { return { items: [] }; },
      });
    } } })).prepare(securedPortal);
    expect(prepared.pages.map((page) => page.id)).toEqual(["visible"]);
    const collectionSlot = prepared.pages[0]?.slots.find((slot) => slot.slot === "page.body.main");
    expect(collectionSlot).toBeDefined();
  });
});
