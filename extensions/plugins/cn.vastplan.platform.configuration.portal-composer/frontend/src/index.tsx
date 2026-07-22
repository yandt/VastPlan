import {
  PortalControlClient,
  type PortalApplicationComposition,
  type PortalAuditEvent,
  type PortalFetch,
  type PortalRevision,
} from "@vastplan/ui-primitives";
import {
  defineCollectionPage,
  jsonSchemaDialect,
  message,
  type CollectionPageDefinition,
  type CollectionQuery,
  type FormSchema,
  type JSONValue,
  type WorkbenchFormDefinition,
  type WorkbenchFrontendPluginContext,
} from "@vastplan/workbench-sdk";
import { createActivationPage, createBindingPage, createProfilePage, governanceMessages } from "./governance-workspaces";

const namespace = "cn.vastplan.platform.configuration.portal-composer";
type EditorValue = Record<string, unknown>;
type ApplicationRow = PortalRevision & Record<string, unknown>;
export type ApplicationComposition = PortalApplicationComposition;

export const portalCompositionSchema: FormSchema = {
  id: "portal-composition.v1",
  schema: {
    $schema: jsonSchemaDialect, title: "Portal Application Composition", type: "object", additionalProperties: false, required: ["name", "route", "plugins"],
    properties: {
      name: { type: "string", title: "名称", minLength: 1 }, route: { type: "string", title: "访问路径", pattern: "^/" }, domains: { type: "array", title: "绑定域名", uniqueItems: true, items: { type: "string", minLength: 1 } }, audience: { type: "array", title: "目标受众", uniqueItems: true, items: { type: "string", minLength: 1 } }, branding: { type: "object", title: "品牌配置", additionalProperties: true, default: {} },
      plugins: { type: "array", title: "应用功能插件", minItems: 1, items: { type: "object", additionalProperties: false, required: ["id", "version"], properties: { id: { type: "string", title: "插件 ID", pattern: "^[a-z0-9]+(?:[.-][a-z0-9]+)+$" }, version: { type: "string", title: "精确版本", pattern: "^\\d+\\.\\d+\\.\\d+(?:[-+][0-9A-Za-z.-]+)?$" }, channel: { type: "string", title: "发布通道", default: "stable", oneOf: [{ const: "stable", title: "稳定版" }, { const: "preview", title: "预发布" }] } } } },
      config: { type: "object", title: "非敏感插件配置", additionalProperties: true, default: {} },
    },
  },
  uiSchema: { route: { "ui:help": "必须以 / 开始；同一租户内不能与其他已发布 Portal 冲突" }, domains: { "ui:help": "留空表示不限制域名" }, audience: { "ui:help": "只声明可见受众，不替代服务端授权" }, branding: { "ui:help": "只能保存非敏感 JSON 品牌配置" }, plugins: { "ui:help": "这里只能选择应用插件；设计系统和平台插件由 Platform Profile 管理", items: { channel: { "ui:widget": "select" } } }, config: { "ui:help": "禁止写入密码、令牌或凭证明文" } },
  localization: { "/properties/name/title": message(namespace, "form.name", "名称"), "/properties/route/title": message(namespace, "form.route", "访问路径"), "/properties/domains/title": message(namespace, "form.domains", "绑定域名"), "/properties/audience/title": message(namespace, "form.audience", "目标受众"), "/properties/branding/title": message(namespace, "form.branding", "品牌配置"), "/properties/plugins/title": message(namespace, "form.plugins", "应用功能插件"), "/properties/plugins/items/properties/id/title": message(namespace, "form.pluginId", "插件 ID"), "/properties/plugins/items/properties/version/title": message(namespace, "form.version", "精确版本"), "/properties/plugins/items/properties/channel/title": message(namespace, "form.channel", "发布通道"), "/properties/plugins/items/properties/channel/oneOf/0/title": message(namespace, "form.stable", "稳定版"), "/properties/plugins/items/properties/channel/oneOf/1/title": message(namespace, "form.preview", "预发布"), "/properties/config/title": message(namespace, "form.config", "非敏感插件配置") },
  uiLocalization: { "/route/ui:help": message(namespace, "help.route", "必须以 / 开始；同一租户内不能与其他已发布 Portal 冲突"), "/domains/ui:help": message(namespace, "help.domains", "留空表示不限制域名"), "/audience/ui:help": message(namespace, "help.audience", "只声明可见受众，不替代服务端授权"), "/branding/ui:help": message(namespace, "help.branding", "只能保存非敏感 JSON 品牌配置"), "/plugins/ui:help": message(namespace, "help.plugins", "这里只能选择应用插件；设计系统和平台插件由 Platform Profile 管理"), "/config/ui:help": message(namespace, "help.config", "禁止写入密码、令牌或凭证明文") },
};

export function buildApplicationComposition(value: EditorValue, revision = 1): ApplicationComposition {
  return { version: 1, revision, id: typeof value.name === "string" && value.name !== "" ? value.name : "portal", target: { kernel: "frontend" }, route: typeof value.route === "string" ? value.route : "/", ...optionalStrings("domains", value.domains), ...optionalStrings("audience", value.audience), ...optionalRecord("branding", value.branding), plugins: normalizePluginRefs(value.plugins), config: jsonRecord(value.config) };
}
export function compositionToEditorValue(composition: ApplicationComposition): EditorValue { return { name: composition.id, route: composition.route, domains: composition.domains ?? [], audience: composition.audience ?? [], branding: composition.branding ?? {}, plugins: composition.plugins.map((ref) => ({ ...ref })), config: composition.config }; }

export function createApplicationPage(client: PortalControlClient): CollectionPageDefinition<ApplicationRow> {
  const form = (id: "create" | "edit"): WorkbenchFormDefinition<ApplicationRow> => ({
    id, schema: portalCompositionSchema,
    presentation: { layout: "vertical", navigation: "sections", sections: [{ id: "identity", title: message(namespace, "section.identity", "Portal 标识"), columns: 2, fields: ["/name", "/route", "/domains", "/audience"] }, { id: "composition", title: message(namespace, "section.composition", "功能组合"), columns: 1, fields: ["/plugins", "/branding", "/config"] }], fields: [{ pointer: "/plugins" }, { pointer: "/branding" }, { pointer: "/config" }] },
    workflow: { surface: "drawer", size: "lg", title: message(namespace, id === "create" ? "action.new" : "action.edit", id === "create" ? "新建 Portal 草稿" : "编辑 Portal 草稿"), submitLabel: message(namespace, id === "create" ? "action.create" : "action.save", id === "create" ? "创建草稿" : "保存草稿"), success: { notify: message(namespace, id === "create" ? "notice.created" : "notice.saved", id === "create" ? "草稿已创建" : "草稿已保存"), refreshCollection: true, close: true } },
    ...(id === "create" ? { initialValue: { route: "/", domains: [], audience: [], branding: {}, plugins: [], config: {} } } : { async load(selected: readonly ApplicationRow[]) { return selected[0] === undefined ? {} : compositionToEditorValue(selected[0].composition); } }),
    async submit({ value, selected }) { if (id === "create") await client.create(buildApplicationComposition(value)); else if (selected[0] !== undefined) await client.update(selected[0].id, buildApplicationComposition(value, selected[0].composition.revision)); },
  });
  const statusLabels = { Draft: message(namespace, "status.draft", "草稿"), PendingApproval: message(namespace, "status.pendingApproval", "待审批"), Approved: message(namespace, "status.approved", "已批准"), Published: message(namespace, "status.published", "已发布") };
  return defineCollectionPage<ApplicationRow>({
    id: "platform.portal-composer", path: "/settings/portals", title: message(namespace, "page.title.v2", "Portal 管理中心"), description: message(namespace, "page.description.v2", "治理 Portal 应用输入、审批与发布"), navigation: { id: "platform.portal-composer", label: message(namespace, "page.navigation.v2", "Portal 管理"), zone: "settings", order: 11 },
    collection: { id: "portal-applications", title: message(namespace, "panel.revisions", "Portal Application Revisions"), view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [10, 20, 50] }, filters: [{ id: "portal", label: message(namespace, "filter.portal", "Portal"), kind: "text" }, { id: "status", label: message(namespace, "filter.status", "状态"), kind: "select", options: Object.entries(statusLabels).map(([value, label]) => ({ value, label })) }], columns: [{ key: "id", label: "Revision", format: "number", defaultVisible: true }, { key: "portalId", label: "Portal", defaultVisible: true, minWidth: 180 }, { key: "status", label: message(namespace, "column.status", "状态"), format: "status", valueLabels: statusLabels, statusTones: { Draft: "neutral", PendingApproval: "warning", Approved: "info", Published: "success" }, defaultVisible: true }, { key: "updatedAt", label: message(namespace, "column.updated", "更新时间"), format: "datetime", defaultVisible: true, minWidth: 180 }], selection: "single", preferences: { allowedColumns: ["id", "portalId", "status", "updatedAt"], density: true }, actions: [
      { id: "application.create", label: message(namespace, "action.new", "新建 Portal 草稿"), icon: "add", placement: "page.primary", tone: "primary", form: "create" },
      { id: "application.edit", label: message(namespace, "action.edit", "编辑草稿"), icon: "edit", placement: "page.secondary", requiresSelection: true, form: "edit", visibleWhen: { pointer: "/status", equals: "Draft" } },
      { id: "application.submit", label: message(namespace, "action.submit", "提交审批"), icon: "upload", placement: "page.secondary", requiresSelection: true, confirm: message(namespace, "confirm.submit", "提交后不能继续编辑，需要由另一位审批人处理。"), visibleWhen: { pointer: "/status", equals: "Draft" } },
      { id: "application.approve", label: message(namespace, "action.approve", "批准"), icon: "success", placement: "page.secondary", tone: "primary", requiresSelection: true, visibleWhen: { pointer: "/status", equals: "PendingApproval" } },
      { id: "application.publish", label: message(namespace, "action.publishInput", "发布为可选输入"), icon: "publish", placement: "page.secondary", tone: "primary", requiresSelection: true, confirm: message(namespace, "confirm.inputPublish", "发布只会使该 Application 可被 Activation 引用，不会直接改变线上 Portal。"), visibleWhen: { pointer: "/status", equals: "Approved" } },
      { id: "application.diff", label: message(namespace, "action.diff", "查看差异"), icon: "search", placement: "page.secondary", requiresSelection: true, overlay: "diff" }, { id: "application.audit", label: message(namespace, "action.audit", "审计记录"), icon: "info", placement: "page.secondary", requiresSelection: true, overlay: "audit" },
    ] },
    forms: [form("create"), form("edit")], overlays: [
      { id: "diff", surface: "dialog", size: "lg", title: message(namespace, "dialog.diff", "Application 差异"), async load(selected) { const row = selected[0]; if (row === undefined) return { kind: "json", documents: [] }; const revisions = await client.list(); const baseline = revisions.find((item) => item.portalId === row.portalId && item.status === "Published" && item.id !== row.id); return { kind: "json", documents: [{ title: message(namespace, "diff.active", "已发布输入"), value: (baseline?.composition ?? {}) as unknown as JSONValue }, { title: message(namespace, "diff.selected", "所选版本"), value: row.composition as unknown as JSONValue }] }; } },
      { id: "audit", surface: "drawer", size: "lg", title: message(namespace, "dialog.audit", "Application 审计"), async load(selected) { const rows = selected[0] === undefined ? [] : await client.audit(selected[0].id); return { kind: "table", rowKey: "id", rows: rows as Array<PortalAuditEvent & Record<string, unknown>>, columns: [{ key: "at", label: message(namespace, "column.time", "时间"), format: "datetime" }, { key: "action", label: message(namespace, "column.action", "动作") }, { key: "actorId", label: message(namespace, "column.actor", "操作者") }, { key: "reason", label: message(namespace, "column.reason", "原因") }] }; } },
    ],
    async load(query: CollectionQuery, signal) { const portal = typeof query.filters.portal === "string" ? query.filters.portal.trim().toLowerCase() : ""; const status = typeof query.filters.status === "string" ? query.filters.status : ""; const rows = (await client.list()).filter((item) => (portal === "" || item.portalId.toLowerCase().includes(portal)) && (status === "" || item.status === status)) as ApplicationRow[]; if (signal.aborted) return { items: [], total: 0 }; const start = (query.page - 1) * query.pageSize; return { items: rows.slice(start, start + query.pageSize), total: rows.length }; },
    async runAction({ action, selected }) { const row = selected[0]; if (row === undefined) return; if (action.id === "application.submit") await client.submit(row.id); else if (action.id === "application.approve") await client.approve(row.id); else if (action.id === "application.publish") await client.publish(row.id); return { notify: { title: action.label, kind: "success" } }; },
  });
}

function createDefaultClient(): PortalControlClient { const fetcher: PortalFetch = (input, init) => globalThis.fetch(input, init as RequestInit); return new PortalControlClient({ fetch: fetcher }); }
function optionalStrings<K extends "domains" | "audience">(key: K, value: unknown): Partial<Pick<ApplicationComposition, K>> { const strings = Array.isArray(value) ? value.filter((item): item is string => typeof item === "string" && item !== "") : []; return strings.length === 0 ? {} : { [key]: strings } as Partial<Pick<ApplicationComposition, K>>; }
function optionalRecord<K extends "branding">(key: K, value: unknown): Partial<Pick<ApplicationComposition, K>> { const record = jsonRecord(value); return Object.keys(record).length === 0 ? {} : { [key]: record } as Partial<Pick<ApplicationComposition, K>>; }
function jsonRecord(value: unknown): Record<string, JSONValue> { if (typeof value !== "object" || value === null || Array.isArray(value)) return {}; return JSON.parse(JSON.stringify(value)) as Record<string, JSONValue>; }
function normalizePluginRefs(value: unknown): ApplicationComposition["plugins"] { if (!Array.isArray(value)) return []; return value.flatMap((candidate) => { if (typeof candidate !== "object" || candidate === null) return []; const { id, version, channel } = candidate as Record<string, unknown>; if (typeof id !== "string" || typeof version !== "string") return []; return [{ id, version, ...(typeof channel === "string" ? { channel } : {}) }]; }); }

const localization = {
  "zh-CN": { "form.name": "名称", "form.route": "访问路径", "form.domains": "绑定域名", "form.audience": "目标受众", "form.branding": "品牌配置", "form.plugins": "应用功能插件", "form.pluginId": "插件 ID", "form.version": "精确版本", "form.channel": "发布通道", "form.stable": "稳定版", "form.preview": "预发布", "form.config": "非敏感插件配置", "help.route": "必须以 / 开始；同一租户内不能与其他已发布 Portal 冲突", "help.domains": "留空表示不限制域名", "help.audience": "只声明可见受众，不替代服务端授权", "help.branding": "只能保存非敏感 JSON 品牌配置", "help.plugins": "这里只能选择应用插件；设计系统和平台插件由 Platform Profile 管理", "help.config": "禁止写入密码、令牌或凭证明文", "page.title.v2": "Portal 管理中心", "page.description.v2": "治理 Portal 应用输入、审批与发布", "page.navigation.v2": "Portal 管理", "filter.portal": "Portal", "filter.status": "状态", "action.new": "新建 Portal 草稿", "action.edit": "编辑草稿", "action.create": "创建草稿", "action.save": "保存草稿", "action.submit": "提交审批", "action.approve": "批准", "action.publishInput": "发布为可选输入", "action.diff": "查看差异", "action.audit": "审计记录", "notice.created": "草稿已创建", "notice.saved": "草稿已保存", "status.draft": "草稿", "status.pendingApproval": "待审批", "status.approved": "已批准", "status.published": "已发布", "section.identity": "Portal 标识", "section.composition": "功能组合" },
  "en-US": { "form.name": "Name", "form.route": "Route", "form.domains": "Domains", "form.audience": "Audience", "form.branding": "Branding", "form.plugins": "Application plugins", "form.pluginId": "Plugin ID", "form.version": "Exact version", "form.channel": "Channel", "form.stable": "Stable", "form.preview": "Preview", "form.config": "Non-sensitive plugin config", "help.route": "Must start with / and be unique in the tenant", "help.domains": "Leave empty to allow any domain", "help.audience": "Visibility only; server authorization still applies", "help.branding": "Only non-sensitive JSON is allowed", "help.plugins": "Application plugins only; Platform Profile manages foundation UI", "help.config": "Passwords and tokens are forbidden", "page.title.v2": "Portal Management", "page.description.v2": "Govern Portal application inputs, approval, and publishing", "page.navigation.v2": "Portal Management", "filter.portal": "Portal", "filter.status": "Status", "action.new": "New Portal draft", "action.edit": "Edit draft", "action.create": "Create draft", "action.save": "Save draft", "action.submit": "Submit", "action.approve": "Approve", "action.publishInput": "Publish as eligible input", "action.diff": "View diff", "action.audit": "Audit log", "notice.created": "Draft created", "notice.saved": "Draft saved", "status.draft": "Draft", "status.pendingApproval": "Pending approval", "status.approved": "Approved", "status.published": "Published", "section.identity": "Portal identity", "section.composition": "Feature composition" },
};

export default {
  register(context: WorkbenchFrontendPluginContext) { const client = createDefaultClient(); context.addCollectionPage(createProfilePage(client)); context.addCollectionPage(createApplicationPage(client)); context.addCollectionPage(createBindingPage(client)); context.addCollectionPage(createActivationPage(client)); },
  localization: { defaultLocale: "zh-CN", messages: { "zh-CN": { ...localization["zh-CN"], ...governanceMessages["zh-CN"] }, "en-US": { ...localization["en-US"], ...governanceMessages["en-US"] } } },
};
