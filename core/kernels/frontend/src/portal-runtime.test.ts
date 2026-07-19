import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { type FrontendPluginContext, type UIRenderAdapter, type UIShellAdapter, type UIWorkbenchAdapter } from "@vastplan/ui-primitives";
import { PortalAssemblyError, PortalRuntime, type FrontendPluginLoader, type PluginRef, type PortalSpec } from "./portal-runtime";

const adapterRef: PluginRef = { id: "cn.vastplan.foundation.frontend.render.adapter", version: "1.0.0" };
const arcoRendererRef: PluginRef = { id: "cn.vastplan.foundation.frontend.render.adapter.arco", version: "1.0.0" };
const muiRendererRef: PluginRef = { id: "cn.vastplan.foundation.frontend.render.adapter.mui", version: "1.0.0" };
const shellRef: PluginRef = { id: "cn.vastplan.foundation.frontend.structure.shell", version: "1.0.0" };
const workbenchRef: PluginRef = { id: "cn.vastplan.foundation.frontend.workflow.workbench", version: "1.0.0" };
const featureRef: PluginRef = { id: "cn.vastplan.platform.configuration.portal-composer", version: "1.0.0" };

const renderer = (id: string) => ({ id, label: id, framework: id, capabilities: ["layout", "menu", "overlay", "form", "data", "feedback", "theme"] as const, themeTemplates: [{ id: "light", label: "Light", scheme: "light" as const }], defaultThemeTemplate: "light", Provider: ({ children }: { children: ReactNode }) => children, localization: { defaultLocale: "zh-CN", messages: { "zh-CN": { label: "测试" }, "en-US": { label: "Test" } } } });
const adapter: UIRenderAdapter = {
  id: "ui.render.adapter", uiContract: "4.0.0", defaultRenderer: "arco",
  renderers: [
    { id: "arco", label: "Arco", framework: "arco", module: arcoRendererRef },
    { id: "mui", label: "MUI", framework: "mui", module: muiRendererRef },
  ],
};
const shell: UIShellAdapter = { id: "ui.structure.shell", uiContract: "4.0.0", templates: [{ id: "standard", label: "Standard" }, { id: "top-navigation", label: "Top" }], defaultTemplate: "standard", compose: ({ pages }) => ({ pages, navigation: { primary: [], settings: [], secondary: [] }, shellSlots: {}, pageSlots: {} }), Shell: () => null };
const workbench: UIWorkbenchAdapter = { id: "ui.workflow.workbench", uiContract: "4.0.0", CollectionPage: () => null };

const portal: PortalSpec = {
  revision: 1, id: "admin", tenantId: "acme", route: "/",
  renderAdapter: { ...adapterRef, uiContract: "^4.0.0", config: { defaultRenderer: "arco", allowedRenderers: ["arco", "mui"], userSelectable: true } },
  shell: { ...shellRef, uiContract: "^4.0.0", config: { defaultTemplate: "standard", allowedTemplates: ["standard", "top-navigation"], userSelectable: true } },
  workbench: { ...workbenchRef, uiContract: "^4.0.0" }, plugins: [adapterRef, arcoRendererRef, muiRendererRef, shellRef, workbenchRef, featureRef],
  management: { tenantId: "acme", portalId: "admin", platformProfile: { id: "portal-default", revision: 1, digest: "a".repeat(64) }, services: [{ id: "settings", logicalService: "platform.settings", routingDomain: "platform", capabilities: [{ capability: "platform.settings", read: ["list"] }] }] },
  resolution: { platformCatalog: { id: "catalog", revision: 1, digest: "c".repeat(64) }, platformProfile: { id: "portal-default", revision: 1, digest: "a".repeat(64) }, applicationComposition: { id: "admin", revision: 1, digest: "b".repeat(64) }, managementBindingDigest: "d".repeat(64), pluginOrigins: { [adapterRef.id]: "platform-profile", [arcoRendererRef.id]: "platform-profile", [muiRendererRef.id]: "platform-profile", [shellRef.id]: "platform-profile", [workbenchRef.id]: "platform-profile", [featureRef.id]: "platform-profile" } },
};

function loader(overrides: Record<string, object> = {}): FrontendPluginLoader {
  const base = { provenance: { signed: true, firstParty: true, integrity: "sha256:test" }, localization: { defaultLocale: "zh-CN", messages: { "zh-CN": { label: "测试" }, "en-US": { label: "Test" } } } };
  return { async load(ref) {
    if (ref.id === adapterRef.id) return { ...base, renderAdapter: adapter, ...overrides[ref.id] };
    if (ref.id === arcoRendererRef.id) return { ...base, renderer: renderer("arco"), ...overrides[ref.id] };
    if (ref.id === muiRendererRef.id) return { ...base, renderer: renderer("mui"), ...overrides[ref.id] };
    if (ref.id === shellRef.id) return { ...base, shell, ...overrides[ref.id] };
    if (ref.id === workbenchRef.id) return { ...base, workbench, ...overrides[ref.id] };
    return { ...base, register(context: FrontendPluginContext) { context.addPage({ id: "home", path: "/home", title: "Home", navigation: { id: "home", label: "Home", zone: "primary" }, slots: [{ id: "body", slot: "page.body.main", component: () => null }] }); }, ...overrides[ref.id] };
  } };
}

describe("PortalRuntime shell", () => {
  it("assembles one signed shell and the functional page", async () => {
    const prepared = await new PortalRuntime(loader()).prepare(portal);
    expect(prepared.shell.id).toBe("ui.structure.shell");
    expect(prepared.pages).toMatchObject([{ id: "home", path: "/home" }]);
  });

  it("selects only an allowed user Renderer and keeps the Adapter singleton", async () => {
    const prepared = await new PortalRuntime(loader()).prepare(portal, { rendererID: "mui" });
    expect(prepared.renderAdapter.id).toBe("mui");
    await expect(new PortalRuntime(loader()).prepare({ ...portal, renderAdapter: { ...portal.renderAdapter, config: { ...portal.renderAdapter.config, allowedRenderers: ["arco"], defaultRenderer: "mui" } } }))
      .rejects.toMatchObject({ code: "DESIGN_SYSTEM_RENDERER_CATALOG_INVALID" } satisfies Partial<PortalAssemblyError>);
  });

  it("loads only the selected Renderer module", async () => {
    const calls: string[] = [];
    const base = loader();
    const tracked: FrontendPluginLoader = { async load(ref) { calls.push(ref.id); return base.load(ref); } };
    await new PortalRuntime(tracked).prepare(portal);
    expect(calls).toContain(arcoRendererRef.id);
    expect(calls).not.toContain(muiRendererRef.id);
  });

  it("rejects undeclared shell templates before feature registration", async () => {
    await expect(new PortalRuntime(loader()).prepare({ ...portal, shell: { ...portal.shell, config: { ...portal.shell.config, allowedTemplates: ["missing"] } } }))
      .rejects.toMatchObject({ code: "SHELL_TEMPLATE_INVALID" } satisfies Partial<PortalAssemblyError>);
  });

  it("rejects a second shell contribution", async () => {
    await expect(new PortalRuntime(loader({ [featureRef.id]: { shell } })).prepare(portal))
      .rejects.toMatchObject({ code: "SECOND_SHELL_FOUNDATION" } satisfies Partial<PortalAssemblyError>);
  });

  it("keeps global shell contributions restricted to Profile plugins", async () => {
    const global = { ...portal, resolution: { ...portal.resolution, pluginOrigins: { ...portal.resolution.pluginOrigins, [featureRef.id]: "application" as const } } };
    await expect(new PortalRuntime(loader({ [featureRef.id]: { register(context: FrontendPluginContext) { context.addShellContribution({ id: "brand", slot: "shell.header.start", component: () => null }); } } })).prepare(global))
      .rejects.toMatchObject({ code: "SHELL_CONTRIBUTION_ORIGIN" } satisfies Partial<PortalAssemblyError>);
  });
});
