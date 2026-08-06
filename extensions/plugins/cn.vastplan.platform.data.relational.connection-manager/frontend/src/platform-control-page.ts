import {
  type PlatformAdminClient,
  PlatformAdminError,
  type PlatformControlChangeRequest,
  type PlatformControlStatus,
} from "@vastplan/platform-admin";
import {
  defineFormPage,
  jsonSchemaDialect,
  message,
  type FormPageDefinition,
  type FormSchema,
  type WorkbenchFormFieldErrors,
} from "@vastplan/workbench-sdk";
import { connectionEndpoint, connectionEndpointFields, defaultConnectionPort } from "./connection-endpoint.js";

const namespace = "cn.vastplan.platform.data.relational.connection-manager";
const defaultPlatformControlDatabase = "vastplan";
const defaultPlatformControlSchema = "platform";

type PlatformControlForm = Record<string, unknown> & {
  phase?: string;
  currentGeneration?: number;
  providerId?: string;
  host?: string;
  port?: number;
  database?: string;
  createDatabaseIfMissing?: boolean;
  schema?: string;
  username?: string;
  tlsMode?: string;
  serverName?: string;
  secretMode?: string;
  password?: string;
  externalKind?: string;
  externalName?: string;
  externalPath?: string;
  contractRange?: string;
};

const schema: FormSchema = {
  id: "platform-control-database.v1",
  schema: {
    $schema: jsonSchemaDialect,
    type: "object",
    additionalProperties: false,
    required: ["providerId", "host", "port", "database", "schema", "username", "tlsMode", "secretMode", "contractRange"],
    properties: {
      phase: { type: "string", title: "当前状态", readOnly: true },
      currentGeneration: { type: "integer", title: "当前配置代", minimum: 0, readOnly: true },
      providerId: { type: "string", title: "数据库类型", oneOf: [{ const: "postgresql", title: "PostgreSQL" }, { const: "mysql", title: "MySQL" }] },
      host: { type: "string", title: "地址", minLength: 1, maxLength: 253 },
      port: { type: "integer", title: "端口", minimum: 1, maximum: 65535 },
      database: { type: "string", title: "数据库", minLength: 1, maxLength: 63 },
      createDatabaseIfMissing: { type: "boolean", title: "数据库不存在时创建", default: true },
      schema: { type: "string", title: "专用 Schema", minLength: 1, maxLength: 63 },
      username: { type: "string", title: "用户名", minLength: 1, maxLength: 128 },
      tlsMode: { type: "string", title: "传输加密模式", oneOf: [{ const: "verify-full", title: "完整校验（推荐）" }, { const: "verify-ca", title: "校验证书机构" }, { const: "disable", title: "关闭（仅受控测试环境）" }] },
      serverName: { type: "string", title: "证书校验服务器名称", maxLength: 253 },
      secretMode: { type: "string", title: "密码提供方式", oneOf: [{ const: "direct", title: "直接输入密码（推荐）" }, { const: "external", title: "外部密钥引用（高级）" }] },
      password: { type: "string", title: "密码", format: "vastplan-secret-material", writeOnly: true, maxLength: 65536 },
      externalKind: { type: "string", title: "外部密钥类型", oneOf: [{ const: "systemd-credential", title: "systemd Credential（生产推荐）" }, { const: "owner-file", title: "受保护文件" }] },
      externalName: { type: "string", title: "Credential 名称", minLength: 1, maxLength: 128 },
      externalPath: { type: "string", title: "密码文件绝对路径", minLength: 1, maxLength: 4096 },
      contractRange: { type: "string", title: "Shared State 契约范围", minLength: 1, maxLength: 64 },
    },
  },
  uiSchema: {
    username: { "ui:options": { autocomplete: "new-password" } },
    password: { "ui:options": { autocomplete: "new-password" } },
    externalName: { "ui:options": { autocomplete: "off" } },
    externalPath: { "ui:options": { autocomplete: "off" } },
  },
};

export function createPlatformControlPage(client: PlatformAdminClient, serviceID: string, path: string): FormPageDefinition {
  return defineFormPage({
    id: `platform.control-database.${serviceID}`,
    path,
    title: message(namespace, "platformControl.page.title", "平台控制数据库"),
    description: message(namespace, "platformControl.page.description", "配置种子服务保存必要运行状态的专用数据库；普通应用插件不能使用此连接。"),
    bodyLayout: "large",
    requiredPermissions: ["platform.database.read", "platform.database.write"],
    navigation: {
      id: `platform.control-database.${serviceID}`,
      label: message(namespace, "platformControl.page.title", "平台控制数据库"),
      parentMenuRef: { pluginID: namespace, nodeID: "databases" },
      order: 10,
    },
    form: {
      id: `platform.control-database.form.${serviceID}`,
      schema,
      size: "md",
      presentation: {
        layout: "horizontal",
        labelPlacement: "inline",
        navigation: "sections",
        sections: [
          { id: "status", title: message(namespace, "platformControl.section.status", "运行状态"), columns: 2, fields: ["/phase", "/currentGeneration"] },
          { id: "database", title: message(namespace, "platformControl.section.database", "数据库连接"), columns: 2, fields: ["/providerId", "/host", "/port", "/database", "/createDatabaseIfMissing", "/schema", "/username"] },
          { id: "security", title: message(namespace, "platformControl.section.security", "传输与密码"), columns: 2, fields: ["/tlsMode", "/serverName", "/secretMode", "/password", "/externalKind", "/externalName", "/externalPath"] },
          { id: "contract", title: message(namespace, "platformControl.section.contract", "能力契约"), columns: 1, fields: ["/contractRange"] },
        ],
        fields: [
          { pointer: "/phase", readOnlyWhen: { pointer: "/phase", exists: true } },
          { pointer: "/currentGeneration", readOnlyWhen: { pointer: "/currentGeneration", exists: true } },
          { pointer: "/createDatabaseIfMissing", visibleWhen: { pointer: "/currentGeneration", equals: 0 } },
          { pointer: "/schema", visibleWhen: { pointer: "/providerId", equals: "postgresql" } },
          { pointer: "/serverName", visibleWhen: { pointer: "/tlsMode", equals: "verify-full" } },
          { pointer: "/password", widget: "secretMaterial", visibleWhen: { pointer: "/secretMode", equals: "direct" } },
          { pointer: "/externalKind", visibleWhen: { pointer: "/secretMode", equals: "external" } },
          { pointer: "/externalName", visibleWhen: { all: [{ pointer: "/secretMode", equals: "external" }, { pointer: "/externalKind", equals: "systemd-credential" }] } },
          { pointer: "/externalPath", visibleWhen: { all: [{ pointer: "/secretMode", equals: "external" }, { pointer: "/externalKind", equals: "owner-file" }] } },
        ],
      },
      workflow: {
        surface: "page",
        title: message(namespace, "platformControl.form.title", "平台控制数据库"),
        description: message(namespace, "platformControl.form.description", "密码仅用于本次测试与初始化；可信宿主会自动生成受保护的密码引用，页面不会保存或回填明文。"),
        submitLabel: message(namespace, "platformControl.action.initialize", "初始化并启用"),
        actions: [{ id: "test", label: message(namespace, "platformControl.action.test", "测试连接"), icon: "success", placement: "footer.start", requiresValid: true }],
        success: { notify: message(namespace, "platformControl.notice.ready", "平台控制数据库已初始化并启用") },
      },
      async load() {
        return statusToForm(await client.platformControlStatus());
      },
      async validate({ value }): Promise<WorkbenchFormFieldErrors> {
        return validatePlatformControlForm(value as PlatformControlForm);
      },
      async runAction({ action, value }) {
        if (action.id !== "test") return;
        try {
          await client.testPlatformControl(toChangeRequest(value as PlatformControlForm));
        } catch (error) {
          if (error instanceof PlatformAdminError && error.validation !== undefined) return { fieldErrors: platformControlValidationErrors(error.validation) };
          if (error instanceof PlatformAdminError && error.code === "database_not_found" && (value as PlatformControlForm).createDatabaseIfMissing === true) {
            return { notify: { title: message(namespace, "platformControl.notice.databaseWillBeCreated", "目标数据库尚不存在；初始化并启用时将自动创建"), kind: "warning" } };
          }
          throw error;
        }
        return { notify: { title: message(namespace, "platformControl.notice.testSucceeded", "平台控制数据库连接测试成功"), kind: "success" } };
      },
      async submit({ value }) {
        try {
          await client.configurePlatformControl(toChangeRequest(value as PlatformControlForm));
        } catch (error) {
          if (error instanceof PlatformAdminError && error.validation !== undefined) return { fieldErrors: platformControlValidationErrors(error.validation) };
          throw error;
        }
      },
    },
  });
}

function statusToForm(status: PlatformControlStatus): PlatformControlForm {
  const profile = status.profile;
  if (profile === undefined) {
    return {
      phase: status.phase,
      currentGeneration: status.generation ?? 0,
      providerId: "postgresql",
      port: defaultConnectionPort("postgresql"),
      database: defaultPlatformControlDatabase,
      createDatabaseIfMissing: true,
      schema: defaultPlatformControlSchema,
      tlsMode: "verify-full",
      secretMode: "direct",
      contractRange: "^1.0.0",
    };
  }
  const endpoint = connectionEndpointFields(profile.connection.endpoint, profile.connection.providerId);
  const options = profile.connection.options;
  return {
    phase: status.phase,
    currentGeneration: status.generation ?? profile.generation,
    providerId: profile.connection.providerId,
    host: endpoint.host,
    port: endpoint.port,
    database: profile.connection.database,
    createDatabaseIfMissing: false,
    schema: profile.schema,
    username: text(options.user),
    tlsMode: text(options.tlsMode),
    serverName: text(options.serverName),
    secretMode: "external",
    externalKind: profile.secretRef.kind,
    externalName: profile.secretRef.kind === "systemd-credential" ? profile.secretRef.name : undefined,
    externalPath: profile.secretRef.kind === "owner-file" ? profile.secretRef.path : undefined,
    contractRange: profile.contractRange,
  };
}

function toChangeRequest(value: PlatformControlForm): PlatformControlChangeRequest {
  const expectedGeneration = number(value.currentGeneration) ?? 0;
  const endpoint = connectionEndpoint(value.host, value.port);
  if (endpoint === undefined) throw new Error("平台控制数据库地址或端口无效");
  const providerValue = text(value.providerId);
  const database = text(value.database);
  const schemaName = providerValue === "mysql" ? database : text(value.schema);
  const username = text(value.username);
  const tlsMode = text(value.tlsMode) as "disable" | "verify-ca" | "verify-full" | undefined;
  const contractRange = text(value.contractRange);
  const secretMode = text(value.secretMode);
  if ((providerValue !== "postgresql" && providerValue !== "mysql") || database === undefined || schemaName === undefined || username === undefined || tlsMode === undefined || contractRange === undefined) throw new Error("平台控制数据库配置不完整");
  const providerId = providerValue;
  const externalKind = text(value.externalKind);
  const secretRef = externalKind === "systemd-credential"
    ? { kind: "systemd-credential" as const, name: text(value.externalName) ?? "" }
    : { kind: "owner-file" as const, path: text(value.externalPath) ?? "" };
  return {
    expectedGeneration,
    ...(expectedGeneration === 0 && value.createDatabaseIfMissing === true ? { createDatabaseIfMissing: true } : {}),
    profile: {
      schemaVersion: 1,
      generation: expectedGeneration + 1,
      connection: {
        providerId,
        endpoint,
        database,
        options: {
          user: username,
          tlsMode,
          connectTimeoutMs: 10000,
          ...(tlsMode === "verify-full" ? { serverName: text(value.serverName) ?? "" } : {}),
        },
        pool: { minIdle: 1, maxIdle: 4, maxOpen: 16, maxLifetimeMs: 1800000, maxIdleTimeMs: 300000, acquireTimeoutMs: 10000, idlePoolTtlMs: 600000 },
      },
      schema: schemaName,
      ...(secretMode === "external" ? { secretRef } : {}),
      contractRange,
    },
    ...(secretMode === "direct" ? { secretMaterial: secret(value.password) ?? "" } : {}),
  };
}

function validatePlatformControlForm(value: PlatformControlForm): WorkbenchFormFieldErrors {
  const endpointInvalid = connectionEndpoint(value.host, value.port) === undefined;
  const secretMode = text(value.secretMode);
  const externalKind = text(value.externalKind);
  return {
    ...(endpointInvalid ? { "/host": message(namespace, "error.hostInvalid", "请输入有效地址"), "/port": message(namespace, "error.portInvalid", "端口必须为 1 到 65535 的整数") } : {}),
    ...(text(value.database) === undefined ? { "/database": message(namespace, "platformControl.error.databaseRequired", "数据库不能为空") } : {}),
    ...(value.providerId !== "mysql" && text(value.schema) === undefined ? { "/schema": message(namespace, "platformControl.error.schemaRequired", "专用 Schema 不能为空") } : {}),
    ...(text(value.username) === undefined ? { "/username": message(namespace, "error.userRequired", "用户名不能为空") } : {}),
    ...(value.tlsMode === "verify-full" && text(value.serverName) === undefined ? { "/serverName": message(namespace, "platformControl.error.serverNameRequired", "完整校验需要填写证书校验服务器名称") } : {}),
    ...(secretMode === "direct" && secret(value.password) === undefined ? { "/password": message(namespace, "platformControl.error.passwordRequired", "请输入数据库密码") } : {}),
    ...(secretMode === "external" && externalKind !== "systemd-credential" && externalKind !== "owner-file" ? { "/externalKind": message(namespace, "platformControl.error.externalKindRequired", "请选择外部密钥类型") } : {}),
    ...(secretMode === "external" && externalKind === "systemd-credential" && text(value.externalName) === undefined ? { "/externalName": message(namespace, "platformControl.error.secretNameRequired", "请输入 systemd Credential 名称") } : {}),
    ...(secretMode === "external" && externalKind === "owner-file" && (text(value.externalPath)?.startsWith("/") !== true) ? { "/externalPath": message(namespace, "platformControl.error.secretPathRequired", "请输入受保护密码文件的绝对路径") } : {}),
  };
}

function platformControlValidationErrors(validation: Readonly<{ field: string; reason: string }>): WorkbenchFormFieldErrors {
  const pointer = ({
    connection: "/providerId",
    "connection.pool": "/providerId",
    "connection.options": "/providerId",
    "connection.endpoint": "/host",
    "profile.connection.endpoint": "/host",
    "profile.connection.database": "/database",
    "profile.connection.options.user": "/username",
    "profile.connection.options.tlsMode": "/tlsMode",
    "profile.connection.options.serverName": "/serverName",
    "profile.schema": "/schema",
    "profile.contractRange": "/contractRange",
    "profile.secretRef.kind": "/secretMode",
    "profile.secretRef.name": "/externalName",
    "profile.secretRef.path": "/externalPath",
    secretMaterial: "/password",
  } as Record<string, string>)[validation.field];
  if (pointer === undefined) return {};
  return { [pointer]: message(namespace, `platformControl.validation.${validation.field}.${validation.reason}`, platformControlValidationMessage(validation)) };
}

function platformControlValidationMessage(validation: Readonly<{ field: string; reason: string }>): string {
  if (validation.field.endsWith("endpoint")) return "数据库地址必须包含有效主机和端口";
  if (validation.field.endsWith("database")) return "请输入有效数据库名称";
  if (validation.field.endsWith("user")) return "请输入有效数据库用户名";
  if (validation.field.endsWith("serverName")) return "完整校验模式必须填写证书校验服务器名称";
  if (validation.field === "profile.schema") return "请输入有效的专用 Schema 名称";
  if (validation.field === "secretMaterial") return "密码格式无效，请重新输入";
  return "此字段配置无效，请检查后重试";
}

function text(value: unknown): string | undefined { return typeof value === "string" && value.trim() !== "" ? value.trim() : undefined; }
function secret(value: unknown): string | undefined { return typeof value === "string" && value.length > 0 ? value : undefined; }
function number(value: unknown): number | undefined { return typeof value === "number" && Number.isSafeInteger(value) && value >= 0 ? value : undefined; }
