import { type PortalApprovalEvidenceRequirement, type PortalConfiguration, type PortalControlClient } from "@vastplan/ui-primitives";
import type { PlatformAdminClient } from "@vastplan/platform-admin";
import { jsonSchemaDialect, message, type FormPresentation, type FormSchema, type JSONValue, type WorkbenchFormDefinition, type WorkbenchFormFieldErrors } from "@vastplan/workbench-sdk";
import { buildPortalConfiguration, configurationToForm, type PortalPermissionChoice, portalConfigurationSchema, portalConfigurationSchemaWithPermissions } from "./portal-form";
import type { PortalRow } from "./portal-model";

const namespace = "cn.vastplan.platform.configuration.portal-composer";
export type PortalPermissionCatalogClient = Pick<PlatformAdminClient, "getAuthorizationPolicy">;

export function portalForms(client: PortalControlClient, permissionCatalog: PortalPermissionCatalogClient): WorkbenchFormDefinition<PortalRow>[] {
  return [configurationForm(client, permissionCatalog, "create"), configurationForm(client, permissionCatalog, "edit"), configurationForm(client, permissionCatalog, "new-working-copy"), approvalReviewForm(client), restoreForm(client)];
}

function approvalReviewForm(client: PortalControlClient): WorkbenchFormDefinition<PortalRow> {
  return {
    id: "approval-review",
    schema: approvalEvidencePlaceholder,
    presentation: { layout: "vertical", fields: [] },
    workflow: {
      dialogWidth: "sm", title: "补充审批证据", description: "请按当前审批策略补充证据；服务端会对冻结资源重新求值。",
      submitLabel: "提交审批", success: { notify: "Portal 审批已完成", refreshCollection: true, close: true },
    },
    async prepare(selected) {
      const row = selected[0];
      if (row === undefined || row.portal.pendingPublication === undefined) throw new Error("未选择待审批 Portal");
      const requirements = row.portal.pendingPublication.approval?.requirements ?? [];
      if (requirements.length === 0) throw new Error("审批 Provider 未返回可填写的证据要求");
      const form = approvalEvidenceDefinition(requirements, row.portal.pendingPublication.approval?.profile.revision ?? 1);
      return {
        schema: form.schema,
        presentation: form.presentation,
        initialValue: { expectedDigest: row.portal.pendingPublication.digest, acknowledged: false, reason: "" },
      };
    },
    async submit({ value, selected }) {
      const row = selected[0];
      if (row === undefined) return;
      await client.approvePortalPublication(row.id, row.publicationId, {
        expectedDigest: typeof value.expectedDigest === "string" ? value.expectedDigest : "",
        acknowledged: value.acknowledged === true,
        reason: typeof value.reason === "string" ? value.reason : "",
      });
    },
  };
}

const approvalEvidencePlaceholder: FormSchema = {
  id: "portal-approval-evidence.loading",
  schema: { $schema: jsonSchemaDialect, type: "object", additionalProperties: false, properties: {} },
};

function approvalEvidenceDefinition(requirements: readonly PortalApprovalEvidenceRequirement[], revision: number): { schema: FormSchema; presentation: FormPresentation } {
  const required: string[] = [];
  const properties: Record<string, JSONValue> = {};
  const fields: NonNullable<FormPresentation["fields"]>[number][] = [];
  for (const requirement of requirements) {
    const title = requirement.title || requirement.id;
    if (requirement.field === "review.expected-digest" && requirement.kind === "exact-fact-match" && requirement.fact === "resource.digest") {
      required.push("expectedDigest");
      properties.expectedDigest = { type: "string", title, pattern: "^[a-f0-9]{64}$", readOnly: true };
      fields.push({ pointer: "/expectedDigest", widget: "text", help: "冻结内容变化后，已有证据会失效。" });
    } else if (requirement.field === "review.acknowledged" && requirement.kind === "boolean-true") {
      required.push("acknowledged");
      properties.acknowledged = { type: "boolean", title };
      fields.push({ pointer: "/acknowledged", widget: "boolean" });
    } else if (requirement.field === "review.reason" && requirement.kind === "text-length") {
      required.push("reason");
      properties.reason = { type: "string", title, minLength: requirement.minLength ?? 1, maxLength: requirement.maxLength ?? 512 };
      fields.push({ pointer: "/reason", widget: "textarea" });
    } else {
      throw new Error(`当前 Portal UI 不支持审批证据 ${requirement.field}/${requirement.kind}`);
    }
  }
  return {
    schema: {
      id: `portal-approval-evidence.v${revision}`,
      schema: { $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required, properties },
    },
    presentation: { layout: "vertical", fields },
  };
}

function configurationForm(client: PortalControlClient, permissionCatalog: PortalPermissionCatalogClient, kind: "create" | "edit" | "new-working-copy"): WorkbenchFormDefinition<PortalRow> {
  return {
    id: kind,
    schema: portalConfigurationSchema,
    presentation: { layout: "vertical", navigation: "sections", sections: [
      { id: "identity", title: "Portal", columns: 2, fields: ["/portalId", "/route", "/domains", "/audience"] },
      { id: "platform", title: "平台与界面", columns: 2, fields: ["/defaultRenderer", "/allowedRenderers", "/userSelectableRenderer", "/defaultTemplate", "/pageBodyWidth", "/navigationOverrides"] },
      { id: "application", title: "功能与服务", columns: 1, fields: ["/applicationPlugins", "/branding", "/config", "/services"] },
    ], fields: [{ pointer: "/applicationPlugins" }, { pointer: "/branding" }, { pointer: "/config" }, { pointer: "/services" }] },
    workflow: {
      dialogWidth: "lg",
      title: kind === "create" ? "新建 Portal" : "编辑 Portal",
      submitLabel: kind === "create" ? "创建" : "保存",
      success: { notify: "Portal 配置已保存", refreshCollection: true, close: true },
    },
    async prepare(selected, signal) {
      const row = selected[0];
      const [snapshot, source] = await Promise.all([
        permissionCatalogSnapshot(permissionCatalog, signal),
        row === undefined ? creationTemplate(client, signal) : Promise.resolve(undefined),
      ]);
      const initialValue = row === undefined
        ? { ...configurationToForm("", source!), portalId: "", route: "/" }
        : configurationToForm(row.id, row.configuration);
      return {
        schema: portalConfigurationSchemaWithPermissions(snapshot.permissions),
        context: { permissionCatalogDigest: snapshot.digest, permissionCodes: snapshot.permissions.map((permission) => permission.code) },
        initialValue,
      };
    },
    async validate({ value, context }) {
      return audiencePermissionErrors(value.audience, stringValues(context.permissionCodes));
    },
    async beforeSubmit({ value }, signal) {
      const snapshot = await permissionCatalogSnapshot(permissionCatalog, signal);
      const fieldErrors = audiencePermissionErrors(value.audience, snapshot.permissions.map((permission) => permission.code));
      return Object.keys(fieldErrors).length === 0 ? undefined : { fieldErrors };
    },
    async submit({ value, selected }) {
      const row = selected[0];
      const template = await creationTemplate(client);
      const base = row?.configuration ?? template;
      const configuration = buildPortalConfiguration(base, value, template.platform);
      const portalId = typeof value.portalId === "string" ? value.portalId : row?.id ?? "";
      if (kind === "create") await client.createPortal(portalId, configuration);
      else if (kind === "new-working-copy" && row !== undefined) await client.createPortalWorkingCopy(row.id, configuration);
      else if (row !== undefined) await client.savePortalWorkingCopy(row.id, row.workingRevision, configuration);
    },
  };
}

async function permissionCatalogSnapshot(client: PortalPermissionCatalogClient, signal: AbortSignal): Promise<{ digest: string; permissions: PortalPermissionChoice[] }> {
  const state = await client.getAuthorizationPolicy();
  if (signal.aborted) throw new DOMException("Permission catalog request cancelled", "AbortError");
  const permissions = [...new Map(state.catalog.permissions
    .filter((permission) => permission.assignable === true && permission.code.trim() !== "")
    .map((permission) => [permission.code, { code: permission.code, title: permission.title } satisfies PortalPermissionChoice] as const)).values()]
    .sort((left, right) => left.code.localeCompare(right.code));
  return { digest: state.catalog.digest, permissions };
}

function audiencePermissionErrors(value: unknown, available: readonly string[]): WorkbenchFormFieldErrors {
  const known = new Set(available);
  const selected = stringValues(value).filter((candidate) => candidate !== "");
  const missing = [...new Set(selected.filter((permission) => !known.has(permission)))].sort();
  return missing.length === 0 ? {} : {
    "/audience": message(namespace, "form.audience.missing", "以下访问权限已不存在或不可分配：{permissions}", { permissions: missing.join("、") }),
  };
}

function stringValues(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((candidate): candidate is string => typeof candidate === "string") : [];
}

const restorePlaceholder: FormSchema = {
  id: "portal-version-restore.loading",
  schema: { $schema: jsonSchemaDialect, type: "object", additionalProperties: false, properties: {} },
};

function restoreForm(client: PortalControlClient): WorkbenchFormDefinition<PortalRow> {
  return {
    id: "restore",
    schema: restorePlaceholder,
    presentation: { layout: "vertical", fields: [{ pointer: "/versionId", widget: "select" }] },
    workflow: {
      dialogWidth: "sm", title: "恢复历史版本", description: "历史内容只会覆盖当前工作副本；保存后仍需重新提交和审批。",
      submitLabel: "恢复到工作副本", success: { notify: "历史版本已恢复到工作副本", refreshCollection: true, close: true },
    },
    async prepare(selected) {
      const row = selected[0];
      if (row === undefined) throw new Error("未选择 Portal");
      const history = await client.portalVersionHistory(row.id);
      if (history.entries.length === 0) throw new Error("当前 Portal 没有可恢复的版本");
      const values = history.entries.map((entry) => entry.versionRef.versionId);
      return {
        schema: {
          id: "portal-version-restore.v1",
          schema: {
            $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["versionId"],
            properties: { versionId: { type: "string", title: "历史版本", enum: values } },
          },
        },
        initialValue: { versionId: values[0] },
      };
    },
    async submit({ value, selected }) {
      const row = selected[0];
      if (row === undefined || typeof value.versionId !== "string") return;
      await client.restorePortalVersion(row.id, value.versionId, row.workingRevision);
    },
  };
}

async function creationTemplate(client: PortalControlClient, signal?: AbortSignal): Promise<PortalConfiguration> {
  const governance = await client.governance();
  if (signal?.aborted === true) throw new DOMException("Portal template request cancelled", "AbortError");
  const source = governance.creationTemplate ?? governance.portals.map(currentConfiguration).find((value): value is PortalConfiguration => value !== undefined);
  if (source === undefined) throw new Error("当前没有可用的平台配置模板");
  return source;
}

function currentConfiguration(portal: { workingCopy?: { configuration: PortalConfiguration }; pendingPublication?: { source: { configuration?: PortalConfiguration } }; publishedPublication?: { source: { configuration?: PortalConfiguration } } }): PortalConfiguration | undefined {
  return portal.workingCopy?.configuration ?? portal.pendingPublication?.source.configuration ?? portal.publishedPublication?.source.configuration;
}
