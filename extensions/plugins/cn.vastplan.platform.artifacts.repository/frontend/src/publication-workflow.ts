import type { PlatformAdminClient } from "@vastplan/platform-admin";
import { jsonSchemaDialect, type FormSchema, type JSONValue, type WorkbenchFormDefinition, type WorkbenchOverlayDefinition } from "@vastplan/workbench-sdk";
import { text, type Row } from "./shared.js";

const schema: FormSchema = {
  id: "artifact-publication-submit.v1",
  schema: {
    $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["reason", "expectedRevision"],
    properties: {
      reason: { type: "string", title: "发布原因", minLength: 1, maxLength: 500 },
      expectedRevision: { type: "integer", title: "审批 Revision", minimum: 0 },
    },
  },
  localization: { "/properties/reason/title": text("form.publication.reason", "发布原因"), "/properties/expectedRevision/title": text("form.publication.revision", "审批 Revision") },
};

export function publicationForm(client: PlatformAdminClient): WorkbenchFormDefinition<Row> {
  return {
    id: "publication", schema,
    presentation: { layout: "vertical", fields: [{ pointer: "/reason", widget: "textarea" }, { pointer: "/expectedRevision", widget: "hidden" }] },
    workflow: { surface: "drawer", size: "md", title: text("form.publication.title", "提交 stable 发布审批"), description: text("form.publication.description", "审批精确绑定当前 testing 制品、发布者签名、来源证明 sidecar 与目标 stable ref。批准人必须是另一位用户。"), submitLabel: text("action.publication.submit", "提交审批"), success: { notify: text("notice.publicationSubmitted", "发布审批已提交"), refreshCollection: true, close: true } },
    async prepare() { const page = await client.listArtifactPublications(); return { initialValue: { reason: "", expectedRevision: page.revision } }; },
    async submit({ value, selected }) {
      const row = selected[0]; if (row === undefined) return;
      await client.submitArtifactPublication({ source: { pluginId: String(row.pluginId), version: String(row.version), channel: String(row.channel) }, targetChannel: "stable", reason: String(value.reason ?? "").trim(), expectedRevision: Number(value.expectedRevision) });
    },
  };
}

export function publicationTransitionForm(client: PlatformAdminClient, mode: "reject" | "cancel"): WorkbenchFormDefinition<Row> {
  const rejecting = mode === "reject";
  return {
    id: mode,
    schema: {
      id: `artifact-publication-${mode}.v1`,
      schema: { $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["reason"], properties: { reason: { type: "string", title: "原因", minLength: 1, maxLength: 500 } } },
      localization: { "/properties/reason/title": text(`form.publication.${mode}Reason`, rejecting ? "驳回原因" : "撤销原因") },
    },
    presentation: { layout: "vertical", fields: [{ pointer: "/reason", widget: "textarea" }] },
    workflow: {
      surface: "dialog", size: "sm",
      title: text(`form.publication.${mode}Title`, rejecting ? "驳回发布审批" : "撤销发布审批"),
      description: text(`form.publication.${mode}Description`, rejecting ? "驳回会立即使当前 stable 发布授权失效；提交人与驳回人必须分离。" : "只有原提交人可以撤销，撤销后该申请不可恢复。"),
      submitLabel: text(`action.publication.${mode}`, rejecting ? "确认驳回" : "确认撤销"),
      success: { notify: text(`notice.publication${rejecting ? "Rejected" : "Cancelled"}`, rejecting ? "发布审批已驳回" : "发布审批已撤销"), refreshCollection: true, close: true },
    },
    async submit({ value, selected }) {
      const row = selected[0]; if (row === undefined) return;
      const reason = String(value.reason ?? "").trim(), id = String(row.id), revision = Number(row.publicationRevision);
      if (rejecting) await client.rejectArtifactPublication(id, revision, reason);
      else await client.cancelArtifactPublication(id, revision, reason);
    },
  };
}

export function evidenceOverlay(client: PlatformAdminClient): WorkbenchOverlayDefinition<Row> {
  return {
    id: "evidence", surface: "drawer", size: "lg", title: text("overlay.evidence.title", "供应链证据"),
    async load(selected) {
      const row = selected[0];
      if (row === undefined) return { kind: "json", documents: [] };
      const evidence = await client.artifactSupplyChainEvidence({ pluginId: String(row.pluginId), version: String(row.version), channel: String(row.channel) });
      return { kind: "json", documents: [{ title: text("overlay.evidence.document", "已验证制品与审批轨迹"), value: JSON.parse(JSON.stringify(evidence)) as JSONValue }] };
    },
  };
}
