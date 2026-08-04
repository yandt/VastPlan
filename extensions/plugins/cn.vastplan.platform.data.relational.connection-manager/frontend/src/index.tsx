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
import { connectionTestFailure, connectionTestResult } from "./connection-test.js";
import { connectionEndpoint, connectionEndpointFields, defaultConnectionPort } from "./connection-endpoint.js";

const namespace = "cn.vastplan.platform.data.relational.connection-manager";

function databaseConnectionSchema(passwordRequired: boolean): FormSchema {
  return {
  id: "platform-database-connection.v2",
  schema: {
    $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["name", "providerId", "options"],
    properties: {
      name: { type: "string", title: "连接名称", minLength: 1, maxLength: 160 },
      providerId: { type: "string", title: "数据库类型", oneOf: [{ const: "postgresql", title: "PostgreSQL" }, { const: "mysql", title: "MySQL" }] },
      host: { type: "string", title: "地址", minLength: 1 },
      port: { type: "integer", title: "端口", minimum: 1, maximum: 65535 },
      socketPath: { type: "string", title: "Unix 套接字路径", minLength: 1 },
      database: { type: "string", title: "数据库" },
      // 该对象由 presentation.sections 的“连接选项”命名；不再定义内部标题，
      // 从源头避免 RJSF 与各 Renderer 重复渲染对象标题。
      options: { type: "object", additionalProperties: false, required: passwordRequired ? ["user", "password"] : ["user"], properties: {
        user: { type: "string", title: "用户名", minLength: 1, maxLength: 128 },
        password: { type: "string", title: "密码", format: "vastplan-secret-material", writeOnly: true },
        tlsMode: { type: "string", title: "传输加密模式", oneOf: [{ const: "verify-full", title: "完整校验（推荐）" }, { const: "disable", title: "关闭（仅受控测试环境）" }] },
        serverName: { type: "string", title: "证书校验服务器名称" }, connectTimeoutMs: { type: "integer", title: "连接超时", minimum: 100, maximum: 300000 },
        applicationName: { type: "string", title: "客户端应用名称" }, network: { type: "string", title: "网络类型", oneOf: [{ const: "tcp", title: "TCP" }, { const: "unix", title: "Unix 套接字" }] },
        readTimeoutMs: { type: "integer", title: "读取超时", minimum: 0, maximum: 300000 }, writeTimeoutMs: { type: "integer", title: "写入超时", minimum: 0, maximum: 300000 }, rejectReadOnly: { type: "boolean", title: "拒绝只读实例" },
      } },
      // 同上：唯一可见标题由“连接池策略”分段提供。
      pool: { type: "object", additionalProperties: false, properties: {
        minIdle: { type: "integer", title: "最小空闲连接", minimum: 0 }, maxIdle: { type: "integer", title: "最大空闲连接", minimum: 0 }, maxOpen: { type: "integer", title: "最大连接数", minimum: 1 },
        maxLifetimeMs: { type: "integer", title: "连接最长生命周期", minimum: 1000 }, maxIdleTimeMs: { type: "integer", title: "最长空闲时间", minimum: 1000 }, acquireTimeoutMs: { type: "integer", title: "获取连接超时", minimum: 100 }, idlePoolTtlMs: { type: "integer", title: "空池回收时间", minimum: 1000 },
      } },
    },
    allOf: [{
      if: { required: ["providerId", "options"], properties: { providerId: { const: "mysql" }, options: { required: ["network"], properties: { network: { const: "unix" } } } } },
      then: { required: ["socketPath"] },
      else: { required: ["host", "port"] },
    }],
  },
  localization: {
    "/properties/name/title": message(namespace,"form.name","连接名称"), "/properties/providerId/title": message(namespace,"form.provider","数据库类型"), "/properties/host/title": message(namespace,"form.host","地址"), "/properties/port/title": message(namespace,"form.port","端口"), "/properties/socketPath/title": message(namespace,"form.socketPath","Unix 套接字路径"), "/properties/database/title": message(namespace,"form.database","数据库"),
    "/properties/options/properties/user/title": message(namespace,"form.user","用户名"), "/properties/options/properties/password/title": message(namespace,"form.credential","密码"), "/properties/options/properties/tlsMode/title": message(namespace,"form.tlsMode","传输加密模式"), "/properties/options/properties/connectTimeoutMs/title": message(namespace,"form.connectTimeout","连接超时"),
    "/properties/options/properties/serverName/title": message(namespace,"form.serverName","证书校验服务器名称"), "/properties/options/properties/applicationName/title": message(namespace,"form.applicationName","客户端应用名称"), "/properties/options/properties/network/title": message(namespace,"form.network","网络类型"), "/properties/options/properties/readTimeoutMs/title": message(namespace,"form.readTimeout","读取超时"), "/properties/options/properties/writeTimeoutMs/title": message(namespace,"form.writeTimeout","写入超时"), "/properties/options/properties/rejectReadOnly/title": message(namespace,"form.rejectReadOnly","拒绝只读实例"),
    "/properties/options/properties/tlsMode/oneOf/0/title": message(namespace,"option.verifyFull","完整校验（推荐）"), "/properties/options/properties/tlsMode/oneOf/1/title": message(namespace,"option.tlsDisable","关闭（仅受控测试环境）"),
    "/properties/options/properties/network/oneOf/0/title": message(namespace,"option.networkTcp","TCP"), "/properties/options/properties/network/oneOf/1/title": message(namespace,"option.networkUnix","Unix 套接字"),
    "/properties/pool/properties/minIdle/title": message(namespace,"form.minIdle","最小空闲连接"), "/properties/pool/properties/maxIdle/title": message(namespace,"form.maxIdle","最大空闲连接"), "/properties/pool/properties/maxOpen/title": message(namespace,"form.maxOpen","最大连接数"), "/properties/pool/properties/maxLifetimeMs/title": message(namespace,"form.maxLifetime","连接最长生命周期"), "/properties/pool/properties/maxIdleTimeMs/title": message(namespace,"form.maxIdleTime","最长空闲时间"), "/properties/pool/properties/acquireTimeoutMs/title": message(namespace,"form.acquireTimeout","获取连接超时"), "/properties/pool/properties/idlePoolTtlMs/title": message(namespace,"form.idlePoolTtl","空池回收时间"),
  },
  // 数据库凭证不是本站登录凭证；避免浏览器回填本地数据库账号。
  // RJSF 的 Ant 输入适配器从 ui:options.autocomplete 映射到原生 autoComplete。
  uiSchema: { options: { user: { "ui:options": { autocomplete: "new-password" } }, password: { "ui:options": { autocomplete: "new-password" } } } },
  };
}

type DatabaseRow = DatabaseConnection & { credentialState: "managed" | "missing"; credentialVersion?: number } & Record<string, unknown>;

const defaults = (): Readonly<Record<string, unknown>> => ({ providerId: "postgresql", port: defaultConnectionPort("postgresql"), options: { tlsMode: "verify-full", connectTimeoutMs: 10000 }, pool: { minIdle: 0, maxIdle: 8, maxOpen: 32, maxLifetimeMs: 1800000, maxIdleTimeMs: 300000, acquireTimeoutMs: 5000, idlePoolTtlMs: 900000 } });

export function createDatabaseConnectionsPage(client: PlatformAdminClient, serviceID: string, path: string, title: ReturnType<typeof message>): CollectionPageDefinition<DatabaseRow> {
  const form = (id: "create" | "edit"): WorkbenchFormDefinition<DatabaseRow> => ({
    id,
    schema: databaseConnectionSchema(id === "create"),
    context: { editing: id === "edit" },
    presentation: {
      // 管理表单保持统一的行内 Label；FormDialog 只负责 Overlay 几何，
      // Dynamic Form/Renderer 根据该语义统一计算 Label 列宽和字段宽度。
      layout: "horizontal", labelPlacement: "inline", navigation: "sections",
      sections: [
        { id: "identity", title: message(namespace, "section.identity", "连接标识"), columns: 2, fields: ["/name", "/providerId", "/host", "/port", "/socketPath", "/database"] },
        { id: "options", title: message(namespace, "section.options", "连接选项"), columns: 2, fields: ["/options"] },
        { id: "pool", title: message(namespace, "section.pool", "连接池策略"), columns: 2, fields: ["/pool"] },
      ],
      fields: [
        { pointer: "/name", readOnlyWhen: { pointer: "/context/editing", equals: true } },
        { pointer: "/providerId" },
        { pointer: "/host", visibleWhen: { not: { all: [{ pointer: "/providerId", equals: "mysql" }, { pointer: "/options/network", equals: "unix" }] } } },
        { pointer: "/port", visibleWhen: { not: { all: [{ pointer: "/providerId", equals: "mysql" }, { pointer: "/options/network", equals: "unix" }] } } },
        { pointer: "/socketPath", visibleWhen: { all: [{ pointer: "/providerId", equals: "mysql" }, { pointer: "/options/network", equals: "unix" }] } },
        { pointer: "/database" }, { pointer: "/options", span: 2 }, { pointer: "/pool", span: 2 },
        { pointer: "/options/connectTimeoutMs", widget: "duration", duration: { storageUnit: "millisecond", units: ["millisecond", "second", "minute"], defaultUnit: "second" } },
        { pointer: "/options/serverName", visibleWhen: { pointer: "/options/tlsMode", equals: "verify-full" } },
        { pointer: "/options/applicationName", visibleWhen: { pointer: "/providerId", equals: "postgresql" } },
        { pointer: "/options/network", visibleWhen: { pointer: "/providerId", equals: "mysql" } },
        { pointer: "/options/readTimeoutMs", widget: "duration", duration: { storageUnit: "millisecond", units: ["millisecond", "second", "minute"], defaultUnit: "second" }, visibleWhen: { pointer: "/providerId", equals: "mysql" } },
        { pointer: "/options/writeTimeoutMs", widget: "duration", duration: { storageUnit: "millisecond", units: ["millisecond", "second", "minute"], defaultUnit: "second" }, visibleWhen: { pointer: "/providerId", equals: "mysql" } },
        { pointer: "/options/rejectReadOnly", visibleWhen: { pointer: "/providerId", equals: "mysql" } },
        { pointer: "/options/password", widget: "secretMaterial" },
        { pointer: "/pool/maxLifetimeMs", widget: "duration", duration: { storageUnit: "millisecond", units: ["second", "minute", "hour", "day", "week"], defaultUnit: "minute" } },
        { pointer: "/pool/maxIdleTimeMs", widget: "duration", duration: { storageUnit: "millisecond", units: ["second", "minute", "hour", "day"], defaultUnit: "minute" } },
        { pointer: "/pool/acquireTimeoutMs", widget: "duration", duration: { storageUnit: "millisecond", units: ["millisecond", "second", "minute"], defaultUnit: "second" } },
        { pointer: "/pool/idlePoolTtlMs", widget: "duration", duration: { storageUnit: "millisecond", units: ["second", "minute", "hour", "day", "week"], defaultUnit: "minute" } },
      ],
    },
    workflow: {
      dialogWidth: "lg",
      title: message(namespace, id === "create" ? "form.createTitle" : "form.editTitle", id === "create" ? "新增数据库连接" : "编辑数据库连接"),
      description: message(namespace, "form.description", "连接定义和池策略保存在数据库插件中；密码明文仅用于本次连接请求。"),
      submitLabel: message(namespace, "action.save", "保存"),
      actions: [{ id: "test", label: message(namespace, "action.test", "测试连接"), icon: "success", placement: "footer.start", requiresValid: true }],
      success: { notify: message(namespace, "notice.saved", "数据库连接已保存"), refreshCollection: true, close: true },
    },
    ...(id === "create" ? { initialValue: defaults() } : {}),
    async load(selected) {
      const item = selected[0];
      if (item === undefined) return defaults();
      if (item.providerId === "mysql" && record(item.options)?.network === "unix") {
        return { name: item.name, providerId: item.providerId, socketPath: item.endpoint, ...(item.database ? { database: item.database } : {}), options: item.options, pool: item.pool };
      }
      const endpoint = connectionEndpointFields(item.endpoint, item.providerId);
      return { name: item.name, providerId: item.providerId, host: endpoint.host, port: endpoint.port, ...(item.database ? { database: item.database } : {}), options: item.options, pool: item.pool };
    },
    async validate({ value }): Promise<WorkbenchFormFieldErrors> {
      const options = record(value.options);
      const mysqlUnix = text(value.providerId) === "mysql" && text(record(value.options)?.network) === "unix";
      const endpointInvalid = mysqlUnix ? text(value.socketPath)?.startsWith("/") !== true : connectionEndpoint(value.host, value.port) === undefined;
      return {
        ...(text(options?.user) === undefined ? { "/options/user": message(namespace, "error.userRequired", "用户名不能为空") } : {}),
        ...(id === "create" && text(options?.password) === undefined ? { "/options/password": message(namespace, "error.credentialRequired", "新建连接必须输入密码") } : {}),
        ...(endpointInvalid ? (mysqlUnix
          ? { "/socketPath": message(namespace, "error.socketPathInvalid", "请输入以 / 开头的 Unix 套接字路径") }
          : { "/host": message(namespace, "error.hostInvalid", "请输入不含端口的有效地址"), "/port": message(namespace, "error.portInvalid", "端口必须为 1 到 65535 的整数") }) : {}),
      };
    },
    async submit({ value }) {
      const input = databaseConnectionInput(value);
      if (input === undefined) return { fieldErrors: requiredConnectionErrors(value) };
      await client.putDatabaseConnection(input.name, input.request);
    },
    async runAction({ action, value }) {
      if (action.id !== "test") return;
      const input = databaseConnectionInput(value);
      if (input === undefined) return { fieldErrors: requiredConnectionErrors(value) };
      try {
        return { notify: connectionTestResult(await client.testDatabaseConnection(input.name, input.request)) };
      } catch (error) {
        return { notify: connectionTestFailure(error) };
      }
    },
  });

  return defineCollectionPage<DatabaseRow>({
    id: `platform.database-connections.${serviceID}`, path, title,
    description: message(namespace, "page.description", "在数据库插件内配置连接、数据库类型、连接池与密码"),
    navigation: { id: `platform.database-connections.${serviceID}`, label: title, parentMenuRef: { pluginID: "cn.vastplan.platform.data.relational.connection-manager", nodeID: "databases" }, order: 40 },
    pageActions: [{ id: "create", label: message(namespace,"action.create","新增连接"), icon: "add", tone: "primary", form: "create" }],
    collection: {
      id: `platform.database-connections.${serviceID}`, title, view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20, 50, 100] },
      filterPanel: { fields: [{ id: "name", label: message(namespace, "filter.name", "连接名称"), kind: "text" }, { id: "providerId", label: message(namespace, "filter.provider", "数据库类型"), kind: "select", options: [{ value: "postgresql", label: "PostgreSQL" }, { value: "mysql", label: "MySQL" }] }] },
      columns: [
        { key: "name", label: message(namespace,"column.name","名称"), defaultVisible: true, minWidth: 180 }, { key: "providerId", label: message(namespace,"column.provider","数据库类型"), defaultVisible: true, minWidth: 120 }, { key: "endpoint", label: message(namespace,"column.endpoint","地址"), defaultVisible: true, minWidth: 220 },
        { key: "runtime", label: message(namespace,"column.runtime","运行状态"), format: "status", valueLabels: { ready: message(namespace,"runtime.ready","已就绪"), pending: message(namespace,"runtime.pending","待发布") }, statusTones: { ready: "success", pending: "warning" }, defaultVisible: true, minWidth: 100 },
        { key: "credentialState", label: message(namespace,"column.credential","密码状态"), format: "status", valueLabels: { managed: message(namespace,"credential.managed","已设置"), missing: message(namespace,"credential.missing","未设置") }, statusTones: { managed: "success", missing: "warning" }, defaultVisible: true, minWidth: 110 },
        { key: "credentialVersion", label: message(namespace,"column.credentialVersion","密码版本"), format: "number", defaultVisible: false, minWidth: 100 },
      ],
      actions: [
        { id: "edit", label: message(namespace,"action.edit","编辑"), icon: "edit", placement: "record.row", form: "edit" }, { id: "probe", label: message(namespace,"action.test","测试连接"), icon: "success", placement: "record.row" }, { id: "delete", label: message(namespace,"action.delete","删除"), icon: "remove", placement: "record.row", tone: "danger", confirm: message(namespace,"confirm.delete","确认删除此连接并清理其密码？") },
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
        try {
          return { notify: connectionTestResult(await client.probeDatabaseConnection(item.name)) };
        } catch (error) {
          return { notify: connectionTestFailure(error) };
        }
      }
    },
  });
}

function text(value: unknown): string | undefined { return typeof value === "string" && value !== "" ? value : undefined; }
function record(value: unknown): Record<string, unknown> | undefined { return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : undefined; }
function pool(value: unknown): DatabasePoolPolicy | undefined { return record(value) as DatabasePoolPolicy | undefined; }
function databaseConnectionInput(value: Readonly<Record<string, unknown>>): { name: string; request: PutDatabaseConnectionRequest } | undefined {
  const name = text(value.name), providerId = text(value.providerId), options = record(value.options);
  const mysqlUnix = providerId === "mysql" && text(options?.network) === "unix";
  const endpoint = mysqlUnix ? text(value.socketPath) : connectionEndpoint(value.host, value.port);
  if (name === undefined || providerId === undefined || endpoint === undefined || options === undefined) return undefined;
  const password = text(options.password);
  const connectionOptions = providerConnectionOptions(providerId, options);
  return { name, request: { providerId, endpoint, options: connectionOptions, ...(text(value.database) === undefined ? {} : { database: text(value.database) }), ...(pool(value.pool) === undefined ? {} : { pool: pool(value.pool) }), ...(text(password) === undefined ? {} : { credentialValue: text(password) }) } };
}

// Keep a Provider's persisted wire options to its declared schema. Hidden
// fields can remain in a browser form after the user switches Provider; they
// must not be sent to another Provider and turn a valid connection test into a
// generic Runtime request rejection.
function providerConnectionOptions(providerId: string, options: Record<string, unknown>): Record<string, unknown> {
  const common = pickOptions(options, ["user", "tlsMode", "serverName", "connectTimeoutMs"]);
  if (providerId === "postgresql") return pickOptions(options, ["user", "tlsMode", "serverName", "connectTimeoutMs", "applicationName"]);
  if (providerId === "mysql") return { ...common, ...pickOptions(options, ["network", "readTimeoutMs", "writeTimeoutMs", "rejectReadOnly"]) };
  return common;
}

function pickOptions(source: Record<string, unknown>, keys: readonly string[]): Record<string, unknown> {
  const result: Record<string, unknown> = {};
  for (const key of keys) if (source[key] !== undefined) result[key] = source[key];
  return result;
}
function requiredConnectionErrors(value: Readonly<Record<string, unknown>>): WorkbenchFormFieldErrors {
  const options = record(value.options);
  const mysqlUnix = text(value.providerId) === "mysql" && text(options?.network) === "unix";
  return {
    ...(text(value.name) === undefined ? { "/name": message(namespace,"error.nameRequired","连接名称不能为空") } : {}),
    ...(text(options?.user) === undefined ? { "/options/user": message(namespace,"error.userRequired","用户名不能为空") } : {}),
    ...(mysqlUnix
      ? { "/socketPath": message(namespace,"error.socketPathInvalid","请输入以 / 开头的 Unix 套接字路径") }
      : { "/host": message(namespace,"error.hostInvalid","请输入不含端口的有效地址"), "/port": message(namespace,"error.portInvalid","端口必须为 1 到 65535 的整数") }),
  };
}

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
"zh-CN": { "form.name":"连接名称","form.provider":"数据库类型","form.host":"地址","form.port":"端口","form.socketPath":"Unix 套接字路径","form.database":"数据库","form.options":"连接选项","form.pool":"连接池","form.credential":"密码","form.user":"用户名","form.tlsMode":"传输加密模式","form.connectTimeout":"连接超时","form.serverName":"证书校验服务器名称","form.applicationName":"客户端应用名称","form.network":"网络类型","form.readTimeout":"读取超时","form.writeTimeout":"写入超时","form.rejectReadOnly":"拒绝只读实例","option.verifyFull":"完整校验（推荐）","option.tlsDisable":"关闭（仅受控测试环境）","option.networkTcp":"TCP","option.networkUnix":"Unix 套接字","form.minIdle":"最小空闲连接","form.maxIdle":"最大空闲连接","form.maxOpen":"最大连接数","form.maxLifetime":"连接最长生命周期","form.maxIdleTime":"最长空闲时间","form.acquireTimeout":"获取连接超时","section.identity":"连接标识","section.options":"连接选项","section.pool":"连接池策略","form.createTitle":"新增数据库连接","form.editTitle":"编辑数据库连接","form.description":"连接定义和池策略保存在数据库插件中；密码明文仅用于本次连接请求。","error.credentialRequired":"新建连接必须输入密码","error.nameRequired":"连接名称不能为空","error.userRequired":"用户名不能为空","error.hostInvalid":"请输入不含端口的有效地址","error.portInvalid":"端口必须为 1 到 65535 的整数","error.socketPathInvalid":"请输入以 / 开头的 Unix 套接字路径","notice.saved":"数据库连接已保存","filter.name":"连接名称","filter.provider":"数据库类型","column.name":"名称","column.provider":"数据库类型","column.endpoint":"地址","column.runtime":"运行状态","column.credential":"密码状态","column.credentialVersion":"密码版本","runtime.ready":"已就绪","runtime.pending":"待发布","credential.managed":"已设置","credential.missing":"未设置","action.create":"新增连接","action.edit":"编辑","action.save":"保存","action.test":"测试连接","action.delete":"删除","confirm.delete":"确认删除此连接并清理其密码？","test.success":"连接测试成功","test.successDetail":"{provider} 响应正常，耗时 {latency} 毫秒","test.failure":"连接测试失败","test.failureDetail":"请检查地址、数据库、用户名、密码和传输加密设置后重试。","test.invalidDetail":"连接参数无效，请检查地址、端口、用户名和传输加密设置。","test.tlsPolicyDetail":"当前部署策略不允许关闭传输加密校验，请启用完整校验或联系平台管理员。","test.dnsDetail":"数据库地址无法解析，请检查主机名和 DNS 配置。","test.refusedDetail":"数据库服务器拒绝了连接，请检查地址、端口和服务监听状态。","test.timeoutDetail":"连接数据库超时，请检查网络、防火墙和连接超时设置。","test.tlsDetail":"传输加密或证书校验失败，请检查证书信任链和证书校验服务器名称。","test.authenticationDetail":"用户名或密码验证失败，请检查数据库账户信息。","test.databaseNotFoundDetail":"指定的数据库不存在，请检查数据库名称。","test.permissionDetail":"数据库账户没有连接或访问该数据库所需的权限。","test.poolDetail":"数据库连接资源暂时不足，请稍后重试或检查连接数限制。","test.runtimeDetail":"数据库运行服务暂时不可用，请稍后重试。","page.title":"数据库连接","page.titleService":"数据库连接 · {service}","page.description":"在数据库插件内配置连接、数据库类型、连接池与密码" },
    "en-US": { "form.name":"Connection name","form.provider":"Provider","form.host":"Host","form.port":"Port","form.socketPath":"Unix socket path","form.database":"Database","form.options":"Connection options","form.pool":"Connection pool","form.credential":"Password","form.user":"User","form.tlsMode":"TLS mode","form.connectTimeout":"Connect timeout","form.serverName":"TLS server name","form.applicationName":"PostgreSQL application name","form.network":"MySQL network","form.readTimeout":"MySQL read timeout","form.writeTimeout":"MySQL write timeout","form.rejectReadOnly":"Reject read-only MySQL instances","option.verifyFull":"Verify fully (recommended)","option.tlsDisable":"Disabled (controlled test environments only)","option.networkTcp":"TCP","option.networkUnix":"Unix socket","form.minIdle":"Minimum idle","form.maxIdle":"Maximum idle","form.maxOpen":"Maximum open","form.maxLifetime":"Maximum lifetime","form.maxIdleTime":"Maximum idle time","form.acquireTimeout":"Acquire timeout","form.idlePoolTtl":"Idle-pool TTL","section.identity":"Connection identity","section.options":"Connection options","section.pool":"Pool policy","form.createTitle":"Create database connection","form.editTitle":"Edit database connection","form.description":"The database plugin stores connection and pool policy. The plaintext password is used only for this connection request.","error.credentialRequired":"A password is required for a new connection","error.nameRequired":"Connection name is required","error.userRequired":"User is required","error.hostInvalid":"Enter a valid host without a port","error.portInvalid":"Port must be an integer from 1 to 65535","error.socketPathInvalid":"Enter an absolute Unix socket path starting with /","notice.saved":"Database connection saved","filter.name":"Connection name","filter.provider":"Provider","column.name":"Name","column.provider":"Provider","column.endpoint":"Endpoint","column.runtime":"Runtime","column.credential":"Password status","column.credentialVersion":"Password version","runtime.ready":"Ready","runtime.pending":"Pending publication","credential.managed":"Set","credential.missing":"Not set","action.create":"Create connection","action.edit":"Edit","action.save":"Save","action.test":"Test connection","action.delete":"Delete","confirm.delete":"Delete this connection and remove its password?","test.success":"Connection test succeeded","test.successDetail":"{provider} responded in {latency} ms","test.failure":"Connection test failed","test.failureDetail":"Check the host, port, database, user, password, and transport encryption settings, then try again.","test.invalidDetail":"The connection parameters are invalid. Check the host, port, user, and TLS settings.","test.tlsPolicyDetail":"The deployment policy does not allow TLS verification to be disabled. Enable full verification or contact the platform administrator.","test.dnsDetail":"The database host could not be resolved. Check the host name and DNS configuration.","test.refusedDetail":"The database server refused the connection. Check the host, port, and listener status.","test.timeoutDetail":"The database connection timed out. Check the network, firewall, and timeout setting.","test.tlsDetail":"TLS or certificate verification failed. Check the trust chain and TLS server name.","test.authenticationDetail":"The database user name or password was rejected.","test.databaseNotFoundDetail":"The specified database does not exist.","test.permissionDetail":"The database account does not have the required connection or database permissions.","test.poolDetail":"Database connection capacity is temporarily exhausted. Retry later or check connection limits.","test.runtimeDetail":"The database runtime is temporarily unavailable. Try again later.","page.title":"Database connections","page.titleService":"Database connections · {service}","page.description":"Configure providers, pools and passwords in the database plugin" }
  } },
};
