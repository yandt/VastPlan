import {
  defineMasterDetailPage,
  defineRecordDetailPage,
  defineTreeDetailPage,
  jsonSchemaDialect,
  message,
  type CollectionQuery,
  type FormSchema,
  type MasterDetailPageDefinition,
  type JSONValue,
  type RecordDetailPageDefinition,
  type TreeDetailPageDefinition,
  type WorkbenchFormDefinition,
  type WorkbenchFrontendPluginContext,
} from "@vastplan/workbench-sdk";

const namespace = "cn.vastplan.product.developer.workbench-gallery";

type ServiceRecord = Record<string, unknown> & {
  id: string; name: string; kind: string; status: string; tone: string;
  owner: string; description: string; updatedAt: string; active: boolean;
};

let services: ServiceRecord[] = [
  { id: "svc-auth", name: "身份服务", kind: "Foundation", status: "运行中", tone: "success", owner: "安全平台组", description: "负责企业身份接入与会话签发。", updatedAt: "2026-07-23T08:30:00Z", active: true },
  { id: "svc-artifact", name: "制品仓库", kind: "Platform", status: "运行中", tone: "success", owner: "平台工程组", description: "保存签名插件制品和发布流水账。", updatedAt: "2026-07-23T07:20:00Z", active: true },
  { id: "svc-runner", name: "Runner 网关", kind: "Product", status: "规划中", tone: "warning", owner: "客户端组", description: "向桌面 Runner 签发任务与执行租约。", updatedAt: "2026-07-22T15:10:00Z", active: false },
  { id: "svc-mobile", name: "Mobile Gateway", kind: "Product", status: "待实施", tone: "neutral", owner: "移动端组", description: "移动 Companion 的身份与交互入口。", updatedAt: "2026-07-22T11:00:00Z", active: false },
];

const detail = {
  titleKey: "name", subtitleKey: "description", status: { labelKey: "status", toneKey: "tone" },
  sections: [
    { id: "identity", title: message(namespace, "section.identity", "基本信息"), columns: 2, fields: [
      { key: "id", label: message(namespace, "field.id", "服务 ID") },
      { key: "kind", label: message(namespace, "field.kind", "服务类型") },
      { key: "owner", label: message(namespace, "field.owner", "责任团队") },
      { key: "updatedAt", label: message(namespace, "field.updated", "更新时间"), format: "datetime" as const },
    ] },
    { id: "state", title: message(namespace, "section.state", "运行状态"), columns: 2, fields: [
      { key: "status", label: message(namespace, "field.status", "状态"), format: "status" as const, statusTones: { "运行中": "success", "规划中": "warning", "待实施": "neutral" } },
      { key: "active", label: message(namespace, "field.active", "已启用"), format: "boolean" as const },
    ] },
  ],
  emptyTitle: message(namespace, "empty.record", "请选择一个服务"),
} as const;

const editorSchema: FormSchema = {
  id: "example.service-editor.v1",
  schema: { $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["name", "owner", "description", "active"], properties: {
    name: { type: "string", title: "服务名称", minLength: 1, maxLength: 80 },
    owner: { type: "string", title: "责任团队", minLength: 1, maxLength: 80 },
    description: { type: "string", title: "说明", maxLength: 500 },
    active: { type: "boolean", title: "启用" },
  } },
  localization: {
    "/properties/name/title": message(namespace, "field.name", "服务名称"),
    "/properties/owner/title": message(namespace, "field.owner", "责任团队"),
    "/properties/description/title": message(namespace, "field.description", "说明"),
    "/properties/active/title": message(namespace, "field.active", "已启用"),
  },
};

const editor: WorkbenchFormDefinition<ServiceRecord> = {
  id: "service-editor", schema: editorSchema,
  presentation: { layout: "vertical", navigation: "sections", sections: [{ id: "service", columns: 2, fields: ["/name", "/owner", "/description", "/active"] }], fields: [{ pointer: "/description", span: 2, widget: "textarea" }] },
  workflow: { surface: "page", title: message(namespace, "editor.title", "编辑服务"), submitLabel: message(namespace, "action.save", "保存"), success: { notify: message(namespace, "notice.saved", "示例记录已保存"), refreshCollection: true } },
  async load(selected) { const row = selected[0]; return row === undefined ? {} : { name: row.name, owner: row.owner, description: row.description, active: row.active }; },
  async submit({ value, selected }) {
    const row = selected[0];
    if (row === undefined) return;
    services = services.map((item) => item.id === row.id ? { ...item, name: String(value.name ?? item.name), owner: String(value.owner ?? item.owner), description: String(value.description ?? item.description), active: value.active === true, updatedAt: new Date().toISOString() } : item);
  },
};

export function recordDetailPage(): RecordDetailPageDefinition<ServiceRecord> {
  return defineRecordDetailPage({
    id: "example.record-detail", path: "/examples/workbench/record-detail", pattern: "record-detail",
    title: message(namespace, "page.detail", "记录详情"), navigation: { id: "example.record-detail", label: message(namespace, "page.detail", "记录详情"), zone: "secondary", order: 10 },
    detail, async load() { return { ...services[0] }; },
  });
}

export function masterDetailPage(): MasterDetailPageDefinition<ServiceRecord> {
  return defineMasterDetailPage({
    id: "example.master-detail", path: "/examples/workbench/master-detail", pattern: "master-detail",
    title: message(namespace, "page.master", "列表与编辑"), navigation: { id: "example.master-detail", label: message(namespace, "page.master", "列表与编辑"), zone: "secondary", order: 20 },
    master: { id: "service", title: message(namespace, "master.services", "服务列表"), keyField: "id", titleField: "name", subtitleField: "owner", status: { labelField: "status", toneField: "tone" }, query: { mode: "page", defaultPageSize: 10, pageSizeOptions: [10, 20] }, filters: [
      { id: "name", label: message(namespace, "filter.name", "服务名称"), kind: "text" },
      { id: "status", label: message(namespace, "filter.status", "状态"), kind: "select", options: ["运行中", "规划中", "待实施"].map((value) => ({ value, label: value })) },
    ] },
    detail, editor,
    actions: [{ id: "refresh-record", label: message(namespace, "action.refresh", "刷新记录"), icon: "refresh", placement: "record.detail" }],
    async loadMaster(query: CollectionQuery) {
      const name = typeof query.filters.name === "string" ? query.filters.name.trim() : "";
      const status = typeof query.filters.status === "string" ? query.filters.status : "";
      const filtered = services.filter((item) => (name === "" || item.name.includes(name)) && (status === "" || item.status === status));
      const start = (query.page - 1) * query.pageSize;
      return { items: filtered.slice(start, start + query.pageSize), total: filtered.length };
    },
    async loadRecord(key) { const item = services.find((candidate) => candidate.id === key); return item === undefined ? undefined : { ...item }; },
  });
}

export function treeDetailPage(): TreeDetailPageDefinition<ServiceRecord> {
  return defineTreeDetailPage({
    id: "example.tree-detail", path: "/examples/workbench/tree-detail", pattern: "tree-detail",
    title: message(namespace, "page.tree", "树形资源详情"), navigation: { id: "example.tree-detail", label: message(namespace, "page.tree", "树形资源详情"), zone: "secondary", order: 30 },
    tree: { id: "service-tree", title: message(namespace, "tree.services", "服务分类"), defaultExpandedDepth: 2 }, detail,
    actions: [{ id: "preview", label: message(namespace, "action.preview", "查看 JSON"), icon: "info", placement: "record.detail", overlay: "preview", requiresSelection: true }],
    overlays: [{ id: "preview", surface: "drawer", title: message(namespace, "action.preview", "查看 JSON"), async load(selected) {
      const row = selected[0];
      const value: JSONValue = row === undefined ? {} : { id: row.id, name: row.name, kind: row.kind, status: row.status, owner: row.owner, description: row.description, updatedAt: row.updatedAt, active: row.active };
      return { kind: "json", documents: [{ value }] };
    } }],
    async loadTree() { return [
      { id: "foundation", title: "Foundation", disabled: true, children: services.filter((item) => item.kind === "Foundation").map((item) => ({ id: item.id, title: item.name, status: { label: item.status, tone: item.tone === "success" ? "success" as const : "neutral" as const } })) },
      { id: "platform", title: "Platform", disabled: true, children: services.filter((item) => item.kind === "Platform").map((item) => ({ id: item.id, title: item.name, status: { label: item.status, tone: "success" as const } })) },
      { id: "product", title: "Product", disabled: true, children: services.filter((item) => item.kind === "Product").map((item) => ({ id: item.id, title: item.name, status: { label: item.status, tone: item.tone === "warning" ? "warning" as const : "neutral" as const } })) },
    ]; },
    async loadRecord(key) { const item = services.find((candidate) => candidate.id === key); return item === undefined ? undefined : { ...item }; },
  });
}

export default {
  register(context: WorkbenchFrontendPluginContext) {
    context.addRecordPage(recordDetailPage());
    context.addRecordPage(masterDetailPage());
    context.addRecordPage(treeDetailPage());
  },
  localization: { defaultLocale: "zh-CN", messages: {
    "zh-CN": { "page.detail":"记录详情","page.master":"列表与编辑","page.tree":"树形资源详情","master.services":"服务列表","tree.services":"服务分类","section.identity":"基本信息","section.state":"运行状态","field.id":"服务 ID","field.name":"服务名称","field.kind":"服务类型","field.owner":"责任团队","field.description":"说明","field.updated":"更新时间","field.status":"状态","field.active":"已启用","editor.title":"编辑服务","action.save":"保存","action.refresh":"刷新记录","action.preview":"查看 JSON","notice.saved":"示例记录已保存","empty.record":"请选择一个服务","filter.name":"服务名称","filter.status":"状态" },
    "en-US": { "page.detail":"Record detail","page.master":"List and editor","page.tree":"Tree and detail","master.services":"Services","tree.services":"Service categories","section.identity":"Identity","section.state":"Runtime state","field.id":"Service ID","field.name":"Service name","field.kind":"Service type","field.owner":"Owner","field.description":"Description","field.updated":"Updated","field.status":"Status","field.active":"Enabled","editor.title":"Edit service","action.save":"Save","action.refresh":"Refresh record","action.preview":"View JSON","notice.saved":"Example record saved","empty.record":"Select a service","filter.name":"Service name","filter.status":"Status" }
  } },
};
