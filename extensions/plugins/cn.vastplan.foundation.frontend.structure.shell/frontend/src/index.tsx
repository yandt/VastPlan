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
        "navigation.account": "用户", "account.open": "打开用户功能", "account.tooltip": "用户信息：{name}",
      },
      "en-US": {
        "template.standard": "Standard sidebar", "template.top-navigation": "Top navigation",
        "navigation.account": "Account", "account.open": "Open account functions", "account.tooltip": "User: {name}",
      },
    },
  },
};

export const localization = adapter.localization;
export default adapter;
