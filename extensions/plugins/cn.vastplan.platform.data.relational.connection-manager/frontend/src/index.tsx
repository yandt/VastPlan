import { createBrowserPlatformAdminClient, type DatabaseConnection, type DatabasePoolPolicy, type PlatformAdminClient, type PutDatabaseConnectionRequest } from "@vastplan/platform-admin";
import {
  defineCollectionPage,
  jsonSchemaDialect,
  managementServicesFor,
  message,
  type CollectionPageDefinition,
  type CollectionQuery,
  type FormSchema,
  type WorkbenchFormDefinition,
  type WorkbenchFormFieldErrors,
  type WorkbenchFrontendPluginContext,
} from "@vastplan/workbench-sdk";

const namespace = "cn.vastplan.platform.data.relational.connection-manager";

const schema: FormSchema = {
  id: "platform-database-connection.v2",
  schema: {
    $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["name", "providerId", "endpoint", "options"],
    properties: {
      name: { type: "string", title: "连接名称", minLength: 1, maxLength: 160 },
      providerId: { type: "string", title: "Provider", oneOf: [{ const: "postgresql", title: "PostgreSQL" }, { const: "mysql", title: "MySQL" }] },
      endpoint: { type: "string", title: "地址", minLength: 1 },
      database: { type: "string", title: "数据库" },
      options: { type: "object", title: "连接选项", additionalProperties: false, required: ["user"], properties: {
        user: { type: "string", title: "用户名", minLength: 1, maxLength: 128 },
        tlsMode: { type: "string", title: "TLS 模式", oneOf: [{ const: "verify-full", title: "完整校验（推荐）" }, { const: "disable", title: "关闭（仅受控测试环境）" }] },
        serverName: { type: "string", title: "TLS Server Name" }, connectTimeoutMs: { type: "integer", title: "连接超时（毫秒）", minimum: 100, maximum: 300000 },
        applicationName: { type: "string", title: "PostgreSQL Application Name" }, network: { type: "string", title: "MySQL 网络", oneOf: [{ const: "tcp", title: "TCP" }, { const: "unix", title: "Unix Socket" }] },
        readTimeoutMs: { type: "integer", title: "MySQL 读取超时（毫秒）", minimum: 0, maximum: 300000 }, writeTimeoutMs: { type: "integer", title: "MySQL 写入超时（毫秒）", minimum: 0, maximum: 300000 }, rejectReadOnly: { type: "boolean", title: "MySQL 拒绝只读实例" },
      } },
      pool: { type: "object", title: "连接池", additionalProperties: false, properties: {
        minIdle: { type: "integer", title: "最小空闲连接", minimum: 0 }, maxIdle: { type: "integer", title: "最大空闲连接", minimum: 0 }, maxOpen: { type: "integer", title: "最大连接数", minimum: 1 },
        maxLifetimeMs: { type: "integer", title: "连接最长生命周期（毫秒）", minimum: 1000 }, maxIdleTimeMs: { type: "integer", title: "最长空闲时间（毫秒）", minimum: 1000 }, acquireTimeoutMs: { type: "integer", title: "获取连接超时（毫秒）", minimum: 100 }, idlePoolTtlMs: { type: "integer", title: "空池回收时间（毫秒）", minimum: 1000 },
      } },
      credentialValue: { type: "string", title: "数据库凭证", format: "vastplan-secret-material", writeOnly: true },
    },
  },
  localization: {
    "/properties/name/title": message(namespace,"form.name","连接名称"), "/properties/providerId/title": message(namespace,"form.provider","Provider"), "/properties/endpoint/title": message(namespace,"form.endpoint","地址"), "/properties/database/title": message(namespace,"form.database","数据库"), "/properties/options/title": message(namespace,"form.options","连接选项"), "/properties/pool/title": message(namespace,"form.pool","连接池"), "/properties/credentialValue/title": message(namespace,"form.credential","数据库凭证"),
    "/properties/options/properties/user/title": message(namespace,"form.user","用户名"), "/properties/options/properties/tlsMode/title": message(namespace,"form.tlsMode","TLS 模式"), "/properties/options/properties/connectTimeoutMs/title": message(namespace,"form.connectTimeout","连接超时（毫秒）"),
    "/properties/options/properties/serverName/title": message(namespace,"form.serverName","TLS Server Name"), "/properties/options/properties/applicationName/title": message(namespace,"form.applicationName","PostgreSQL Application Name"), "/properties/options/properties/network/title": message(namespace,"form.network","MySQL 网络"), "/properties/options/properties/readTimeoutMs/title": message(namespace,"form.readTimeout","MySQL 读取超时（毫秒）"), "/properties/options/properties/writeTimeoutMs/title": message(namespace,"form.writeTimeout","MySQL 写入超时（毫秒）"), "/properties/options/properties/rejectReadOnly/title": message(namespace,"form.rejectReadOnly","MySQL 拒绝只读实例"),
    "/properties/options/properties/tlsMode/oneOf/0/title": message(namespace,"option.verifyFull","完整校验（推荐）"), "/properties/options/properties/tlsMode/oneOf/1/title": message(namespace,"option.tlsDisable","关闭（仅受控测试环境）"),
    "/properties/pool/properties/minIdle/title": message(namespace,"form.minIdle","最小空闲连接"), "/properties/pool/properties/maxIdle/title": message(namespace,"form.maxIdle","最大空闲连接"), "/properties/pool/properties/maxOpen/title": message(namespace,"form.maxOpen","最大连接数"), "/properties/pool/properties/maxLifetimeMs/title": message(namespace,"form.maxLifetime","连接最长生命周期（毫秒）"), "/properties/pool/properties/maxIdleTimeMs/title": message(namespace,"form.maxIdleTime","最长空闲时间（毫秒）"), "/properties/pool/properties/acquireTimeoutMs/title": message(namespace,"form.acquireTimeout","获取连接超时（毫秒）"), "/properties/pool/properties/idlePoolTtlMs/title": message(namespace,"form.idlePoolTtl","空池回收时间（毫秒）"),
  },
};

type DatabaseRow = DatabaseConnection & { credentialState: "managed" | "missing"; credentialVersion?: number } & Record<string, unknown>;

const defaults = (): Readonly<Record<string, unknown>> => ({ providerId: "postgresql", options: { tlsMode: "verify-full", connectTimeoutMs: 10000 }, pool: { minIdle: 0, maxIdle: 8, maxOpen: 32, maxLifetimeMs: 1800000, maxIdleTimeMs: 300000, acquireTimeoutMs: 5000, idlePoolTtlMs: 900000 } });

export function createDatabaseConnectionsPage(client: PlatformAdminClient, serviceID: string, path: string, title: ReturnType<typeof message>): CollectionPageDefinition<DatabaseRow> {
  const form = (id: "create" | "edit"): WorkbenchFormDefinition<DatabaseRow> => ({
    id,
    schema,
    context: { editing: id === "edit" },
    presentation: {
      layout: "vertical", navigation: "sections",
      sections: [
        { id: "identity", title: message(namespace, "section.identity", "连接标识"), columns: 2, fields: ["/name", "/providerId", "/endpoint", "/database"] },
        { id: "options", title: message(namespace, "section.options", "Provider 连接选项"), columns: 1, fields: ["/options"] },
        { id: "pool", title: message(namespace, "section.pool", "连接池策略"), columns: 1, fields: ["/pool"] },
        { id: "credential", title: message(namespace, "section.credential", "托管凭证"), columns: 1, fields: ["/credentialValue"] },
      ],
      fields: [
        { pointer: "/name", readOnlyWhen: { pointer: "/context/editing", equals: true } },
        { pointer: "/providerId" }, { pointer: "/endpoint" }, { pointer: "/database" }, { pointer: "/options" }, { pointer: "/pool" },
        { pointer: "/options/applicationName", visibleWhen: { pointer: "/providerId", equals: "postgresql" } },
        { pointer: "/options/network", visibleWhen: { pointer: "/providerId", equals: "mysql" } },
        { pointer: "/options/readTimeoutMs", visibleWhen: { pointer: "/providerId", equals: "mysql" } },
        { pointer: "/options/writeTimeoutMs", visibleWhen: { pointer: "/providerId", equals: "mysql" } },
        { pointer: "/options/rejectReadOnly", visibleWhen: { pointer: "/providerId", equals: "mysql" } },
        { pointer: "/credentialValue", widget: "secretMaterial", help: message(namespace, id === "create" ? "form.credentialCreateHelp" : "form.credentialEditHelp", id === "create" ? "新建连接必须输入；提交后立即清除。" : "留空会保留现有托管凭证；输入新值会原子替换，提交后立即清除。") },
      ],
    },
    workflow: {
      surface: "drawer", size: "lg",
      title: message(namespace, id === "create" ? "form.createTitle" : "form.editTitle", id === "create" ? "新增数据库连接" : "编辑数据库连接"),
      description: message(namespace, "form.description", "连接定义和池策略保存在数据库插件中；凭证明文仅用于本次 TLS 请求。"),
      submitLabel: message(namespace, "action.save", "保存"),
      success: { notify: message(namespace, "notice.saved", "数据库连接已保存"), refreshCollection: true, close: true },
    },
    ...(id === "create" ? { initialValue: defaults() } : {}),
    async load(selected) {
      const item = selected[0];
      if (item === undefined) return defaults();
      return { name: item.name, providerId: item.providerId, endpoint: item.endpoint, ...(item.database ? { database: item.database } : {}), options: item.options, pool: item.pool };
    },
    async validate({ value }): Promise<WorkbenchFormFieldErrors> {
      if (id === "create" && (typeof value.credentialValue !== "string" || value.credentialValue === "")) return { credentialValue: message(namespace, "error.credentialRequired", "新建连接必须输入数据库凭证") };
      return {};
    },
    async submit({ value }) {
      const name = text(value.name), providerId = text(value.providerId), endpoint = text(value.endpoint), options = record(value.options);
      if (name === undefined || providerId === undefined || endpoint === undefined || options === undefined) return { fieldErrors: { name: message(namespace,"error.nameRequired","连接名称不能为空"), endpoint: message(namespace,"error.endpointRequired","连接地址不能为空") } };
      const request: PutDatabaseConnectionRequest = { providerId, endpoint, options, ...(text(value.database) === undefined ? {} : { database: text(value.database) }), ...(pool(value.pool) === undefined ? {} : { pool: pool(value.pool) }), ...(text(value.credentialValue) === undefined ? {} : { credentialValue: text(value.credentialValue) }) };
      await client.putDatabaseConnection(name, request);
    },
  });

  return defineCollectionPage<DatabaseRow>({
    id: `platform.database-connections.${serviceID}`, path, title,
    description: message(namespace, "page.description", "在数据库插件内配置连接、Provider、连接池与托管凭证"),
    navigation: { id: `platform.database-connections.${serviceID}`, label: title, zone: "settings", order: 40 },
    collection: {
      id: `platform.database-connections.${serviceID}`, title, view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20, 50, 100] },
      filters: [{ id: "name", label: message(namespace, "filter.name", "连接名称"), kind: "text" }, { id: "providerId", label: message(namespace, "filter.provider", "Provider"), kind: "select", options: [{ value: "postgresql", label: "PostgreSQL" }, { value: "mysql", label: "MySQL" }] }],
      columns: [
        { key: "name", label: message(namespace,"column.name","名称"), defaultVisible: true, minWidth: 180 }, { key: "providerId", label: message(namespace,"column.provider","Provider"), defaultVisible: true, minWidth: 120 }, { key: "endpoint", label: message(namespace,"column.endpoint","地址"), defaultVisible: true, minWidth: 220 },
        { key: "runtime", label: message(namespace,"column.runtime","Runtime"), format: "status", valueLabels: { ready: message(namespace,"runtime.ready","已就绪"), pending: message(namespace,"runtime.pending","待发布") }, statusTones: { ready: "success", pending: "warning" }, defaultVisible: true, minWidth: 100 },
        { key: "credentialState", label: message(namespace,"column.credential","凭证"), format: "status", valueLabels: { managed: message(namespace,"credential.managed","已托管"), missing: message(namespace,"credential.missing","未配置") }, statusTones: { managed: "success", missing: "warning" }, defaultVisible: true, minWidth: 110 },
        { key: "credentialVersion", label: message(namespace,"column.credentialVersion","凭证版本"), format: "number", defaultVisible: false, minWidth: 100 },
      ],
      actions: [
        { id: "create", label: message(namespace,"action.create","新增连接"), icon: "add", placement: "page.primary", tone: "primary", form: "create" }, { id: "edit", label: message(namespace,"action.edit","编辑"), placement: "record.row", form: "edit" }, { id: "probe", label: message(namespace,"action.probe","探测"), placement: "record.row" }, { id: "delete", label: message(namespace,"action.delete","删除"), placement: "record.row", tone: "danger", confirm: message(namespace,"confirm.delete","确认删除此连接并退役其托管凭证？") },
      ],
    },
    forms: [form("create"), form("edit")],
    async load(query: CollectionQuery, signal) {
      const name = typeof query.filters.name === "string" ? query.filters.name.trim().toLowerCase() : "";
      const provider = typeof query.filters.providerId === "string" ? query.filters.providerId : "";
      const values = (await client.listDatabaseConnections()).filter((item) => (name === "" || item.name.toLowerCase().includes(name)) && (provider === "" || item.providerId === provider));
      if (signal.aborted) return { items: [], total: 0 };
      const rows = values.map((item) => ({ ...item, credentialState: item.credential.managed ? "managed" : "missing", ...(item.credential.managed ? { credentialVersion: item.credential.version } : {}) }) as DatabaseRow);
      const start = Math.max(0, (query.page - 1) * query.pageSize);
      return { items: rows.slice(start, start + query.pageSize), total: rows.length };
    },
    async runAction({ action, selected }) {
      const item = selected[0]; if (item === undefined) return;
      if (action.id === "delete") { await client.deleteDatabaseConnection(item.name); return { notify: { title: action.label, kind: "success" } }; }
      if (action.id === "probe") {
        const result = await client.probeDatabaseConnection(item.name);
        return { notify: { title: message(namespace, result.ready ? "status.ready" : "status.unavailable", result.ready ? "连接正常" : "连接不可用"), ...(result.message === undefined ? {} : { content: result.message }), kind: result.ready ? "success" : "error" } };
      }
    },
  });
}

function text(value: unknown): string | undefined { return typeof value === "string" && value !== "" ? value : undefined; }
function record(value: unknown): Record<string, unknown> | undefined { return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : undefined; }
function pool(value: unknown): DatabasePoolPolicy | undefined { return record(value) as DatabasePoolPolicy | undefined; }

export default {
  register(context: WorkbenchFrontendPluginContext) {
    const services = managementServicesFor(context.portal, "platform.database");
    if (services.length === 0) throw new Error("Portal 未绑定 platform.database 服务");
    for (const service of services) {
      const client = createBrowserPlatformAdminClient(context.portal.id, service.id);
      const suffix = services.length === 1 ? "" : `/${service.id}`;
      const title = context.i18n.message(services.length === 1 ? "page.title" : "page.titleService", services.length === 1 ? "数据库连接" : "数据库连接 · {service}", { service: service.label ?? service.id });
      context.addCollectionPage(createDatabaseConnectionsPage(client, service.id, `/settings/databases${suffix}`, title));
    }
  },
  localization: { defaultLocale:"zh-CN", messages: {
    "zh-CN": { "form.name":"连接名称","form.provider":"Provider","form.endpoint":"地址","form.database":"数据库","form.options":"连接选项","form.pool":"连接池","form.credential":"数据库凭证","form.user":"用户名","form.tlsMode":"TLS 模式","form.connectTimeout":"连接超时（毫秒）","form.serverName":"TLS Server Name","form.applicationName":"PostgreSQL Application Name","form.network":"MySQL 网络","form.readTimeout":"MySQL 读取超时（毫秒）","form.writeTimeout":"MySQL 写入超时（毫秒）","form.rejectReadOnly":"MySQL 拒绝只读实例","option.verifyFull":"完整校验（推荐）","option.tlsDisable":"关闭（仅受控测试环境）","form.minIdle":"最小空闲连接","form.maxIdle":"最大空闲连接","form.maxOpen":"最大连接数","form.maxLifetime":"连接最长生命周期（毫秒）","form.maxIdleTime":"最长空闲时间（毫秒）","form.acquireTimeout":"获取连接超时（毫秒）","form.idlePoolTtl":"空池回收时间（毫秒）","section.identity":"连接标识","section.options":"Provider 连接选项","section.pool":"连接池策略","section.credential":"托管凭证","form.credentialCreateHelp":"新建连接必须输入；提交后立即清除。","form.credentialEditHelp":"留空会保留现有托管凭证；输入新值会原子替换，提交后立即清除。","form.createTitle":"新增数据库连接","form.editTitle":"编辑数据库连接","form.description":"连接定义和池策略保存在数据库插件中；凭证明文仅用于本次 TLS 请求。","error.credentialRequired":"新建连接必须输入数据库凭证","error.nameRequired":"连接名称不能为空","error.endpointRequired":"连接地址不能为空","notice.saved":"数据库连接已保存","filter.name":"连接名称","filter.provider":"Provider","column.name":"名称","column.provider":"Provider","column.endpoint":"地址","column.runtime":"Runtime","column.credential":"凭证","column.credentialVersion":"凭证版本","runtime.ready":"已就绪","runtime.pending":"待发布","credential.managed":"已托管","credential.missing":"未配置","action.create":"新增连接","action.edit":"编辑","action.save":"保存","action.probe":"探测","action.delete":"删除","confirm.delete":"确认删除此连接并退役其托管凭证？","status.ready":"连接正常","status.unavailable":"连接不可用","page.title":"数据库连接","page.titleService":"数据库连接 · {service}","page.description":"在数据库插件内配置连接、Provider、连接池与托管凭证" },
    "en-US": { "form.name":"Connection name","form.provider":"Provider","form.endpoint":"Endpoint","form.database":"Database","form.options":"Connection options","form.pool":"Connection pool","form.credential":"Database credential","form.user":"User","form.tlsMode":"TLS mode","form.connectTimeout":"Connect timeout (ms)","form.serverName":"TLS server name","form.applicationName":"PostgreSQL application name","form.network":"MySQL network","form.readTimeout":"MySQL read timeout (ms)","form.writeTimeout":"MySQL write timeout (ms)","form.rejectReadOnly":"Reject read-only MySQL instances","option.verifyFull":"Verify fully (recommended)","option.tlsDisable":"Disabled (controlled test environments only)","form.minIdle":"Minimum idle","form.maxIdle":"Maximum idle","form.maxOpen":"Maximum open","form.maxLifetime":"Maximum lifetime (ms)","form.maxIdleTime":"Maximum idle time (ms)","form.acquireTimeout":"Acquire timeout (ms)","form.idlePoolTtl":"Idle-pool TTL (ms)","section.identity":"Connection identity","section.options":"Provider options","section.pool":"Pool policy","section.credential":"Managed credential","form.credentialCreateHelp":"Required for a new connection and cleared immediately after submission.","form.credentialEditHelp":"Leave blank to retain the managed credential; a new value replaces it atomically and is then cleared.","form.createTitle":"Create database connection","form.editTitle":"Edit database connection","form.description":"The database plugin stores connection and pool policy. Plaintext is used only for this TLS request.","error.credentialRequired":"A database credential is required for a new connection","error.nameRequired":"Connection name is required","error.endpointRequired":"Endpoint is required","notice.saved":"Database connection saved","filter.name":"Connection name","filter.provider":"Provider","column.name":"Name","column.provider":"Provider","column.endpoint":"Endpoint","column.runtime":"Runtime","column.credential":"Credential","column.credentialVersion":"Credential version","runtime.ready":"Ready","runtime.pending":"Pending publication","credential.managed":"Managed","credential.missing":"Missing","action.create":"Create connection","action.edit":"Edit","action.save":"Save","action.probe":"Probe","action.delete":"Delete","confirm.delete":"Delete this connection and retire its managed credential?","status.ready":"Connection ready","status.unavailable":"Connection unavailable","page.title":"Database connections","page.titleService":"Database connections · {service}","page.description":"Configure providers, pools and managed credentials in the database plugin" }
  } },
};
