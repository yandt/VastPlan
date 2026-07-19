import type { UIRenderAdapter } from "@vastplan/ui-primitives";
import { arcoRenderer } from "@vastplan/ui-render-adapter-arco";
import { muiRenderer } from "@vastplan/ui-render-adapter-mui";

/**
 * The only Portal render-adapter foundation. Frameworks are trusted internal
 * renderers, selected from this catalog instead of becoming competing roots.
 */
const adapter: UIRenderAdapter = {
  id: "ui.render.adapter",
  uiContract: "4.0.0",
  renderers: [arcoRenderer, muiRenderer],
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
