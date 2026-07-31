import type { UIShellAdapter } from "@vastplan/ui-primitives";
import { uiContractVersion } from "@vastplan/ui-contract";
import { compose } from "./composition";

const adapter: UIShellAdapter = {
  id: "ui.structure.shell",
  uiContract: uiContractVersion,
  templates: [],
  defaultTemplate: "standard",
  compose,
  localization: {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": {
        "template.standard": "标准侧栏", "template.top-navigation": "顶部导航",
        "navigation.primary": "主要功能", "navigation.secondary": "辅助功能", "navigation.settings": "系统设置",
        "navigation.account": "用户", "navigation.accountSettings": "用户设置", "account.open": "打开用户功能",
      },
      "en-US": {
        "template.standard": "Standard sidebar", "template.top-navigation": "Top navigation",
        "navigation.primary": "Primary", "navigation.secondary": "Secondary", "navigation.settings": "System settings",
        "navigation.account": "Account", "navigation.accountSettings": "User settings", "account.open": "Open account functions",
      },
    },
  },
};

export const localization = adapter.localization;
export default adapter;
