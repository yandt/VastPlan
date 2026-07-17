import { describe, expect, it } from "vitest";
import type { DesignSystemAdapter } from "@vastplan/portal-ui";
import { PortalAssemblyError, PortalRuntime, type FrontendPluginLoader, type PluginRef } from "./portal-runtime";

const arcoRef: PluginRef = { id: "com.vastplan.foundation.frontend.design-system.arco", version: "1.0.0" };
const composerRef: PluginRef = { id: "com.vastplan.platform.configuration.portal-composer", version: "1.0.0" };

const adapter = {
  id: "ui.design-system",
  framework: "arco",
  uiContract: "1.0.0",
  capabilities: ["layout", "menu", "overlay", "form", "data", "feedback", "theme"],
  Provider: () => null,
} satisfies DesignSystemAdapter;

function loader(overrides: Record<string, unknown> = {}): FrontendPluginLoader {
  return {
    async load(ref) {
      const base = {
        provenance: { signed: true, firstParty: true, integrity: "sha256:test" },
      };
      if (ref.id === arcoRef.id) {
        return { ...base, designSystem: adapter, ...(overrides[ref.id] as object) };
      }
      return {
        ...base,
        async register(context) {
          context.addRoute({ path: "/settings/portals", pluginID: ref.id });
          context.addMenu({ id: "portal-composer", title: "门户组合", route: "/settings/portals" });
        },
        ...(overrides[ref.id] as object),
      };
    },
  };
}

const portal = {
  id: "admin", tenant: "acme", route: "/", designSystem: { ...arcoRef, uiContract: "^1.0.0" }, plugins: [arcoRef, composerRef],
};

describe("PortalRuntime", () => {
  it("only assembles one signed first-party design system and framework-neutral plugins", async () => {
    const prepared = await new PortalRuntime(loader()).prepare(portal);
    expect(prepared.designSystem.framework).toBe("arco");
    expect(prepared.routes).toEqual([{ path: "/settings/portals", pluginID: composerRef.id }]);
  });

  it("fails closed for an untrusted design system", async () => {
    const runtime = new PortalRuntime(loader({ [arcoRef.id]: { provenance: { signed: false, firstParty: true, integrity: "sha256:test" } } }));
    await expect(runtime.prepare(portal)).rejects.toMatchObject({ code: "UNTRUSTED_REMOTE" } satisfies Partial<PortalAssemblyError>);
  });

  it("rejects a second design system contribution", async () => {
    const runtime = new PortalRuntime(loader({ [composerRef.id]: { designSystem: adapter } }));
    await expect(runtime.prepare(portal)).rejects.toMatchObject({ code: "SECOND_DESIGN_SYSTEM" } satisfies Partial<PortalAssemblyError>);
  });
});
