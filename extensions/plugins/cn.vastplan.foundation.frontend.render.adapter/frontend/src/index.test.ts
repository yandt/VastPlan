import { describe, expect, it } from "vitest";
import { uiContractVersion } from "@vastplan/ui-contract";
import adapter from "./index";
import { rendererPluginVersions } from "./renderer-versions.generated";

describe("unified render adapter", () => {
  it("publishes an Ant-only product catalog through the generic Renderer protocol", () => {
    expect(adapter).toMatchObject({ id: "ui.render.adapter", uiContract: uiContractVersion, defaultRenderer: "antd" });
    expect(adapter.renderers.map((renderer) => renderer.id)).toEqual(["antd"]);
    expect(adapter.renderers.map((renderer) => renderer.module.id)).toEqual(["cn.vastplan.foundation.frontend.render.adapter.antd"]);
    expect(adapter.renderers.map((renderer) => renderer.module.version)).toEqual([rendererPluginVersions.antd]);
  });

  it("keeps Renderer labels in the Adapter namespace", () => {
    expect(adapter.renderers.every((renderer) => typeof renderer.label !== "string" && renderer.label.namespace === "cn.vastplan.foundation.frontend.render.adapter")).toBe(true);
  });
});
