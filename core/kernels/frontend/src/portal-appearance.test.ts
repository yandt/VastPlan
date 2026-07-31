import { beforeEach, describe, expect, it, vi } from "vitest";
import { PortalAppearanceSession } from "./portal-appearance";

const portal = {
  id: "operations", tenantId: "tenant-a", account: { subjectID: "alice", tenantID: "tenant-a", displayName: "Alice" },
  renderAdapter: { config: { defaultRenderer: "antd", allowedRenderers: ["antd", "alternate"], userSelectable: true, rendererOptions: {
    antd: { allowedThemeTemplates: ["light", "light-soft", "dark", "dark-midnight"], allowedIconThemes: ["canonical", "renderer-native"], themeTemplate: "light", iconTheme: "canonical" },
    alternate: { allowedThemeTemplates: ["light", "dark"], allowedIconThemes: ["canonical"] },
  } } },
  shell: { config: { defaultTemplate: "standard", allowedTemplates: ["standard", "top-navigation"], userSelectable: true } },
} as any;

beforeEach(() => {
  const values = new Map<string, string>();
  vi.stubGlobal("localStorage", { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value) });
  vi.stubGlobal("matchMedia", () => ({ matches: false }));
});

describe("PortalAppearanceSession", () => {
  it("persists all appearance choices locally without a network port", () => {
    const session = PortalAppearanceSession.open(portal);
    session.setRenderer("alternate");
    session.setShellTemplate("top-navigation");
    session.setAppearance("alternate", { mode: "dark", light: { templateID: "light" }, dark: { templateID: "dark", colors: { primary: "#112233" } } });
    expect(PortalAppearanceSession.open(portal).resolve()).toMatchObject({ rendererID: "alternate", shellTemplateID: "top-navigation", themeTemplateID: "dark" });
    PortalAppearanceSession.open(portal).commitPendingRenderer();
    expect(PortalAppearanceSession.open(portal).appearance("alternate").dark.colors?.primary).toBe("#112233");
  });

  it("isolates values by user", () => {
    const alice = PortalAppearanceSession.open(portal);
    alice.setRenderer("alternate");
    alice.commitPendingRenderer();
    const bob = PortalAppearanceSession.open({ ...portal, account: { ...portal.account, subjectID: "bob" } });
    expect(bob.resolve().rendererID).toBe("antd");
  });

  it("can roll back a renderer that failed during the host epoch", () => {
    const session = PortalAppearanceSession.open(portal);
    session.setRenderer("alternate");
    expect(session.hasPendingRenderer()).toBe(true);
    session.discardPendingRenderer();
    expect(session.resolve().rendererID).toBe("antd");
  });

  it("keeps theme settings when renderer switching is locked", () => {
    const locked = { ...portal, renderAdapter: { config: { ...portal.renderAdapter.config, userSelectable: false } } };
    const session = PortalAppearanceSession.open(locked);
    session.setAppearance("antd", { mode: "dark", light: { templateID: "light" }, dark: { templateID: "dark" } });
    expect(PortalAppearanceSession.open(locked).appearance("antd").mode).toBe("dark");
  });
});
