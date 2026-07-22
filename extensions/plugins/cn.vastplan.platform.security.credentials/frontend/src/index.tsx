import { createBrowserPlatformAdminClient, type CredentialMetadata, type PlatformAdminClient } from "@vastplan/platform-admin";
import {
  defineCollectionPage,
  jsonSchemaDialect,
  managementServicesFor,
  message,
  type CollectionPageDefinition,
  type CollectionQuery,
  type WorkbenchFormDefinition,
  type WorkbenchFrontendPluginContext,
} from "@vastplan/workbench-sdk";

const namespace = "cn.vastplan.platform.security.credentials";

const schema = {
  id: "platform-credential.v2",
  schema: {
    $schema: jsonSchemaDialect,
    type: "object",
    additionalProperties: false,
    required: ["name", "value"],
    properties: {
      name: { type: "string", title: "凭证名称", minLength: 1, maxLength: 160 },
      value: { type: "string", title: "凭证明文", format: "vastplan-secret-material", writeOnly: true, minLength: 1 },
    },
  },
  localization: {
    "/properties/name/title": message(namespace, "form.name", "凭证名称"),
    "/properties/value/title": message(namespace, "form.value", "凭证明文"),
  },
} as const;

type CredentialRow = CredentialMetadata & { status: "available" | "revoked" } & Record<string, unknown>;

export function createCredentialsPage(client: PlatformAdminClient, serviceID: string, path: string, title: ReturnType<typeof message>): CollectionPageDefinition<CredentialRow> {
  const saveForm: WorkbenchFormDefinition<CredentialRow> = {
    id: "save",
    schema,
    presentation: {
      layout: "vertical",
      navigation: "sections",
      sections: [{ id: "credential", title: message(namespace, "form.section", "保存或替换凭证"), columns: 1, fields: ["/name", "/value"] }],
      fields: [
        { pointer: "/name", widget: "text" },
        { pointer: "/value", widget: "secretMaterial", help: message(namespace, "form.valueHelp", "明文仅用于本次 TLS 请求；无论成功或失败，提交后都必须重新输入。") },
      ],
    },
    workflow: {
      surface: "drawer",
      title: message(namespace, "form.title", "安全保存凭证"),
      description: message(namespace, "form.description", "列表和读取 API 永远不会返回明文或密文。使用同名凭证会创建新版本。"),
      size: "md",
      submitLabel: message(namespace, "action.save", "安全保存"),
      confirmBeforeSubmit: message(namespace, "confirm.save", "确认通过本次 TLS 请求提交凭证明文？提交完成后输入会立即清除。"),
      success: { notify: message(namespace, "notice.saved", "凭证已安全保存"), refreshCollection: true, close: true },
    },
    async submit({ value }) {
      if (typeof value.name !== "string" || value.name === "" || typeof value.value !== "string" || value.value === "") return {
        fieldErrors: {
          name: message(namespace, "error.nameRequired", "凭证名称不能为空"),
          value: message(namespace, "error.valueRequired", "凭证明文不能为空"),
        },
      };
      await client.putCredential(value.name, value.value);
    },
  };

  return defineCollectionPage<CredentialRow>({
    id: `platform.credentials.${serviceID}`,
    path,
    title,
    description: message(namespace, "page.description", "管理不返回明文的凭证元数据"),
    navigation: { id: `platform.credentials.${serviceID}`, label: title, zone: "settings", order: 30 },
    collection: {
      id: `platform.credentials.${serviceID}`,
      title,
      view: "table",
      query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20, 50, 100] },
      filters: [{ id: "name", label: message(namespace, "filter.name", "凭证名称前缀"), kind: "text", sensitive: false }],
      columns: [
        { key: "name", label: message(namespace, "column.name", "名称"), defaultVisible: true, minWidth: 200 },
        { key: "version", label: message(namespace, "column.version", "版本"), format: "number", defaultVisible: true, minWidth: 80 },
        { key: "keyVersion", label: message(namespace, "column.keyVersion", "包裹密钥"), defaultVisible: true, minWidth: 140 },
        { key: "status", label: message(namespace, "column.status", "状态"), format: "status", valueLabels: { available: message(namespace,"status.available","可用"), revoked: message(namespace,"status.revoked","已撤销") }, statusTones: { available: "success", revoked: "error" }, defaultVisible: true, minWidth: 100 },
        { key: "updatedAt", label: message(namespace, "column.updatedAt", "更新时间"), format: "datetime", defaultVisible: true, minWidth: 180 },
      ],
      actions: [
        { id: "save", label: message(namespace, "action.create", "保存或替换凭证"), icon: "add", placement: "page.primary", tone: "primary", form: "save" },
        { id: "rotate", label: message(namespace, "action.rotate", "轮换包裹密钥"), placement: "record.row", confirm: message(namespace, "confirm.rotate", "确认轮换此凭证的包裹密钥？") },
        { id: "revoke", label: message(namespace, "action.revoke", "撤销"), placement: "record.row", tone: "danger", confirm: message(namespace, "confirm.revoke", "确认撤销此凭证？撤销后依赖它的服务将无法取得材料租约。") },
      ],
    },
    forms: [saveForm],
    async load(query: CollectionQuery, signal) {
      const prefix = typeof query.filters.name === "string" ? query.filters.name.trim() : "";
      const values = await client.listCredentials(prefix);
      if (signal.aborted) return { items: [], total: 0 };
      const rows = values.map((item) => ({ ...item, status: item.revoked ? "revoked" : "available" }) as CredentialRow);
      const start = Math.max(0, (query.page - 1) * query.pageSize);
      return { items: rows.slice(start, start + query.pageSize), total: rows.length };
    },
    async runAction({ action, selected }) {
      const item = selected[0];
      if (item === undefined) return;
      if (action.id === "rotate") await client.rotateCredential(item.name);
      if (action.id === "revoke") await client.revokeCredential(item.name);
      return { notify: { title: action.label, kind: "success" } };
    },
  });
}

export default {
  register(context: WorkbenchFrontendPluginContext) {
    const services = managementServicesFor(context.portal, "platform.credentials");
    if (services.length === 0) throw new Error("Portal 未绑定 platform.credentials 服务");
    for (const service of services) {
      const client = createBrowserPlatformAdminClient(context.portal.id, service.id);
      const suffix = services.length === 1 ? "" : `/${service.id}`;
      const title = context.i18n.message(services.length === 1 ? "page.title" : "page.titleService", services.length === 1 ? "凭证引用" : "凭证引用 · {service}", { service: service.label ?? service.id });
      context.addCollectionPage(createCredentialsPage(client, service.id, `/settings/credentials${suffix}`, title));
    }
  },
  localization: { defaultLocale: "zh-CN", messages: {
    "zh-CN": { "form.name":"凭证名称","form.value":"凭证明文","form.section":"保存或替换凭证","form.valueHelp":"明文仅用于本次 TLS 请求；无论成功或失败，提交后都必须重新输入。","form.title":"安全保存凭证","form.description":"列表和读取 API 永远不会返回明文或密文。使用同名凭证会创建新版本。","confirm.save":"确认通过本次 TLS 请求提交凭证明文？提交完成后输入会立即清除。","notice.saved":"凭证已安全保存","error.nameRequired":"凭证名称不能为空","error.valueRequired":"凭证明文不能为空","filter.name":"凭证名称前缀","column.name":"名称","column.version":"版本","column.keyVersion":"包裹密钥","column.status":"状态","column.updatedAt":"更新时间","status.available":"可用","status.revoked":"已撤销","action.create":"保存或替换凭证","action.save":"安全保存","action.rotate":"轮换包裹密钥","action.revoke":"撤销","confirm.rotate":"确认轮换此凭证的包裹密钥？","confirm.revoke":"确认撤销此凭证？撤销后依赖它的服务将无法取得材料租约。","page.title":"凭证引用","page.titleService":"凭证引用 · {service}","page.description":"管理不返回明文的凭证元数据" },
    "en-US": { "form.name":"Credential name","form.value":"Credential plaintext","form.section":"Save or replace credential","form.valueHelp":"Plaintext is used only for this TLS request and must be re-entered after every submit, successful or not.","form.title":"Save credential securely","form.description":"List and read APIs never return plaintext or ciphertext. Reusing a name creates a new version.","confirm.save":"Submit credential plaintext over this TLS request? The input is cleared immediately after submission.","notice.saved":"Credential saved securely","error.nameRequired":"Credential name is required","error.valueRequired":"Credential plaintext is required","filter.name":"Credential name prefix","column.name":"Name","column.version":"Version","column.keyVersion":"Wrapping key","column.status":"Status","column.updatedAt":"Updated","status.available":"Available","status.revoked":"Revoked","action.create":"Save or replace credential","action.save":"Save securely","action.rotate":"Rotate wrapping key","action.revoke":"Revoke","confirm.rotate":"Rotate the wrapping key for this credential?","confirm.revoke":"Revoke this credential? Dependent services will no longer receive material leases.","page.title":"Credential references","page.titleService":"Credential references · {service}","page.description":"Manage credential metadata without exposing plaintext" }
  } },
};
