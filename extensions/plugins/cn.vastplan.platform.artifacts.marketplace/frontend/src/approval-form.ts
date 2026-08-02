import type { PlatformAdminClient } from "@vastplan/platform-admin";
import { jsonSchemaDialect, type FormSchema, type JSONValue, type LocalizedText, type WorkbenchFormDefinition } from "@vastplan/workbench-sdk";
import type { TaskRow } from "./task-page.js";

interface EvidenceBinding { property: string; field: string; }

export function installationApprovalForm(deployment: PlatformAdminClient, message: Message): WorkbenchFormDefinition<TaskRow> {
  return {
    id: "approve-service-plugin-installation", schema: approvalSchema([]), presentation: { preset: "compact", layout: "horizontal" },
    workflow: { title: message("action.approve", "批准"), description: "审批要求由当前 Approval Policy Profile 动态生成；提交时服务端会按冻结计划重新求值。", submitLabel: message("action.approve", "批准"), success: { notify: message("action.approve", "批准"), refreshCollection: true, close: true } },
    async prepare(selected) {
      const row = selected[0]; if (row === undefined) throw new Error("请选择一个安装候选");
      const requirements = row.candidate.preview.approval?.requirements ?? [];
      const bindings: EvidenceBinding[] = requirements.map((requirement, index) => ({ property: `evidence${index + 1}`, field: requirement.field }));
      const initialValue: Record<string, unknown> = {};
      requirements.forEach((requirement, index) => {
        if (requirement.kind === "exact-fact-match" && requirement.fact === "resource.digest") initialValue[`evidence${index + 1}`] = row.candidate.preview.planDigest;
        if (requirement.kind === "boolean-true") initialValue[`evidence${index + 1}`] = false;
      });
      return { schema: approvalSchema(requirements), initialValue, context: { bindings } };
    },
    async submit({ selected, value, context }) {
      const row = selected[0]; if (row === undefined) throw new Error("请选择一个安装候选");
      const bindings = Array.isArray(context?.bindings) ? context.bindings as unknown as EvidenceBinding[] : [];
      const evidence = Object.fromEntries(bindings.map((binding) => [binding.field, value[binding.property]]));
      await deployment.approveSelfServicePluginInstallationCandidate(row.id, evidence);
    },
  };
}

function approvalSchema(requirements: NonNullable<TaskRow["candidate"]["preview"]["approval"]>["requirements"]): FormSchema {
  const properties: Record<string, JSONValue> = {};
  const required: string[] = [];
  requirements?.forEach((requirement, index) => {
    const property = `evidence${index + 1}`;
    required.push(property);
    const title = requirement.title || requirement.id;
    if (requirement.kind === "boolean-true") properties[property] = { type: "boolean", title, const: true };
    else properties[property] = { type: "string", title, ...(requirement.kind === "exact-fact-match" ? { readOnly: true } : { ...(requirement.minLength === undefined ? {} : { minLength: requirement.minLength }), ...(requirement.maxLength === undefined ? {} : { maxLength: requirement.maxLength }) }) };
  });
  return { id: "plugin-installation.approval.v1", schema: { $schema: jsonSchemaDialect, type: "object", additionalProperties: false, properties, ...(required.length === 0 ? {} : { required }) }, uiSchema: {} };
}

type Message = (key: string, fallback: string) => LocalizedText;
