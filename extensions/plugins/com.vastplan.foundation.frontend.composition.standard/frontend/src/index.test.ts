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
        id: "settings", pluginID: "com.vastplan.platform.test", path: "/settings", title: "设置",
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
    expect(model.pageSlots["page.header.end"]?.[0].component).toBe(Action);
    expect(model.pageSlots["page.body.main"]?.[0].component).toBe(Body);
  });

  it("uses governed group descriptors and rejects unknown groups", () => {
    const page = {
      id: "jobs", pluginID: "com.vastplan.platform.test", path: "/jobs", title: "任务",
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
      shellContributions: [{ id: "logo", pluginID: "com.vastplan.foundation.brand", slot: "shell.navigation.start", component: Logo }],
    });
    expect(model.shellSlots["shell.navigation.start"]?.[0].component).toBe(Logo);
    expect(model.pageSlots).toEqual({});
  });
});
