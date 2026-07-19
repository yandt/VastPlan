import type { UIShellAdapter, UIShellProps } from "@vastplan/ui-primitives";
import { compose } from "@vastplan/ui-structure-composition-standard";
import { StandardShell } from "@vastplan/ui-structure-layout-standard";
import { TopNavigationShell } from "@vastplan/ui-structure-layout-top-navigation";

function Shell(props: UIShellProps) {
  switch (props.template.id) {
    case "standard":
      return <StandardShell {...props} />;
    case "top-navigation":
      return <TopNavigationShell {...props} />;
    default:
      throw new Error(`Shell 模板未实现: ${props.template.id}`);
  }
}

const adapter: UIShellAdapter = {
  id: "ui.structure.shell",
  uiContract: "4.0.0",
  templates: [
    { id: "standard", label: { namespace: "cn.vastplan.foundation.frontend.structure.shell", key: "template.standard", fallback: "标准侧栏" } },
    { id: "top-navigation", label: { namespace: "cn.vastplan.foundation.frontend.structure.shell", key: "template.topNavigation", fallback: "顶部导航" } },
  ],
  defaultTemplate: "standard",
  compose,
  Shell,
  localization: {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": { "template.standard": "标准侧栏", "template.topNavigation": "顶部导航" },
      "en-US": { "template.standard": "Standard sidebar", "template.topNavigation": "Top navigation" },
    },
  },
};

export const localization = adapter.localization;
export default adapter;
