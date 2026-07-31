import { describe, expect, it } from "vitest";
import { uiContractVersion } from "@vastplan/ui-contract";
import adapter from "./index";

describe("unified render adapter", () => {
  it("publishes renderer semantics without embedding exact module versions", () => {
    expect(adapter).toMatchObject({ id: "ui.render.adapter", uiContract: uiContractVersion, defaultRenderer: "antd" });
    expect(adapter.renderers).toEqual([]);
  });

  it("keeps Renderer labels in the Adapter namespace", () => {
    expect(adapter.localization?.messages["zh-CN"]?.["renderer.antd"]).toBe("Ant Design");
  });
});
