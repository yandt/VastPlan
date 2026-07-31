import type { UIRenderAdapter } from "@vastplan/ui-primitives";
import { uiContractVersion } from "@vastplan/ui-contract";

/**
 * The only Portal render-adapter foundation. Frameworks are trusted internal
 * renderer modules. The trusted Portal host derives the exact catalog from the
 * current Manifest Contribution Index and fetches only the selected module.
 */
const adapter: UIRenderAdapter = {
  id: "ui.render.adapter",
  uiContract: uiContractVersion,
  renderers: [],
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
