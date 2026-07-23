import type { ArtifactLifecycleRequest, PlatformAdminClient } from "@vastplan/platform-admin";
import {
  jsonSchemaDialect,
  type FormSchema,
  type WorkbenchFormDefinition,
  type WorkbenchFormFieldErrors,
  type WorkbenchFormSubmitResult,
} from "@vastplan/workbench-sdk";
import { lifecycleOptions, text, type Row } from "./shared.js";

const pluginIDPattern = "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$";

const schema: FormSchema = {
  id: "artifact-lifecycle.v1",
  schema: {
    $schema: jsonSchemaDialect,
    type: "object",
    additionalProperties: false,
    required: ["status", "reason"],
    properties: {
      status: { type: "string", title: "生命周期", oneOf: lifecycleOptions.map((option) => ({ const: option.value, title: option.value })) },
      reason: { type: "string", title: "变更原因", minLength: 1, maxLength: 500 },
      replacementPluginId: { type: "string", title: "替代插件 ID", pattern: pluginIDPattern, maxLength: 160 },
      replacementConstraint: { type: "string", title: "替代版本约束", minLength: 1, maxLength: 160 },
    },
  },
  localization: {
    "/properties/status/title": text("form.lifecycle.status", "生命周期"),
    "/properties/status/oneOf/0/title": text("lifecycle.active", "活动"),
    "/properties/status/oneOf/1/title": text("lifecycle.deprecated", "已弃用"),
    "/properties/status/oneOf/2/title": text("lifecycle.yanked", "已下架"),
    "/properties/status/oneOf/3/title": text("lifecycle.revoked", "已撤销"),
    "/properties/reason/title": text("form.lifecycle.reason", "变更原因"),
    "/properties/replacementPluginId/title": text("form.lifecycle.replacementPlugin", "替代插件 ID"),
    "/properties/replacementConstraint/title": text("form.lifecycle.replacementConstraint", "替代版本约束"),
  },
};

export function lifecycleForm(client: PlatformAdminClient): WorkbenchFormDefinition<Row> {
  return {
    id: "lifecycle",
    schema,
    presentation: {
      layout: "vertical",
      navigation: "sections",
      sections: [
        { id: "transition", title: text("form.lifecycle.transition", "生命周期变更"), columns: 2, fields: ["/status", "/reason"] },
        { id: "replacement", title: text("form.lifecycle.replacement", "弃用替代方案"), description: text("form.lifecycle.replacementHelp", "仅 deprecated 状态可以声明替代插件和 SemVer 约束。"), columns: 2, fields: ["/replacementPluginId", "/replacementConstraint"] },
      ],
      fields: [
        { pointer: "/status", widget: "select" },
        { pointer: "/reason", span: 2, widget: "textarea" },
        { pointer: "/replacementPluginId", visibleWhen: { pointer: "/status", equals: "deprecated" } },
        { pointer: "/replacementConstraint", visibleWhen: { pointer: "/status", equals: "deprecated" } },
      ],
    },
    workflow: {
      surface: "drawer",
      size: "md",
      title: text("form.lifecycle.title", "变更制品生命周期"),
      description: text("form.lifecycle.description", "变更使用 Catalog revision CAS 写入审计流水；revoked 为不可逆安全状态。"),
      submitLabel: text("action.lifecycle.save", "确认变更"),
      confirmBeforeSubmit: text("confirm.lifecycle", "生命周期变更会影响新的解析和交付，请确认原因与替代版本均准确。"),
      success: { notify: text("notice.lifecycleSaved", "制品生命周期已更新"), refreshCollection: true, close: true },
    },
    async load(selected) {
      const row = selected[0];
      return row === undefined ? {} : {
        status: row.lifecycle,
        reason: row.lifecycleReason ?? "",
        replacementPluginId: row.replacementPluginId ?? "",
        replacementConstraint: row.replacementConstraint ?? "",
      };
    },
    async validate({ value }): Promise<WorkbenchFormFieldErrors> {
      const errors: Record<string, ReturnType<typeof text>> = {};
      const status = typeof value.status === "string" ? value.status : "";
      const pluginId = typeof value.replacementPluginId === "string" ? value.replacementPluginId.trim() : "";
      const constraint = typeof value.replacementConstraint === "string" ? value.replacementConstraint.trim() : "";
      if ((pluginId === "") !== (constraint === "")) {
        errors.replacementPluginId = text("error.lifecycle.replacementPair", "替代插件与版本约束必须同时填写");
        errors.replacementConstraint = text("error.lifecycle.replacementPair", "替代插件与版本约束必须同时填写");
      }
      if (status !== "deprecated" && (pluginId !== "" || constraint !== "")) {
        errors.replacementPluginId = text("error.lifecycle.replacementDeprecated", "只有 deprecated 状态可以声明替代制品");
      }
      return errors;
    },
    async submit({ value, selected }): Promise<WorkbenchFormSubmitResult | void> {
      const row = selected[0];
      if (row === undefined) return;
      const status = value.status;
      const reason = typeof value.reason === "string" ? value.reason.trim() : "";
      if (!lifecycleOptions.some((option) => option.value === status) || reason === "") {
        return { fieldErrors: { reason: text("error.lifecycle.required", "请选择状态并填写变更原因") } };
      }
      if (status === row.lifecycle) {
        return { fieldErrors: { status: text("error.lifecycle.same", "请选择不同于当前状态的新状态") } };
      }
      const pluginId = typeof value.replacementPluginId === "string" ? value.replacementPluginId.trim() : "";
      const constraint = typeof value.replacementConstraint === "string" ? value.replacementConstraint.trim() : "";
      if ((pluginId === "") !== (constraint === "")) {
        return { fieldErrors: { replacementPluginId: text("error.lifecycle.replacementPair", "替代插件与版本约束必须同时填写"), replacementConstraint: text("error.lifecycle.replacementPair", "替代插件与版本约束必须同时填写") } };
      }
      const request: ArtifactLifecycleRequest = {
        ref: { pluginId: String(row.pluginId), version: String(row.version), channel: String(row.channel) },
        status: status as ArtifactLifecycleRequest["status"],
        reason,
        expectedRevision: Number(row.catalogRevision),
        ...(pluginId === "" ? {} : { replacement: { pluginId, constraint } }),
      };
      await client.setArtifactLifecycle(request);
    },
  };
}
