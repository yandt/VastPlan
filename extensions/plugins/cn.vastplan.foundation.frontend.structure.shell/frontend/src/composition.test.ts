import { describe, expect, it } from "vitest";
import type { PortalNavigationCatalog, PortalRegisteredPage } from "@vastplan/ui-primitives";
import { compose } from "./composition";

const pluginID = "cn.vastplan.platform.test";
const icon = { kind: "semantic" as const, name: "menu" as const };

function catalog(nodes: PortalNavigationCatalog["nodes"]): PortalNavigationCatalog {
  return { pluginID, nodes };
}

function node(id: string, zone: "primary" | "settings" | "secondary", parentID?: string) {
  return {
    id: `${pluginID}/${id}`,
    ref: { pluginID, nodeID: id },
    label: id,
    zone,
    icon,
    ...(parentID === undefined ? {} : { parent: { pluginID, nodeID: parentID, mode: "required" as const } }),
  };
}

function page(id: string, nodeID: string): PortalRegisteredPage {
  return {
    id,
    pluginID,
    path: `/${id}`,
    title: id,
    navigation: { id, label: id, parentMenuRef: { pluginID, nodeID } },
    slots: [{ id: "body", slot: "page.body.main", component: () => null }],
  };
}

describe("shell composition core", () => {
  it("owns stable slots and deterministic navigation/content order", () => {
    const Body = () => null;
    const Action = () => null;
    const settings = { ...page("settings-page", "settings"), slots: [
      { id: "body", slot: "page.body.main" as const, component: Body, order: 20 },
      { id: "action", slot: "page.header.end" as const, component: Action, order: 10 },
    ] };
    const model = compose({
      activePageID: settings.id,
      navigationCatalogs: [catalog([node("settings", "settings")])],
      shellContributions: [],
      pages: [settings],
    });
    expect(model.navigation.settings[0].id).toBe(`${pluginID}/settings`);
    expect(model.activeNavigationPath).toEqual({ zone: "settings", rootGroupID: `${pluginID}/settings`, pageID: settings.id });
    expect(model.pageSlots["page.header.end"]?.[0].component).toBe(Action);
    expect(model.pageSlots["page.body.main"]?.[0].component).toBe(Body);
  });

  it("builds one bounded child-group level and one authoritative active path", () => {
    const model = compose({
      activePageID: "workers",
      navigationCatalogs: [catalog([node("operations", "primary"), node("compute", "primary", "operations")])],
      shellContributions: [],
      pages: [page("overview", "operations"), page("workers", "compute")],
    });
    expect(model.navigation.primary[0].pages.map((item) => item.id)).toEqual(["overview"]);
    expect(model.navigation.primary[0].children[0].pages.map((item) => item.id)).toEqual(["workers"]);
    expect(model.activeNavigationPath).toEqual({ zone: "primary", rootGroupID: `${pluginID}/operations`, childGroupID: `${pluginID}/compute`, pageID: "workers" });
  });

  it("applies Portal overrides without creating navigation nodes", () => {
    const model = compose({
      activePageID: "workers",
      navigationCatalogs: [catalog([node("operations", "primary"), node("compute", "primary")])],
      shellContributions: [],
      config: { navigationOverrides: [{ target: `${pluginID}/compute`, parent: `${pluginID}/operations`, order: 5, labels: { "zh-CN": "计算资源" } }] },
      pages: [page("workers", "compute")],
    });
    expect(model.navigation.primary[0].children[0]).toMatchObject({ id: `${pluginID}/compute`, parentID: `${pluginID}/operations`, labels: { "zh-CN": "计算资源" } });
    expect(() => compose({ navigationCatalogs: [catalog([node("operations", "primary")])], pages: [], shellContributions: [], config: { navigationOverrides: [{ target: `${pluginID}/missing` }] } })).toThrow("未知覆盖");
    expect(() => compose({ navigationCatalogs: [catalog([node("operations", "primary")])], pages: [], shellContributions: [], config: { navigationOverrides: [{ target: `${pluginID}/operations`, parent: "vastplan.host/account" }] } })).toThrow("无效、重复或未知覆盖");
  });

  it("accepts plugin-owned account menus beneath the only trusted host anchor", () => {
    const accountParent = { pluginID: "vastplan.host", nodeID: "account", mode: "required" as const };
    const accountCatalog: PortalNavigationCatalog = {
      pluginID,
      nodes: [
        { ...node("profile", "secondary"), parent: accountParent },
        { ...node("preferences", "secondary"), parent: accountParent },
      ],
    };
    const accountPage = page("profile", "profile");
    const appearancePage = page("appearance", "preferences");
    const model = compose({ activePageID: "appearance", navigationCatalogs: [accountCatalog], accountNavigationOwnerID: pluginID, pages: [accountPage, appearancePage], shellContributions: [] });
    const account = model.navigation.secondary.find((group) => group.id === "vastplan.host/account");
    expect(account?.pages).toEqual([]);
    expect(account?.children.map((child) => child.id)).toEqual([`${pluginID}/preferences`, `${pluginID}/profile`]);
    expect(account?.children.find((child) => child.id === `${pluginID}/preferences`)?.pages.map((item) => item.id)).toEqual(["appearance"]);
    expect(model.activeNavigationPath).toEqual({ zone: "secondary", rootGroupID: "vastplan.host/account", childGroupID: `${pluginID}/preferences`, pageID: "appearance" });
    expect(() => compose({ navigationCatalogs: [accountCatalog], accountNavigationOwnerID: "cn.vastplan.other", pages: [accountPage], shellContributions: [] })).toThrow("只有当前个人中心插件");
  });

  it("rejects unknown page nodes and cross-zone or overly deep catalogs", () => {
    expect(() => compose({ navigationCatalogs: [], pages: [page("jobs", "missing")], shellContributions: [] })).toThrow("未安装菜单");
    expect(() => compose({ navigationCatalogs: [catalog([node("root", "primary"), node("child", "settings", "root")])], pages: [], shellContributions: [] })).toThrow("跨 zone");
    expect(() => compose({ navigationCatalogs: [catalog([node("root", "primary"), node("child", "primary", "root"), node("deep", "primary", "child")])], pages: [], shellContributions: [] })).toThrow("深度超过");
  });

  it("keeps global shell contributions independent from the active page", () => {
    const Logo = () => null;
    const model = compose({ navigationCatalogs: [], pages: [], shellContributions: [{ id: "logo", pluginID: "cn.vastplan.foundation.brand", slot: "shell.navigation.start", component: Logo }] });
    expect(model.shellSlots["shell.navigation.start"]?.[0].component).toBe(Logo);
    expect(model.pageSlots).toEqual({});
  });
});
