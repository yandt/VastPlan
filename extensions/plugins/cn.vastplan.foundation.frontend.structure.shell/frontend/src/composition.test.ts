import { describe, expect, it } from "vitest";
import { compose } from "./composition";

describe("shell composition core", () => {
  it("owns stable slots and deterministic navigation/content order", () => {
    const Body = () => null;
    const Action = () => null;
    const model = compose({
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
    const model = compose({
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

  it("remaps stable plugin semantics through the selected Portal navigation policy", () => {
    const model = compose({
      activePageID: "deployments",
      shellContributions: [],
      config: {
        navigationGroups: [
          { id: "operations", label: "服务与部署", zone: "primary", icon: "menu", order: 10 },
          { id: "operations.deployments", parentID: "operations", label: "部署管理", zone: "primary", icon: "menu", order: 10 },
        ],
        navigationPlacements: [{ semanticID: "platform.operations.deployment", groupID: "operations.deployments" }],
      },
      pages: [{
        id: "deployments", pluginID: "cn.vastplan.platform.infrastructure.deployment-manager", path: "/deployments", title: "部署管理",
        navigation: { id: "deployments", label: "部署管理", semanticID: "platform.operations.deployment", zone: "settings" },
        slots: [{ id: "body", slot: "page.body.main", component: () => null }],
      }],
    });
    expect(model.navigation.settings).toEqual([]);
    expect(model.navigation.primary[0].children[0].pages[0]).toMatchObject({ id: "deployments", zone: "primary", groupID: "operations.deployments" });
    expect(model.activeNavigationPath).toEqual({ zone: "primary", rootGroupID: "operations", childGroupID: "operations.deployments", pageID: "deployments" });
  });

  it("rejects duplicate and unknown semantic navigation mappings", () => {
    const page = {
      id: "deployments", pluginID: "cn.vastplan.platform.test", path: "/deployments", title: "部署管理",
      navigation: { id: "deployments", label: "部署管理", semanticID: "platform.operations.deployment", zone: "settings" as const },
      slots: [{ id: "body", slot: "page.body.main" as const, component: () => null }],
    };
    expect(() => compose({ pages: [page], shellContributions: [], config: { navigationPlacements: [
      { semanticID: "platform.operations.deployment", groupID: "primary" },
      { semanticID: "platform.operations.deployment", groupID: "primary" },
    ] } })).toThrow("无效或重复映射");
    expect(() => compose({ pages: [page], shellContributions: [], config: { navigationPlacements: [
      { semanticID: "platform.operations.deployment", groupID: "missing" },
    ] } })).toThrow("未知分组");
  });

  it("composes account plugins through the same root-child-page navigation pipeline", () => {
    const model = compose({
      activePageID: "appearance",
      shellContributions: [],
      pages: [
        { id: "profile", pluginID: "cn.vastplan.foundation.frontend.identity.account-center", path: "/account/profile", title: "用户信息", navigation: { id: "account.profile", label: "用户信息", zone: "secondary", groupID: "account" }, slots: [{ id: "body", slot: "page.body.main", component: () => null }] },
        { id: "appearance", pluginID: "cn.vastplan.foundation.frontend.identity.account-center", path: "/account/settings/appearance", title: "外观", navigation: { id: "account.appearance", label: "外观", zone: "secondary", groupID: "account.settings" }, slots: [{ id: "body", slot: "page.body.main", component: () => null }] },
      ],
    });
    const account = model.navigation.secondary.find((group) => group.id === "account");
    expect(account?.pages.map((page) => page.id)).toEqual(["account.profile"]);
    expect(account?.children[0]).toMatchObject({ id: "account.settings", parentID: "account" });
    expect(account?.children[0].pages.map((page) => page.id)).toEqual(["account.appearance"]);
    expect(model.activeNavigationPath).toEqual({ zone: "secondary", rootGroupID: "account", childGroupID: "account.settings", pageID: "account.appearance" });
  });

  it("keeps the account root available when the account feature plugin is absent", () => {
    const model = compose({ pages: [], shellContributions: [] });
    const account = model.navigation.secondary.find((group) => group.id === "account");
    expect(account).toMatchObject({ id: "account", zone: "secondary" });
    expect(account?.pages).toEqual([]);
    expect(account?.children).toEqual([]);
  });

  it("rejects unknown parents, cross-zone children, and a third group level", () => {
    const attempt = (navigationGroups: unknown[]) => () => compose({ pages: [], shellContributions: [], config: { navigationGroups } });
    expect(attempt([{ id: "child", parentID: "missing", label: "子组", zone: "primary", icon: "menu" }])).toThrow("未知根组");
    expect(attempt([
      { id: "root", label: "根组", zone: "primary", icon: "menu" },
      { id: "child", parentID: "root", label: "子组", zone: "settings", icon: "settings" },
    ])).toThrow("不能跨语义区");
    expect(attempt([
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
    const model = compose({
      pages: [page], shellContributions: [],
      config: { navigationGroups: [{ id: "operations", label: "运行管理", zone: "primary", icon: "menu", order: 5 }] },
    });
    expect(model.navigation.primary[0]).toMatchObject({ id: "operations", label: "运行管理", icon: "menu" });
    expect(() => compose({ pages: [page], shellContributions: [] })).toThrow("未治理的分组");
  });

  it("keeps global shell contributions independent from the active page", () => {
    const Logo = () => null;
    const model = compose({
      pages: [],
      shellContributions: [{ id: "logo", pluginID: "cn.vastplan.foundation.brand", slot: "shell.navigation.start", component: Logo }],
    });
    expect(model.shellSlots["shell.navigation.start"]?.[0].component).toBe(Logo);
    expect(model.pageSlots).toEqual({});
  });
});
