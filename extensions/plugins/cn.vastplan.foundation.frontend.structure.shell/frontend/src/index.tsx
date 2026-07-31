import type { UIShellAdapter } from "@vastplan/ui-primitives";
import { uiContractVersion } from "@vastplan/ui-contract";
import { compose } from "./composition";
import { shellLibraryPluginVersions } from "./template-versions.generated.js";

const adapter: UIShellAdapter = {
  id: "ui.structure.shell",
  uiContract: uiContractVersion,
  templates: [
    { id: "standard", label: { namespace: "cn.vastplan.foundation.frontend.structure.shell", key: "template.standard", fallback: "标准侧栏" }, module: { id: "cn.vastplan.foundation.frontend.structure.layout.standard", version: shellLibraryPluginVersions.standard, channel: "stable" } },
    { id: "top-navigation", label: { namespace: "cn.vastplan.foundation.frontend.structure.shell", key: "template.topNavigation", fallback: "顶部导航" }, module: { id: "cn.vastplan.foundation.frontend.structure.layout.top-navigation", version: shellLibraryPluginVersions.topNavigation, channel: "stable" } },
  ],
  defaultTemplate: "standard",
  compose,
  localization: {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": {
        "template.standard": "标准侧栏", "template.topNavigation": "顶部导航",
        "navigation.primary": "主要功能", "navigation.secondary": "辅助功能", "navigation.settings": "系统设置",
        "navigation.account": "用户", "navigation.accountSettings": "用户设置", "account.open": "打开用户功能",
      },
      "en-US": {
        "template.standard": "Standard sidebar", "template.topNavigation": "Top navigation",
        "navigation.primary": "Primary", "navigation.secondary": "Secondary", "navigation.settings": "System settings",
        "navigation.account": "Account", "navigation.accountSettings": "User settings", "account.open": "Open account functions",
      },
    },
  },
};

export const localization = adapter.localization;
export default adapter;
