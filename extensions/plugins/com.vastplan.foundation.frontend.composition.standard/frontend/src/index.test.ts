import { describe, expect, it } from "vitest";
import adapter from "./index";

describe("standard shell composition", () => {
  it("owns stable slots and deterministic navigation/content order", () => {
    const Body = () => null;
    const Action = () => null;
    const model = adapter.compose({
      activePageID: "settings",
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
    expect(model.navigation.settings.map((item) => item.id)).toEqual(["settings"]);
    expect(model.slots["page.header.end"]?.[0].component).toBe(Action);
    expect(model.slots["page.body.main"]?.[0].component).toBe(Body);
  });
});
