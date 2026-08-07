import type { PortalConfiguration, PortalPlatformProfile } from "@vastplan/ui-primitives";
import { jsonSchemaDialect, message, type FormSchema, type JSONValue } from "@vastplan/workbench-sdk";

const namespace = "cn.vastplan.platform.configuration.portal-composer";
const rendererChoices = [{ const: "antd", title: "Ant Design" }] as const;
type KnownRenderer = (typeof rendererChoices)[number]["const"];
export interface PortalPermissionChoice { readonly code: string; readonly title: string }

export const portalConfigurationSchema: FormSchema = {
  id: "portal-configuration.v2",
  schema: {
    $schema: jsonSchemaDialect,
    title: "Portal Configuration",
    type: "object",
    additionalProperties: false,
    required: ["portalId", "route", "defaultRenderer", "allowedRenderers", "defaultTemplate", "applicationPlugins", "services"],
    properties: {
      portalId: { type: "string", title: "Portal ID", pattern: "^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$" },
      route: { type: "string", title: "访问路径", pattern: "^/" },
      domains: { type: "array", title: "绑定域名", uniqueItems: true, items: { type: "string", minLength: 1 } },
      audience: {
        type: "array",
        title: "访问权限",
        description: "限制可进入此 Portal 的权限代码；用户拥有任意一项即可访问，留空则不额外限制。",
        uniqueItems: true,
        items: { type: "string", minLength: 1 },
      },
      defaultRenderer: { type: "string", title: "默认 UI 框架", oneOf: rendererChoices },
      allowedRenderers: { type: "array", title: "允许的 UI 框架", minItems: 1, uniqueItems: true, items: { type: "string", oneOf: rendererChoices } },
      userSelectableRenderer: { type: "boolean", title: "允许用户切换 UI 框架" },
      defaultTemplate: { type: "string", title: "默认布局", oneOf: [{ const: "standard", title: "标准侧栏布局" }, { const: "top-navigation", title: "顶部导航布局" }] },
      pageBodyWidth: { type: "string", title: "页面正文宽度", oneOf: [{ const: "fluid", title: "自适应" }, { const: "contained", title: "最大 1280px" }] },
      navigationOverrides: {
        type: "array", title: "导航显示覆盖", items: {
          type: "object", additionalProperties: false, required: ["target"],
          properties: {
            target: { type: "string", title: "插件菜单节点", pattern: "^[A-Za-z0-9][A-Za-z0-9._-]{0,159}/[A-Za-z0-9][A-Za-z0-9._-]{0,159}$" },
            hidden: { type: "boolean", title: "隐藏" },
            order: { type: "integer", title: "排序" },
            parent: { type: "string", title: "新父节点", pattern: "^[A-Za-z0-9][A-Za-z0-9._-]{0,159}/[A-Za-z0-9][A-Za-z0-9._-]{0,159}$" },
            labels: { type: "object", title: "多语言名称覆盖", additionalProperties: { type: "string", minLength: 1, maxLength: 80 } },
          },
        },
      },
      applicationPlugins: { type: "array", title: "应用功能插件", items: pluginRefSchema() },
      branding: { type: "object", title: "品牌配置", additionalProperties: true, default: {} },
      config: { type: "object", title: "非敏感插件配置", additionalProperties: true, default: {} },
      services: { type: "array", title: "管理服务绑定", minItems: 1, items: { type: "object", additionalProperties: true } },
    },
  },
  uiSchema: {
    domains: { items: { "ui:title": "" } },
    audience: { items: { "ui:title": "" } },
    defaultRenderer: { "ui:widget": "select" },
    defaultTemplate: { "ui:widget": "select" },
    pageBodyWidth: { "ui:widget": "select" },
    navigationOverrides: { "ui:help": "菜单由已安装插件自治声明；Portal 只能隐藏、排序、调整父级或覆盖受支持语言的名称。" },
    applicationPlugins: { "ui:help": "这里只选择应用插件；平台运行栈随同 Portal 工作副本和 Publication 保存。" },
    config: { "ui:help": "禁止写入密码、令牌或凭证明文。" },
  },
  localization: {
    "/properties/portalId/title": message(namespace, "form.portalId", "Portal ID"),
    "/properties/route/title": message(namespace, "form.route", "访问路径"),
    "/properties/audience/title": message(namespace, "form.audience", "访问权限"),
    "/properties/audience/description": message(namespace, "form.audience.description", "限制可进入此 Portal 的权限代码；用户拥有任意一项即可访问，留空则不额外限制。"),
    "/properties/defaultRenderer/title": message(namespace, "form.renderer", "默认 UI 框架"),
    "/properties/defaultTemplate/title": message(namespace, "form.layout", "默认布局"),
    "/properties/navigationOverrides/title": message(namespace, "form.navigationOverrides", "导航显示覆盖"),
    "/properties/applicationPlugins/title": message(namespace, "form.plugins", "应用功能插件"),
    "/properties/services/title": message(namespace, "form.services", "管理服务绑定"),
  },
};

export function portalConfigurationSchemaWithPermissions(permissions: readonly PortalPermissionChoice[]): FormSchema {
  const root = portalConfigurationSchema.schema;
  const properties = objectValue(root.properties);
  const audience = objectValue(properties.audience);
  const items = objectValue(audience.items);
  const choices = [...new Map(permissions
    .filter((permission) => permission.code.trim() !== "")
    .map((permission) => [permission.code, permission] as const)).values()]
    .sort((left, right) => left.code.localeCompare(right.code))
    .map((permission) => ({ const: permission.code, title: permission.title.trim() === "" ? permission.code : `${permission.title} (${permission.code})` }));
  return {
    ...portalConfigurationSchema,
    schema: { ...root, properties: { ...properties, audience: { ...audience, items: { ...items, oneOf: choices } } } },
    uiSchema: {
      ...portalConfigurationSchema.uiSchema,
      audience: { items: { "ui:title": "", "ui:widget": "select" } },
    },
  };
}

function objectValue(value: JSONValue | undefined): Readonly<Record<string, JSONValue>> {
  return isJSONObject(value) ? value : {};
}

function isJSONObject(value: JSONValue | undefined): value is Readonly<Record<string, JSONValue>> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function configurationToForm(portalId: string, configuration: PortalConfiguration): Record<string, unknown> {
  const template = configuration.platform.shell.config.defaultTemplate;
  const options = configuration.platform.shell.config.templateOptions?.[template];
  return {
    portalId,
    route: configuration.application.route,
    domains: configuration.application.domains ?? [],
    audience: configuration.application.audience ?? [],
    defaultRenderer: knownRenderer(configuration.platform.renderAdapter.config.defaultRenderer),
    allowedRenderers: configuration.platform.renderAdapter.config.allowedRenderers.filter(isKnownRenderer),
    userSelectableRenderer: configuration.platform.renderAdapter.config.userSelectable,
    defaultTemplate: template === "top-navigation" ? "top-navigation" : "standard",
    pageBodyWidth: options?.pageBodyWidth === "contained" ? "contained" : "fluid",
    navigationOverrides: Array.isArray(configuration.platform.shell.config.navigationOverrides) ? configuration.platform.shell.config.navigationOverrides : [],
    applicationPlugins: configuration.application.plugins.map((plugin) => ({ ...plugin })),
    branding: configuration.application.branding ?? {},
    config: configuration.application.config ?? {},
    services: configuration.services,
  };
}

export function buildPortalConfiguration(base: PortalConfiguration, value: Readonly<Record<string, unknown>>, platformDefault: PortalPlatformProfile = base.platform): PortalConfiguration {
  const defaultRenderer = knownRenderer(value.defaultRenderer);
  const allowedRenderers = Array.isArray(value.allowedRenderers) ? value.allowedRenderers.filter(isKnownRenderer) : [defaultRenderer];
  if (!allowedRenderers.includes(defaultRenderer)) allowedRenderers.unshift(defaultRenderer);
  const defaultTemplate = value.defaultTemplate === "top-navigation" ? "top-navigation" : "standard";
  const templateOptions = {
    ...(base.platform.shell.config.templateOptions ?? {}),
    [defaultTemplate]: { ...(base.platform.shell.config.templateOptions?.[defaultTemplate] ?? {}), pageBodyWidth: value.pageBodyWidth === "contained" ? "contained" : "fluid" },
  };
  const accountCenter = base.platform.accountCenter ?? platformDefault.accountCenter;
  return {
    platform: {
      ...base.platform,
      accountCenter: { ...accountCenter },
      plugins: withAccountCenter(base.platform.plugins, accountCenter),
      renderAdapter: { ...base.platform.renderAdapter, config: { ...base.platform.renderAdapter.config, defaultRenderer, allowedRenderers, userSelectable: value.userSelectableRenderer === true } },
      shell: { ...base.platform.shell, config: { ...base.platform.shell.config, defaultTemplate, navigationOverrides: jsonArray(value.navigationOverrides), templateOptions } },
    },
    application: {
      ...base.application,
      route: typeof value.route === "string" ? value.route : "/",
      ...optionalStrings("domains", value.domains),
      ...optionalStrings("audience", value.audience),
      branding: jsonRecord(value.branding),
      plugins: pluginRefs(value.applicationPlugins),
      config: jsonRecord(value.config),
    },
    services: Array.isArray(value.services) ? JSON.parse(JSON.stringify(value.services)) as PortalConfiguration["services"] : [],
  };
}

export function profileSummary(profile: PortalPlatformProfile): { renderer: string; layout: string } {
  return { renderer: profile.renderAdapter.config.defaultRenderer, layout: profile.shell.config.defaultTemplate };
}

function pluginRefSchema(): Record<string, JSONValue> {
  return { type: "object", additionalProperties: false, required: ["id", "version"], properties: { id: { type: "string" }, version: { type: "string" }, channel: { type: "string", default: "stable" } } };
}
function isKnownRenderer(value: unknown): value is KnownRenderer { return value === "antd"; }
function knownRenderer(value: unknown): KnownRenderer { return isKnownRenderer(value) ? value : "antd"; }
function jsonArray(value: unknown): JSONValue[] { return Array.isArray(value) ? JSON.parse(JSON.stringify(value)) as JSONValue[] : []; }
function jsonRecord(value: unknown): Record<string, JSONValue> { return typeof value === "object" && value !== null && !Array.isArray(value) ? JSON.parse(JSON.stringify(value)) as Record<string, JSONValue> : {}; }
function pluginRefs(value: unknown): PortalConfiguration["application"]["plugins"] { return Array.isArray(value) ? value.flatMap((candidate) => { if (typeof candidate !== "object" || candidate === null) return []; const item = candidate as Record<string, unknown>; return typeof item.id === "string" && typeof item.version === "string" ? [{ id: item.id, version: item.version, ...(typeof item.channel === "string" ? { channel: item.channel } : {}) }] : []; }) : []; }
function withAccountCenter(plugins: PortalPlatformProfile["plugins"], accountCenter: PortalPlatformProfile["accountCenter"]): PortalPlatformProfile["plugins"] {
  const withoutSelected = plugins.filter((plugin) => plugin.id !== accountCenter.id);
  return [...withoutSelected, { ...accountCenter }];
}
function optionalStrings<K extends "domains" | "audience">(key: K, value: unknown): Partial<Pick<PortalConfiguration["application"], K>> { const values = Array.isArray(value) ? value.filter((item): item is string => typeof item === "string" && item !== "") : []; return values.length === 0 ? { [key]: undefined } as Partial<Pick<PortalConfiguration["application"], K>> : { [key]: values } as Partial<Pick<PortalConfiguration["application"], K>>; }
