import type { UIRenderAdapter } from "@vastplan/ui-primitives";

/**
 * The only Portal render-adapter foundation. Frameworks are trusted internal
 * renderer modules, selected from this catalog instead of becoming competing
 * roots. Framework code is intentionally not imported here: the Portal host
 * fetches exactly one verified Renderer after profile selection.
 */
const adapter: UIRenderAdapter = {
  id: "ui.render.adapter",
  uiContract: "4.0.0",
  renderers: [
    { id: "arco", label: { namespace: "cn.vastplan.foundation.frontend.render.adapter", key: "renderer.arco", fallback: "Arco Design" }, framework: "arco", module: { id: "cn.vastplan.foundation.frontend.render.adapter.arco", version: "1.3.0", channel: "stable" } },
    { id: "mui", label: { namespace: "cn.vastplan.foundation.frontend.render.adapter", key: "renderer.mui", fallback: "Material UI" }, framework: "mui", module: { id: "cn.vastplan.foundation.frontend.render.adapter.mui", version: "1.3.0", channel: "stable" } },
  ],
  defaultRenderer: "arco",
  localization: {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": { "renderer.arco": "Arco Design", "renderer.mui": "Material UI" },
      "en-US": { "renderer.arco": "Arco Design", "renderer.mui": "Material UI" },
    },
  },
};

export const localization = adapter.localization;
export default adapter;
