import type { PlatformAdminClient } from "@vastplan/platform-admin";
import { jsonSchemaDialect, type FormSchema, type JSONValue, type WorkbenchFormDefinition } from "@vastplan/workbench-sdk";
import type { InstallationRow } from "./installation-page.js";
import { message } from "./localization.js";

interface EvidenceBinding { property: string; field: string; }

export function installationApprovalForm(deployment: PlatformAdminClient): WorkbenchFormDefinition<InstallationRow> {
  return {
    id: "approve-plugin-installation", schema: evidenceSchema([]), presentation: { preset: "compact", layout: "horizontal" },
    workflow: {
      title: message("action.approve", "批准"),
      description: message("installation.approval.description", "审批绑定当前插件、DataModel 迁移计划和证据；计划变化后必须重新审批。"),
      submitLabel: message("action.approve", "批准"),
      success: { notify: message("action.approve", "批准"), refreshCollection: true, close: true },
    },
    async prepare(selected) {
      const row = selected[0]; if (row === undefined) throw new Error("请选择一个安装候选");
      const requirements = [...(row.candidate.preview.approval?.requirements ?? [])];
      if (row.candidate.preview.impact.schema.requiresBackup && !requirements.some((item) => item.field === "database.backup-ref")) {
        requirements.push({ id: "database.schema-backup", field: "database.backup-ref", kind: "text-length", minLength: 8, maxLength: 512, title: "数据库备份证据引用", audit: true });
      }
      const bindings = requirements.map((requirement, index) => ({ property: `evidence${index + 1}`, field: requirement.field }));
      const initialValue: Record<string, unknown> = {};
      requirements.forEach((requirement, index) => {
        if (requirement.kind === "exact-fact-match" && requirement.fact === "resource.digest") initialValue[`evidence${index + 1}`] = row.candidate.preview.planDigest;
        if (requirement.kind === "boolean-true") initialValue[`evidence${index + 1}`] = false;
      });
      return { schema: evidenceSchema(requirements), initialValue, context: { bindings } };
    },
    async submit({ selected, value, context }) {
      const row = selected[0]; if (row === undefined) throw new Error("请选择一个安装候选");
      const bindings = Array.isArray(context?.bindings) ? context.bindings as unknown as EvidenceBinding[] : [];
      const evidence = Object.fromEntries(bindings.map((binding) => [binding.field, value[binding.property]]));
      await deployment.approvePluginInstallationCandidate(row.id, evidence);
    },
  };
}

function evidenceSchema(requirements: NonNullable<InstallationRow["candidate"]["preview"]["approval"]>["requirements"]): FormSchema {
  const properties: Record<string, JSONValue> = {}, required: string[] = [];
  requirements?.forEach((requirement, index) => {
    const property = `evidence${index + 1}`; required.push(property);
    if (requirement.kind === "boolean-true") properties[property] = { type: "boolean", title: requirement.title || requirement.id, const: true };
    else properties[property] = { type: "string", title: requirement.title || requirement.id, ...(requirement.kind === "exact-fact-match" ? { readOnly: true } : { ...(requirement.minLength === undefined ? {} : { minLength: requirement.minLength }), ...(requirement.maxLength === undefined ? {} : { maxLength: requirement.maxLength }) }) };
  });
  return { id: "plugin-installation.approval.v1", schema: { $schema: jsonSchemaDialect, type: "object", additionalProperties: false, properties, ...(required.length === 0 ? {} : { required }) }, uiSchema: {} };
}
