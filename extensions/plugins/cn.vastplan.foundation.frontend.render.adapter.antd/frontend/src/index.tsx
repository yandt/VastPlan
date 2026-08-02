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
        "form.invalid": "输入内容不符合要求", "form.asyncUnavailable": "异步校验暂时不可用", "form.credentialPlaceholder": "输入 credential:// 凭证引用（禁止填写明文）",
        "form.validation.required": "此项为必填项", "form.validation.minLength": "请至少输入 {limit} 个字符", "form.validation.maxLength": "最多可输入 {limit} 个字符",
        "form.validation.minimum": "数值不能小于 {limit}", "form.validation.maximum": "数值不能大于 {limit}", "form.validation.exclusiveMinimum": "数值必须大于 {limit}", "form.validation.exclusiveMaximum": "数值必须小于 {limit}",
        "form.validation.minItems": "请至少添加 {limit} 项", "form.validation.maxItems": "最多可添加 {limit} 项", "form.validation.minProperties": "请至少填写 {limit} 项", "form.validation.maxProperties": "最多可填写 {limit} 项",
        "form.validation.pattern": "输入格式不正确", "form.validation.format": "输入格式不正确", "form.validation.enum": "请选择有效选项", "form.validation.type": "输入值类型不正确", "form.validation.uniqueItems": "列表中不能包含重复项", "form.validation.additionalProperties": "包含不允许的字段",
      },
      "en-US": {
        "command.title": "Commands", "command.search": "Search commands", "command.empty": "No matching commands", "action.retry": "Retry",
        "theme.light": "Light", "theme.dark": "Dark", "iconTheme.canonical": "VastPlan Icons", "iconTheme.native": "Native Ant Design Icons",
        "form.invalid": "The entered value is invalid", "form.asyncUnavailable": "Asynchronous validation is temporarily unavailable", "form.credentialPlaceholder": "Enter a credential:// reference (plaintext is forbidden)",
        "form.validation.required": "This field is required", "form.validation.minLength": "Enter at least {limit} characters", "form.validation.maxLength": "Enter no more than {limit} characters",
        "form.validation.minimum": "Enter a value of at least {limit}", "form.validation.maximum": "Enter a value no greater than {limit}", "form.validation.exclusiveMinimum": "Enter a value greater than {limit}", "form.validation.exclusiveMaximum": "Enter a value less than {limit}",
        "form.validation.minItems": "Add at least {limit} items", "form.validation.maxItems": "Add no more than {limit} items", "form.validation.minProperties": "Complete at least {limit} fields", "form.validation.maxProperties": "Complete no more than {limit} fields",
        "form.validation.pattern": "Enter a value in the expected format", "form.validation.format": "Enter a value in the expected format", "form.validation.enum": "Select a valid option", "form.validation.type": "The value has an invalid type", "form.validation.uniqueItems": "Duplicate items are not allowed", "form.validation.additionalProperties": "The form contains an unsupported field",
      },
    },
  },
};

/** Explicit module role consumed only through the unified Adapter catalog. */
export const renderer = antdRenderer;
export default antdRenderer;
