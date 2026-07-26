import type { UIRenderer } from "@vastplan/ui-primitives";
import { AntdProvider, antdIconForTheme } from "./provider";
import { antdPortalUIComponents } from "./portal-ui";
import { antdIconThemes, antdThemeTemplates, namespace } from "./theme";

export { antdIconForTheme, antdPortalUIComponents };

export const antdRenderer: UIRenderer = {
  id: "antd",
  label: { namespace, key: "renderer.antd", fallback: "Ant Design" },
  framework: "antd",
  capabilities: ["layout", "menu", "overlay", "form", "data", "feedback", "theme", "navigation"],
  themeTemplates: antdThemeTemplates,
  defaultThemeTemplate: "light",
  iconThemes: antdIconThemes,
  defaultIconTheme: "canonical",
  Provider: AntdProvider,
  localization: {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": {
        "command.title": "命令", "command.search": "搜索命令", "command.empty": "没有匹配命令", "action.retry": "重试",
        "theme.light": "浅色", "theme.dark": "深色", "iconTheme.canonical": "VastPlan 图标", "iconTheme.native": "Ant Design 原生图标",
        "form.invalid": "值不符合 Schema", "form.asyncUnavailable": "异步校验暂时不可用", "form.credentialPlaceholder": "输入 credential:// 凭证引用（禁止填写明文）",
      },
      "en-US": {
        "command.title": "Commands", "command.search": "Search commands", "command.empty": "No matching commands", "action.retry": "Retry",
        "theme.light": "Light", "theme.dark": "Dark", "iconTheme.canonical": "VastPlan Icons", "iconTheme.native": "Native Ant Design Icons",
        "form.invalid": "Value does not match the schema", "form.asyncUnavailable": "Asynchronous validation is temporarily unavailable", "form.credentialPlaceholder": "Enter a credential:// reference (plaintext is forbidden)",
      },
    },
  },
};

/** Explicit module role consumed only through the unified Adapter catalog. */
export const renderer = antdRenderer;
export default antdRenderer;
