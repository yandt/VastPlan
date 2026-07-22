import type {
  APIExposureDraftRequest,
  APIExposureRevision,
  DataPlaneExposureDraftRequest,
  DataPlaneExposureRevision,
} from "@vastplan/platform-admin";
import {
  jsonSchemaDialect,
  type FormSchema,
} from "@vastplan/workbench-sdk";

export type APIExposureRow = APIExposureRevision & Record<string, unknown>;
export type DataPlaneExposureRow = DataPlaneExposureRevision & Record<string, unknown>;

export const apiExposureFormSchema: FormSchema = {
  id: "api-exposure.v1",
  schema: {
    $schema: jsonSchemaDialect,
    type: "object",
    additionalProperties: false,
    required: [
      "displayName",
      "pluginId",
      "artifactSha256",
      "contributionId",
      "hosts",
      "authenticationProfileId",
      "allowAnonymous",
      "requiredPermissions",
      "logicalService",
      "routingDomain",
    ],
    properties: {
      displayName: { type: "string", title: "名称", minLength: 1, maxLength: 160 },
      pluginId: { type: "string", title: "实现插件 ID", minLength: 3 },
      artifactSha256: { type: "string", title: "制品 SHA-256", pattern: "^[a-f0-9]{64}$" },
      contributionId: { type: "string", title: "Contract 贡献 ID", pattern: "^[a-z][a-z0-9-]{0,63}$" },
      portalId: { type: "string", title: "Portal ID" },
      hosts: { type: "array", title: "绑定 Host", minItems: 1, uniqueItems: true, items: { type: "string" } },
      authenticationProfileId: { type: "string", title: "认证 Profile", minLength: 1 },
      allowAnonymous: { type: "boolean", title: "允许匿名访问", default: false },
      requiredPermissions: { type: "array", title: "所需权限", uniqueItems: true, items: { type: "string" } },
      maxBodyBytes: { type: "integer", title: "最大请求字节", minimum: 1, default: 1_048_576 },
      maxResponseBytes: { type: "integer", title: "最大响应字节", minimum: 1, default: 4_194_304 },
      requestsPerMinute: { type: "integer", title: "每分钟请求", minimum: 1, default: 60 },
      timeoutMs: { type: "integer", title: "超时毫秒", minimum: 100, default: 10_000 },
      logicalService: { type: "string", title: "逻辑服务", minLength: 1 },
      routingDomain: { type: "string", title: "路由域", minLength: 1 },
    },
  },
  uiSchema: {
    pluginId: { "ui:help": "仅用于可信管理面选择已验签 Contract，不会进入公开 URL" },
    artifactSha256: { "ui:help": "必须精确匹配 Contract Catalog 中的已验签制品" },
    hosts: { "ui:help": "公开请求 Host 必须命中此列表" },
  },
};

export function toDraftRequest(value: Record<string, unknown>): APIExposureDraftRequest {
  const portalId = optionalString(value.portalId);
  return {
    contract: {
      pluginId: stringValue(value.pluginId),
      artifactSha256: stringValue(value.artifactSha256),
      contributionId: stringValue(value.contributionId),
    },
    input: {
      displayName: stringValue(value.displayName),
      ...(portalId === undefined ? {} : { portalId }),
      hosts: stringArray(value.hosts),
      authentication: { profileId: stringValue(value.authenticationProfileId), allowAnonymous: value.allowAnonymous === true },
      requiredPermissions: stringArray(value.requiredPermissions),
      limits: {
        maxBodyBytes: integer(value.maxBodyBytes, 1_048_576),
        maxResponseBytes: integer(value.maxResponseBytes, 4_194_304),
        requestsPerMinute: integer(value.requestsPerMinute, 60),
        timeoutMs: integer(value.timeoutMs, 10_000),
      },
      target: {
        logicalService: stringValue(value.logicalService),
        routingDomain: stringValue(value.routingDomain),
      },
    },
  };
}

export const dataPlaneExposureFormSchema: FormSchema = {
  id: "data-plane-exposure.v1",
  schema: {
    $schema: jsonSchemaDialect,
    type: "object",
    additionalProperties: false,
    required: ["pluginId", "artifactSha256", "contributionId", "hosts", "allowedModes", "allowedEndpointOrigins", "tlsIdentityPrefix", "authenticationProfileId", "allowAnonymous", "requiredPermissions", "maxObjectBytes"],
    properties: {
      pluginId: { type: "string", title: "服务插件 ID", minLength: 3 },
      artifactSha256: { type: "string", title: "制品 SHA-256", pattern: "^[a-f0-9]{64}$" },
      contributionId: { type: "string", title: "Data Plane 贡献 ID", pattern: "^[a-z][a-z0-9-]{0,63}$" },
      hosts: { type: "array", title: "绑定 Host", minItems: 1, uniqueItems: true, items: { type: "string" } },
      allowedModes: { type: "array", title: "允许模式", minItems: 1, uniqueItems: true, items: { type: "string", enum: ["gateway-proxy", "ticket-redirect", "private-direct"] } },
      allowedEndpointOrigins: { type: "array", title: "允许的 Endpoint Origins", minItems: 1, uniqueItems: true, items: { type: "string", pattern: "^https://" } },
      tlsIdentityPrefix: { type: "string", title: "SPIFFE 身份前缀", pattern: "^spiffe://.+/$" },
      authenticationProfileId: { type: "string", title: "认证 Profile", minLength: 1 },
      allowAnonymous: { type: "boolean", title: "允许匿名访问", default: false },
      requiredPermissions: { type: "array", title: "所需权限", uniqueItems: true, items: { type: "string" } },
      maxObjectBytes: { type: "integer", title: "最大对象字节", minimum: 1, maximum: 1_099_511_627_776, default: 268_435_456 },
    },
  },
  uiSchema: {
    pluginId: { "ui:help": "只在可信管理面选择已验签 Data Plane Service，不进入公开地址" },
    allowedModes: { "ui:help": "公开大对象优先 ticket-redirect；private-direct 只供受信服务网格" },
  },
};

export function toDataPlaneDraftRequest(value: Record<string, unknown>): DataPlaneExposureDraftRequest {
  return {
    input: {
      hosts: stringArray(value.hosts),
      service: {
        pluginId: stringValue(value.pluginId),
        artifactSha256: stringValue(value.artifactSha256),
        contributionId: stringValue(value.contributionId),
      },
      allowedModes: stringArray(value.allowedModes) as DataPlaneExposureDraftRequest["input"]["allowedModes"],
      allowedEndpointOrigins: stringArray(value.allowedEndpointOrigins),
      tlsIdentityPrefix: stringValue(value.tlsIdentityPrefix),
      authentication: { profileId: stringValue(value.authenticationProfileId), allowAnonymous: value.allowAnonymous === true },
      requiredPermissions: stringArray(value.requiredPermissions),
      maxObjectBytes: integer(value.maxObjectBytes, 268_435_456),
    },
  };
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function optionalString(value: unknown): string | undefined {
  const result = stringValue(value).trim();
  return result === "" ? undefined : result;
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string" && item !== "")
    : [];
}

function integer(value: unknown, fallback: number): number {
  return typeof value === "number" && Number.isSafeInteger(value) ? value : fallback;
}
