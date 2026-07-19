import { describe, expect, it } from "vitest";
import adapter from "./index";

describe("standard shell composition", () => {
  it("owns stable slots and deterministic navigation/content order", () => {
    const Body = () => null;
    const Action = () => null;
    const model = adapter.compose({
      activePageID: "settings",
      shellContributions: [],
      pages: [{
        id: "settings", pluginID: "cn.vastplan.platform.test", path: "/settings", title: "设置",
        navigation: { id: "settings", label: "设置", zone: "settings", order: 20 },
        slots: [
          { id: "body", slot: "page.body.main", component: Body, order: 20 },
          { id: "action", slot: "page.header.end", component: Action, order: 10 },
        ],
      }],
    });
    expect(model.activePage?.id).toBe("settings");
    expect(model.navigation.settings.map((group) => group.id)).toEqual(["settings"]);
    expect(model.navigation.settings[0].pages.map((item) => item.id)).toEqual(["settings"]);
    expect(model.activeNavigationPath).toEqual({ zone: "settings", rootGroupID: "settings", pageID: "settings" });
    expect(model.pageSlots["page.header.end"]?.[0].component).toBe(Action);
    expect(model.pageSlots["page.body.main"]?.[0].component).toBe(Body);
  });

  it("builds one bounded child-group level and one authoritative active path", () => {
    const model = adapter.compose({
      activePageID: "workers",
      shellContributions: [],
      config: { navigationGroups: [
        { id: "operations", label: "运行管理", zone: "primary", icon: "menu", order: 5 },
        { id: "compute", parentID: "operations", label: "计算资源", zone: "primary", icon: "settings", order: 10 },
      ] },
      pages: [
        { id: "overview", pluginID: "cn.vastplan.platform.test", path: "/overview", title: "概览", navigation: { id: "overview", label: "概览", zone: "primary", groupID: "operations" }, slots: [{ id: "body", slot: "page.body.main", component: () => null }] },
        { id: "workers", pluginID: "cn.vastplan.platform.test", path: "/workers", title: "工作节点", navigation: { id: "workers", label: "工作节点", zone: "primary", groupID: "compute" }, slots: [{ id: "body", slot: "page.body.main", component: () => null }] },
      ],
    });
    expect(model.navigation.primary[0].pages.map((page) => page.id)).toEqual(["overview"]);
    expect(model.navigation.primary[0].children[0].pages.map((page) => page.id)).toEqual(["workers"]);
    expect(model.activeNavigationPath).toEqual({ zone: "primary", rootGroupID: "operations", childGroupID: "compute", pageID: "workers" });
  });

  it("rejects unknown parents, cross-zone children, and a third group level", () => {
    const compose = (navigationGroups: unknown[]) => () => adapter.compose({ pages: [], shellContributions: [], config: { navigationGroups } });
    expect(compose([{ id: "child", parentID: "missing", label: "子组", zone: "primary", icon: "menu" }])).toThrow("未知根组");
    expect(compose([
      { id: "root", label: "根组", zone: "primary", icon: "menu" },
      { id: "child", parentID: "root", label: "子组", zone: "settings", icon: "settings" },
    ])).toThrow("不能跨语义区");
    expect(compose([
      { id: "root", label: "根组", zone: "primary", icon: "menu" },
      { id: "child", parentID: "root", label: "子组", zone: "primary", icon: "menu" },
      { id: "too-deep", parentID: "child", label: "过深", zone: "primary", icon: "menu" },
    ])).toThrow("导航深度超过");
  });

  it("uses governed group descriptors and rejects unknown groups", () => {
    const page = {
      id: "jobs", pluginID: "cn.vastplan.platform.test", path: "/jobs", title: "任务",
      navigation: { id: "jobs", label: "任务", zone: "primary" as const, groupID: "operations" },
      slots: [{ id: "body", slot: "page.body.main" as const, component: () => null }],
    };
    const model = adapter.compose({
      pages: [page], shellContributions: [],
      config: { navigationGroups: [{ id: "operations", label: "运行管理", zone: "primary", icon: "menu", order: 5 }] },
    });
    expect(model.navigation.primary[0]).toMatchObject({ id: "operations", label: "运行管理", icon: "menu" });
    expect(() => adapter.compose({ pages: [page], shellContributions: [] })).toThrow("未治理的分组");
  });

  it("keeps global shell contributions independent from the active page", () => {
    const Logo = () => null;
    const model = adapter.compose({
      pages: [],
      shellContributions: [{ id: "logo", pluginID: "cn.vastplan.foundation.brand", slot: "shell.navigation.start", component: Logo }],
    });
    expect(model.shellSlots["shell.navigation.start"]?.[0].component).toBe(Logo);
    expect(model.pageSlots).toEqual({});
  });
});
