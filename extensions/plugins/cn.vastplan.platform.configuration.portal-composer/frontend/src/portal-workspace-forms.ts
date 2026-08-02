import { type PortalConfiguration, type PortalControlClient } from "@vastplan/ui-primitives";
import { jsonSchemaDialect, type FormSchema, type WorkbenchFormDefinition } from "@vastplan/workbench-sdk";
import { buildPortalConfiguration, configurationToForm, portalConfigurationSchema } from "./portal-form";
import type { PortalRow } from "./portal-model";

export function portalForms(client: PortalControlClient): WorkbenchFormDefinition<PortalRow>[] {
  return [configurationForm(client, "create"), configurationForm(client, "edit"), configurationForm(client, "new-working-copy"), approvalReviewForm(client), restoreForm(client)];
}

function approvalReviewForm(client: PortalControlClient): WorkbenchFormDefinition<PortalRow> {
  return {
    id: "approval-review",
    schema: {
      id: "portal-approval-review.v1",
      schema: {
        $schema: jsonSchemaDialect, type: "object", additionalProperties: false,
        required: ["expectedDigest", "acknowledged", "reason"],
        properties: {
          expectedDigest: { type: "string", title: "冻结配置摘要", pattern: "^[a-f0-9]{64}$", readOnly: true },
          acknowledged: { type: "boolean", title: "我已复核上述冻结配置，确认内容准确" },
          reason: { type: "string", title: "审批原因", minLength: 4, maxLength: 512 },
        },
      },
    },
    presentation: { layout: "vertical", fields: [
      { pointer: "/expectedDigest", widget: "text", help: "提交后若冻结内容发生变化，审批会被拒绝。" },
      { pointer: "/acknowledged", widget: "boolean" },
      { pointer: "/reason", widget: "textarea" },
    ] },
    workflow: {
      dialogWidth: "sm", title: "复验并批准", description: "当前种子策略允许同一管理员复验，但不会把它记录为异人审批。",
      submitLabel: "确认批准", success: { notify: "Portal 已通过单人复验批准", refreshCollection: true, close: true },
    },
    async prepare(selected) {
      const row = selected[0];
      if (row === undefined || row.portal.pendingPublication === undefined) throw new Error("未选择待审批 Portal");
      return { initialValue: { expectedDigest: row.portal.pendingPublication.digest, acknowledged: false, reason: "" } };
    },
    async submit({ value, selected }) {
      const row = selected[0];
      if (row === undefined || typeof value.expectedDigest !== "string" || value.acknowledged !== true || typeof value.reason !== "string") return;
      await client.approvePortalPublication(row.id, row.publicationId, {
        expectedDigest: value.expectedDigest, acknowledged: true, reason: value.reason,
      });
    },
  };
}

function configurationForm(client: PortalControlClient, kind: "create" | "edit" | "new-working-copy"): WorkbenchFormDefinition<PortalRow> {
  return {
    id: kind,
    schema: portalConfigurationSchema,
    presentation: { layout: "vertical", navigation: "sections", sections: [
      { id: "identity", title: "Portal", columns: 2, fields: ["/portalId", "/route", "/domains", "/audience"] },
      { id: "platform", title: "平台与界面", columns: 2, fields: ["/defaultRenderer", "/allowedRenderers", "/userSelectableRenderer", "/defaultTemplate", "/pageBodyWidth", "/navigationGroups"] },
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
      if (row !== undefined) return { initialValue: configurationToForm(row.id, row.configuration) };
      const source = await creationTemplate(client, signal);
      return { initialValue: { ...configurationToForm("", source), portalId: "", route: "/" } };
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
