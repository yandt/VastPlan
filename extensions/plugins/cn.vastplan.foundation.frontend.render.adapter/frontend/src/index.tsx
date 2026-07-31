import type { UIRenderAdapter } from "@vastplan/ui-primitives";
import { uiContractVersion } from "@vastplan/ui-contract";
import { rendererPluginVersions } from "./renderer-versions.generated.js";

/**
 * The only Portal render-adapter foundation. Frameworks are trusted internal
 * renderer modules, selected from this catalog instead of becoming competing
 * roots. Framework code is intentionally not imported here: the Portal host
 * fetches exactly one verified Renderer after profile selection. The current
 * product catalog intentionally contains only Ant Design; the protocol remains
 * open to additional signed Renderer plugins later.
 */
const adapter: UIRenderAdapter = {
  id: "ui.render.adapter",
  uiContract: uiContractVersion,
  renderers: [
    { id: "antd", label: { namespace: "cn.vastplan.foundation.frontend.render.adapter", key: "renderer.antd", fallback: "Ant Design" }, framework: "antd", module: { id: "cn.vastplan.foundation.frontend.render.adapter.antd", version: rendererPluginVersions.antd, channel: "stable" } },
  ],
  defaultRenderer: "antd",
  localization: {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": { "renderer.antd": "Ant Design" },
      "en-US": { "renderer.antd": "Ant Design" },
    },
  },
};

export const localization = adapter.localization;
export default adapter;
