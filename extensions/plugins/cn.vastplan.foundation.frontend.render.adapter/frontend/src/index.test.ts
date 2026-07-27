import { describe, expect, it } from "vitest";
import adapter from "./index";

describe("unified render adapter", () => {
  it("owns the complete first-party Renderer catalog", () => {
    expect(adapter).toMatchObject({ id: "ui.render.adapter", uiContract: "4.0.0", defaultRenderer: "antd" });
    expect(adapter.renderers.map((renderer) => renderer.id)).toEqual(["antd", "arco", "mui"]);
    expect(adapter.renderers.map((renderer) => renderer.module.id)).toEqual([
      "cn.vastplan.foundation.frontend.render.adapter.antd",
      "cn.vastplan.foundation.frontend.render.adapter.arco",
      "cn.vastplan.foundation.frontend.render.adapter.mui",
    ]);
    expect(adapter.renderers.map((renderer) => renderer.module.version)).toEqual(["1.0.2", "1.6.4", "1.7.5"]);
  });

  it("keeps Renderer labels in the Adapter namespace", () => {
    expect(adapter.renderers.every((renderer) => typeof renderer.label !== "string" && renderer.label.namespace === "cn.vastplan.foundation.frontend.render.adapter")).toBe(true);
  });
});
