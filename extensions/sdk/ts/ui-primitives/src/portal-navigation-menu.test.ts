import { describe, expect, it } from "vitest";
import { composedNavigationMenuItems } from "./portal-navigation-menu";
import { accountNavigationNodeID, type PortalNavigationGroup, type ShellCompositionModel } from "./portal-runtime";

const i18n = { locale: "zh-CN", text: (value: unknown) => typeof value === "string" ? value : (value as { fallback: string }).fallback };
const composition = (pages: unknown): ShellCompositionModel => ({ pages, navigation: { primary: [], secondary: [], settings: [] }, navigationCollections: { primary: [], secondary: [], settings: [] }, shellSlots: {}, pageSlots: {} } as ShellCompositionModel);

describe("PortalNavigationMenu", () => {
  it("projects the same direct and nested page model for inline and popup renderers", () => {
    const group: PortalNavigationGroup = {
      id: "cn.vastplan.example/operations",
      label: "Operations",
      zone: "primary",
      icon: { kind: "semantic", name: "menu" },
      pages: [{ id: "overview", label: "Overview", zone: "primary", groupID: "cn.vastplan.example/operations", parentMenuRef: { pluginID: "cn.vastplan.example", nodeID: "operations" } }],
      children: [{
        id: "audit",
        label: "Audit",
        zone: "primary",
        icon: { kind: "semantic", name: "menu" },
        parentID: "cn.vastplan.example/operations",
        pages: [{ id: "events", label: "Events", zone: "primary", groupID: "cn.vastplan.example/operations", parentMenuRef: { pluginID: "cn.vastplan.example", nodeID: "operations" } }],
      }],
    };
    const items = composedNavigationMenuItems([group], composition([{ id: "overview-page", path: "/overview", navigation: { id: "overview" } }, { id: "events-page", path: "/events", navigation: { id: "events" } }]), i18n, false);
    expect(items).toMatchObject([{ id: "overview", href: "/overview" }, { id: "group:audit", children: [{ id: "events", href: "/events" }] }]);
  });

  it("adds trusted logout only to the composed account root", () => {
    const group: PortalNavigationGroup = { id: accountNavigationNodeID, label: "Account", zone: "secondary", icon: { kind: "semantic", name: "menu" }, pages: [], children: [] };
    expect(composedNavigationMenuItems([group], composition([]), i18n, true)).toMatchObject([{ id: "account.logout" }]);
    expect(composedNavigationMenuItems([group], composition([]), i18n, false)).toEqual([]);
  });

  it("renders collected roots as sections without adding a root submenu", () => {
    const root = (id: string, page: string): PortalNavigationGroup => ({
      id, label: id, zone: "primary", icon: { kind: "semantic", name: "menu" },
      pages: [{ id: page, label: page, zone: "primary", groupID: id, parentMenuRef: { pluginID: "cn.vastplan.example", nodeID: id.split("/")[1]! } }], children: [],
    });
    const items = composedNavigationMenuItems(
      [root("cn.vastplan.example/one", "page-one"), root("cn.vastplan.example/two", "page-two")],
      composition([{ id: "one", path: "/one", navigation: { id: "page-one" } }, { id: "two", path: "/two", navigation: { id: "page-two" } }]),
      i18n,
      false,
      "section",
    );
    expect(items).toMatchObject([
      { id: "group:cn.vastplan.example/one", kind: "section", children: [{ id: "page-one" }] },
      { id: "group:cn.vastplan.example/two", kind: "section", children: [{ id: "page-two" }] },
    ]);
  });
});
