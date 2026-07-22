import type { UIShellAdapter } from "@vastplan/ui-primitives";
import { compose } from "./composition";

const adapter: UIShellAdapter = {
  id: "ui.structure.shell",
  uiContract: "4.0.0",
  templates: [
    { id: "standard", label: { namespace: "cn.vastplan.foundation.frontend.structure.shell", key: "template.standard", fallback: "标准侧栏" }, module: { id: "cn.vastplan.foundation.frontend.structure.layout.standard", version: "1.1.0", channel: "stable" } },
    { id: "top-navigation", label: { namespace: "cn.vastplan.foundation.frontend.structure.shell", key: "template.topNavigation", fallback: "顶部导航" }, module: { id: "cn.vastplan.foundation.frontend.structure.layout.top-navigation", version: "1.1.0", channel: "stable" } },
  ],
  defaultTemplate: "standard",
  compose,
  localization: {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": {
        "template.standard": "标准侧栏", "template.topNavigation": "顶部导航",
        "navigation.primary": "主要功能", "navigation.secondary": "辅助功能", "navigation.settings": "系统设置",
      },
      "en-US": {
        "template.standard": "Standard sidebar", "template.topNavigation": "Top navigation",
        "navigation.primary": "Primary", "navigation.secondary": "Secondary", "navigation.settings": "System settings",
      },
    },
  },
};

export const localization = adapter.localization;
export default adapter;
