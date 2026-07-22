import type { AuthorizationBindingRevision, PlatformAdminClient } from "@vastplan/platform-admin";
import {
  defineCollectionPage,
  jsonSchemaDialect,
  message,
  type CollectionPageDefinition,
  type FormSchema,
  type WorkbenchFormDefinition,
} from "@vastplan/workbench-sdk";
import { namespace, page } from "../model.js";

type BindingRow = AuthorizationBindingRevision & {
  subjectDisplay: string;
  roleDisplay: string;
} & Record<string, unknown>;

const lifecycleLabels = {
  Draft: message(namespace, "status.draft", "草稿"),
  PendingApproval: message(namespace, "status.pending", "待审批"),
  Approved: message(namespace, "status.approved", "已批准"),
  Published: message(namespace, "status.published", "已发布"),
  Retired: message(namespace, "status.retired", "已退役"),
};

export function bindingsPage(client: PlatformAdminClient): CollectionPageDefinition<BindingRow> {
  return defineCollectionPage<BindingRow>({
    id: "platform.authorization.bindings",
    path: "/settings/authorization/bindings",
    title: message(namespace, "bindings.title", "主体绑定"),
    description: message(namespace, "bindings.description", "把用户或外部目录组绑定到精确 Role revision，并设置有效期。"),
    requiredPermissions: ["platform.authorization.catalog"],
    requiredAnyPermissions: ["platform.authorization.binding", "platform.authorization.approve", "platform.authorization.publish", "platform.authorization.revoke"],
    navigation: {
      id: "platform.authorization.bindings",
      label: message(namespace, "bindings.navigation", "主体绑定"),
      zone: "settings",
      groupID: "platform.authorization",
      order: 30,
    },
    collection: {
      id: "authorization-bindings",
      title: message(namespace, "bindings.title", "主体绑定"),
      view: "table",
      query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20, 50] },
      filters: [{ id: "search", label: message(namespace, "filter.binding", "主体、角色或 Binding ID"), kind: "text" }],
      columns: [
        { key: "id", label: "Binding ID", defaultVisible: true, minWidth: 220 },
        { key: "revision", label: "Revision", format: "number", defaultVisible: true },
        { key: "subjectDisplay", label: message(namespace, "column.subject", "主体"), defaultVisible: true, minWidth: 240 },
        { key: "roleDisplay", label: message(namespace, "column.role", "角色"), defaultVisible: true, minWidth: 220 },
        { key: "state", label: message(namespace, "column.status", "状态"), format: "status", valueLabels: lifecycleLabels, statusTones: { Draft: "neutral", PendingApproval: "warning", Approved: "info", Published: "success", Retired: "neutral" }, defaultVisible: true },
        { key: "expiresAt", label: message(namespace, "column.expires", "到期时间"), format: "datetime", defaultVisible: true },
        { key: "updatedAt", label: message(namespace, "column.updated", "更新时间"), format: "datetime", defaultVisible: true },
      ],
      selection: "single",
      preferences: { allowedColumns: ["id", "revision", "subjectDisplay", "roleDisplay", "state", "expiresAt", "updatedAt"], density: true },
      actions: [
        { id: "create", label: message(namespace, "action.createBinding", "新建绑定"), icon: "add", placement: "page.primary", tone: "primary", form: "create", requiredPermissions: ["platform.authorization.binding"] },
        { id: "edit", label: message(namespace, "action.edit", "编辑"), placement: "record.row", form: "edit", requiredPermissions: ["platform.authorization.binding"], visibleWhen: { pointer: "/state", equals: "Draft" } },
        { id: "submit", label: message(namespace, "action.submit", "提交审批"), placement: "record.row", requiredPermissions: ["platform.authorization.binding"], visibleWhen: { pointer: "/state", equals: "Draft" } },
        { id: "approve", label: message(namespace, "action.approve", "批准"), placement: "record.row", requiredPermissions: ["platform.authorization.approve"], visibleWhen: { pointer: "/state", equals: "PendingApproval" } },
        { id: "publish", label: message(namespace, "action.publish", "发布"), placement: "record.row", requiredPermissions: ["platform.authorization.publish"], visibleWhen: { pointer: "/state", equals: "Approved" } },
        { id: "revoke", label: message(namespace, "action.revoke", "即时撤权"), placement: "record.row", tone: "danger", requiredPermissions: ["platform.authorization.revoke"], confirm: message(namespace, "confirm.revokeBinding", "立即撤销该 Binding？"), visibleWhen: { pointer: "/state", equals: "Published" } },
        { id: "retire", label: message(namespace, "action.retire", "退役"), placement: "record.row", tone: "danger", requiredPermissions: ["platform.authorization.binding"], visibleWhen: { pointer: "/state", equals: "Published" } },
      ],
    },
    forms: [bindingForm(client, "create"), bindingForm(client, "edit")],
    async load(query, signal) {
      const state = await client.getAuthorizationPolicy();
      if (signal.aborted) return { items: [], total: 0 };
      const rows = state.bindings.map((item) => ({
        ...item,
        subjectDisplay: `${item.subject.kind}:${item.subject.issuer ?? ""}:${item.subject.id}`,
        roleDisplay: `${item.roleId}@${item.roleRevision}`,
      } as BindingRow));
      return page(rows, query, (row, text) => row.id.toLowerCase().includes(text) || row.subjectDisplay.toLowerCase().includes(text) || row.roleDisplay.toLowerCase().includes(text));
    },
    async runAction({ action, selected }) {
      const row = selected[0];
      if (row === undefined) return;
      const state = await client.getAuthorizationPolicy();
      if (action.id === "revoke") {
        await client.revokeAuthorization({
          expectedGeneration: state.generation,
          id: `revoke.${row.id}.${Date.now()}`,
          kind: "binding",
          targetId: row.id,
          effectiveAt: new Date().toISOString(),
          reasonCode: "administrator_revocation",
        });
      } else if (["submit", "approve", "publish", "retire"].includes(action.id)) {
        await client.transitionAuthorizationBinding(row.id, row.revision, action.id as "submit" | "approve" | "publish" | "retire", state.generation);
      }
      return { notify: { title: action.label, kind: "success" } };
    },
  });
}

function bindingForm(client: PlatformAdminClient, mode: "create" | "edit"): WorkbenchFormDefinition<BindingRow> {
  const base = bindingFormSchema();
  return {
    id: mode,
    schema: base,
    presentation: {
      layout: "vertical",
      sections: [{ id: "binding", title: message(namespace, "form.bindingSection", "授权绑定"), columns: 2, fields: ["/id", "/subjectKind", "/subjectId", "/issuer", "/roleKey", "/notBefore", "/expiresAt"] }],
    },
    workflow: {
      surface: "drawer", size: "lg", title: message(namespace, mode === "create" ? "action.createBinding" : "action.editBinding", mode === "create" ? "新建绑定" : "编辑绑定"), submitLabel: message(namespace, "action.save", "保存"),
      success: { notify: message(namespace, "notice.bindingSaved", "绑定已保存"), refreshCollection: true, close: true },
    },
    async prepare() {
      const state = await client.getAuthorizationPolicy();
      const roles = state.roles.filter((item) => item.state === "Published");
      const root = base.schema as Record<string, unknown>;
      const properties = root.properties as Record<string, unknown>;
			const schema = {
				...base,
				schema: { ...root, properties: { ...properties, roleKey: { type: "string", title: "Role revision", oneOf: roles.map((role) => ({ const: `${role.id}@${role.revision}`, title: `${role.title} · ${role.id}@${role.revision}` })) } } } as FormSchema["schema"],
			};
      return {
        schema,
        initialValue: { subjectKind: "user", issuer: "vastplan.authentication", notBefore: new Date().toISOString(), expiresAt: new Date(Date.now() + 30 * 24 * 60 * 60 * 1000).toISOString() },
      };
    },
    async load(selected) {
			const row = selected[0];
			if (row === undefined) return {};
			return { id: row.id, subjectKind: row.subject.kind, subjectId: row.subject.id, issuer: row.subject.issuer ?? "", roleKey: `${row.roleId}@${row.roleRevision}`, notBefore: row.notBefore, expiresAt: row.expiresAt };
		},
    async submit({ value, selected }) {
      const roleKey = String(value.roleKey ?? "");
      const separator = roleKey.lastIndexOf("@");
      if (separator < 1) return { fieldErrors: { roleKey: message(namespace, "error.roleRequired", "请选择 Role revision") } };
      const state = await client.getAuthorizationPolicy();
			const subjectKind: "user" | "group" = value.subjectKind === "group" ? "group" : "user";
      const request = {
        expectedGeneration: state.generation,
        domainId: "platform.root",
        subject: { kind: subjectKind, id: String(value.subjectId ?? ""), issuer: String(value.issuer ?? "") },
        roleId: roleKey.slice(0, separator),
        roleRevision: Number(roleKey.slice(separator + 1)),
        notBefore: String(value.notBefore ?? ""),
        expiresAt: String(value.expiresAt ?? ""),
      };
			if (mode === "create") {
				await client.createAuthorizationBinding({ ...request, id: String(value.id ?? "") });
			} else {
				const row = selected[0];
				if (row !== undefined) await client.updateAuthorizationBinding(row.id, row.revision, request);
			}
    },
  };
}

function bindingFormSchema(): FormSchema {
  return {
    id: "authorization-binding.v1",
    schema: {
      $schema: jsonSchemaDialect,
      type: "object",
      additionalProperties: false,
      required: ["id", "subjectKind", "subjectId", "issuer", "roleKey", "notBefore", "expiresAt"],
      properties: {
        id: { type: "string", title: "Binding ID", minLength: 1, maxLength: 160 },
        subjectKind: { type: "string", title: "主体类型", oneOf: [{ const: "user", title: "用户" }, { const: "group", title: "外部组" }] },
        subjectId: { type: "string", title: "主体 ID", minLength: 1, maxLength: 160 },
        issuer: { type: "string", title: "Issuer", minLength: 1, maxLength: 512 },
        roleKey: { type: "string", title: "Role revision" },
        notBefore: { type: "string", format: "date-time", title: "生效时间" },
        expiresAt: { type: "string", format: "date-time", title: "到期时间" },
      },
    },
    uiSchema: { subjectKind: { "ui:widget": "select" }, roleKey: { "ui:widget": "select" } },
    localization: {
      "/properties/id/title": message(namespace, "form.bindingId", "Binding ID"),
      "/properties/subjectKind/title": message(namespace, "form.subjectKind", "主体类型"),
      "/properties/subjectId/title": message(namespace, "form.subjectId", "主体 ID"),
      "/properties/issuer/title": message(namespace, "form.issuer", "Issuer"),
      "/properties/roleKey/title": message(namespace, "form.role", "Role revision"),
      "/properties/notBefore/title": message(namespace, "form.notBefore", "生效时间"),
      "/properties/expiresAt/title": message(namespace, "form.expiresAt", "到期时间"),
    },
  };
}
