import { createBrowserPlatformAdminClient, type PlatformAdminClient, type Setting } from "@vastplan/platform-admin";
import { defineCollectionPage, jsonSchemaDialect, managementServicesFor, message, type CollectionPageDefinition, type CollectionQuery, type FormSchema, type WorkbenchFormDefinition, type WorkbenchFormFieldErrors, type WorkbenchFormSubmitResult, type WorkbenchFrontendPluginContext } from "@vastplan/workbench-sdk";

const namespace = "cn.vastplan.platform.configuration.global-settings";

const schema: FormSchema = {
  id: "platform-setting.v1",
  schema: {
    $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["key", "value"],
    properties: {
      key: { type: "string", title: "设置键", minLength: 1, maxLength: 320 },
      value: { type: "string", title: "JSON 值", minLength: 1 },
    },
  },
  localization: { "/properties/key/title": message(namespace, "form.key", "设置键"), "/properties/value/title": message(namespace, "form.value", "JSON 值") },
};

type SettingRow = Setting & Record<string, unknown>;

export function createGlobalSettingsPage(client: PlatformAdminClient, serviceID: string, path: string, title: ReturnType<typeof message>): CollectionPageDefinition<SettingRow> {
  const form = (id: "create" | "edit"): WorkbenchFormDefinition<SettingRow> => ({
    id,
    schema,
    context: { editing: id === "edit" },
    presentation: {
      layout: "vertical",
      navigation: "sections",
      sections: [{ id: "setting", title: message(namespace, id === "edit" ? "form.sectionEdit" : "form.sectionCreate", id === "edit" ? "编辑设置" : "新增设置"), columns: 2, fields: ["/key", "/value"] }],
      fields: [
        { pointer: "/key", span: 2, readOnlyWhen: { pointer: "/context/editing", equals: true } },
        { pointer: "/value", span: 2, widget: "textarea", help: message(namespace, "form.valueHelp", "保存前会校验为有效 JSON；禁止保存密码和令牌。") },
      ],
    },
    workflow: {
      surface: "drawer",
      title: message(namespace, id === "edit" ? "form.editTitle" : "form.createTitle", id === "edit" ? "编辑全局设置" : "新增全局设置"),
      description: message(namespace, "form.description", "这里只保存非敏感平台配置；密码、令牌和密钥必须交给凭证插件。"),
      size: "md",
      submitLabel: message(namespace, "action.save", "保存"),
      success: { notify: message(namespace, "notice.saved", "设置已保存"), refreshCollection: true, close: true },
    },
    initialValue: { value: "{}" },
    async load(selected) {
      const setting = selected[0];
      return setting === undefined ? { value: "{}" } : { key: setting.key, value: JSON.stringify(setting.value, null, 2) };
    },
    async validate({ value }): Promise<WorkbenchFormFieldErrors> {
      if (typeof value.value !== "string") return { value: message(namespace, "error.valueText", "设置值必须是 JSON 文本") };
      try { JSON.parse(value.value); return {}; } catch { return { value: message(namespace, "error.valueInvalid", "设置值不是有效 JSON") }; }
    },
    async submit({ value, selected }): Promise<WorkbenchFormSubmitResult | void> {
      if (typeof value.key !== "string" || value.key === "" || typeof value.value !== "string") return { fieldErrors: { key: message(namespace, "error.keyRequired", "设置键不能为空"), value: message(namespace, "error.valueRequired", "设置值不能为空") } };
      let parsed: unknown;
      try { parsed = JSON.parse(value.value); } catch { return { fieldErrors: { value: message(namespace, "error.valueInvalid", "设置值不是有效 JSON") } }; }
      await client.putSetting(value.key, parsed, selected[0]?.version);
    },
  });
  return defineCollectionPage<SettingRow>({
    id: `platform.global-settings.${serviceID}`,
    path,
    title,
    description: message(namespace, "page.description", "管理平台级非敏感配置"),
    navigation: { id: `platform.global-settings.${serviceID}`, label: title, zone: "settings", order: 20 },
    collection: {
      id: `platform.global-settings.${serviceID}`,
      title,
      view: "table",
      query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20, 50, 100] },
      filters: [{ id: "key", label: message(namespace, "filter.key", "设置键前缀"), kind: "text" }],
      columns: [
        { key: "key", label: message(namespace, "column.key", "键"), defaultVisible: true, minWidth: 240 },
        { key: "version", label: message(namespace, "column.version", "版本"), format: "number", defaultVisible: true, minWidth: 90 },
        { key: "updatedAt", label: message(namespace, "column.updatedAt", "更新时间"), format: "datetime", defaultVisible: true, minWidth: 180 },
      ],
      actions: [
        { id: "create", label: message(namespace, "action.create", "新增设置"), icon: "add", placement: "page.primary", tone: "primary", form: "create" },
        { id: "edit", label: message(namespace, "action.edit", "编辑"), placement: "record.row", form: "edit" },
        { id: "delete", label: message(namespace, "action.delete", "删除"), placement: "record.row", tone: "danger", confirm: message(namespace, "confirm.delete", "确认删除此设置？版本不匹配时系统会拒绝。") },
      ],
    },
    forms: [form("create"), form("edit")],
    async load(query: CollectionQuery, signal) {
      const prefix = typeof query.filters.key === "string" ? query.filters.key.trim() : "";
      const items = await client.listSettings(prefix);
      if (signal.aborted) return { items: [], total: 0 };
      const start = Math.max(0, (query.page - 1) * query.pageSize);
      return { items: items.slice(start, start + query.pageSize) as SettingRow[], total: items.length };
    },
    async runAction({ action, selected }) {
      if (action.id !== "delete" || selected[0] === undefined) return;
      await client.deleteSetting(selected[0].key, selected[0].version);
    },
  });
}

export default {
  register(context: WorkbenchFrontendPluginContext) {
    const services = managementServicesFor(context.portal, "platform.settings");
    if (services.length === 0) throw new Error("Portal 未绑定 platform.settings 服务");
    for (const service of services) {
      const client = createBrowserPlatformAdminClient(context.portal.id, service.id);
      const suffix = services.length === 1 ? "" : `/${service.id}`;
      const title = context.i18n.message(services.length === 1 ? "page.title" : "page.titleService", services.length === 1 ? "全局设置" : "全局设置 · {service}", { service: service.label ?? service.id });
      context.addCollectionPage(createGlobalSettingsPage(client, service.id, `/settings/global${suffix}`, title));
    }
  },
  localization: { defaultLocale: "zh-CN", messages: {
    "zh-CN": { "form.key":"设置键","form.value":"JSON 值","form.valueHelp":"保存前会校验为有效 JSON；禁止保存密码和令牌。","form.sectionCreate":"新增设置","form.sectionEdit":"编辑设置","form.createTitle":"新增全局设置","form.editTitle":"编辑全局设置","form.description":"这里只保存非敏感平台配置；密码、令牌和密钥必须交给凭证插件。","notice.saved":"设置已保存","filter.key":"设置键前缀","action.create":"新增设置","action.edit":"编辑","action.save":"保存","action.delete":"删除","confirm.delete":"确认删除此设置？版本不匹配时系统会拒绝。","column.key":"键","column.version":"版本","column.updatedAt":"更新时间","page.title":"全局设置","page.titleService":"全局设置 · {service}","page.description":"管理平台级非敏感配置","error.valueText":"设置值必须是 JSON 文本","error.valueInvalid":"设置值不是有效 JSON","error.keyRequired":"设置键不能为空","error.valueRequired":"设置值不能为空" },
    "en-US": { "form.key":"Setting key","form.value":"JSON value","form.valueHelp":"The value must be valid JSON. Passwords and tokens are not allowed.","form.sectionCreate":"Create setting","form.sectionEdit":"Edit setting","form.createTitle":"Create global setting","form.editTitle":"Edit global setting","form.description":"Only non-sensitive platform configuration belongs here. Passwords, tokens, and keys must use the credential plugin.","notice.saved":"Setting saved","filter.key":"Setting key prefix","action.create":"Create setting","action.edit":"Edit","action.save":"Save","action.delete":"Delete","confirm.delete":"Delete this setting? The request will be rejected if the version does not match.","column.key":"Key","column.version":"Version","column.updatedAt":"Updated","page.title":"Global settings","page.titleService":"Global settings · {service}","page.description":"Manage non-sensitive platform configuration","error.valueText":"The setting value must be JSON text","error.valueInvalid":"The setting value is not valid JSON","error.keyRequired":"The setting key is required","error.valueRequired":"The setting value is required" }
  } },
};
