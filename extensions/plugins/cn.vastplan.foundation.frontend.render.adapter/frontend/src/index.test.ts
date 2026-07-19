import { describe, expect, it } from "vitest";
import adapter from "./index";

describe("unified render adapter", () => {
  it("owns the complete first-party Renderer catalog", () => {
    expect(adapter).toMatchObject({ id: "ui.render.adapter", uiContract: "4.0.0", defaultRenderer: "arco" });
    expect(adapter.renderers.map((renderer) => renderer.id)).toEqual(["arco", "mui"]);
  });

  it("keeps Renderer labels in the Adapter namespace", () => {
    expect(adapter.renderers.every((renderer) => typeof renderer.label !== "string" && renderer.label.namespace === "cn.vastplan.foundation.frontend.render.adapter")).toBe(true);
  });
});
