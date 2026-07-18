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
          context.addRoute({ path: "/settings/portals", component: () => null });
          context.addMenu({ id: "portal-composer", title: "门户组合", route: "/settings/portals" });
        },
        ...(overrides[ref.id] as object),
      };
    },
  };
}

const portal = {
  revision: 1, id: "admin", tenantId: "acme", route: "/", designSystem: { ...arcoRef, uiContract: "^1.0.0" }, plugins: [arcoRef, composerRef],
  resolution: {
    platformProfile: { id: "portal-default", revision: 1, digest: "a".repeat(64) },
    applicationComposition: { id: "admin", revision: 1, digest: "b".repeat(64) },
    pluginOrigins: { [arcoRef.id]: "platform-profile" as const, [composerRef.id]: "platform-profile" as const },
  },
};

describe("PortalRuntime", () => {
  it("only assembles one signed first-party design system and framework-neutral plugins", async () => {
    const prepared = await new PortalRuntime(loader()).prepare(portal);
    expect(prepared.designSystem.framework).toBe("arco");
    expect(prepared.routes).toHaveLength(1);
    expect(prepared.routes[0]).toMatchObject({ path: "/settings/portals", pluginID: composerRef.id });
  });

  it("fails closed for an untrusted design system", async () => {
    const runtime = new PortalRuntime(loader({ [arcoRef.id]: { provenance: { signed: false, firstParty: true, integrity: "sha256:test" } } }));
    await expect(runtime.prepare(portal)).rejects.toMatchObject({ code: "UNTRUSTED_REMOTE" } satisfies Partial<PortalAssemblyError>);
  });

  it("rejects a second design system contribution", async () => {
    const runtime = new PortalRuntime(loader({ [composerRef.id]: { designSystem: adapter } }));
    await expect(runtime.prepare(portal)).rejects.toMatchObject({ code: "SECOND_DESIGN_SYSTEM" } satisfies Partial<PortalAssemblyError>);
  });

  it("rejects a design system selected by the application input", async () => {
    const invalid = { ...portal, resolution: { ...portal.resolution, pluginOrigins: { ...portal.resolution.pluginOrigins, [arcoRef.id]: "application" as const } } };
    await expect(new PortalRuntime(loader()).prepare(invalid)).rejects.toMatchObject({ code: "ORIGIN_LOCK_INVALID" } satisfies Partial<PortalAssemblyError>);
  });
});
