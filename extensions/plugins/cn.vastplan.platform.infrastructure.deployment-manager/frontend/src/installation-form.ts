import type { ArtifactCatalogEntry, PlatformAdminClient, PluginInstallationPreviewRequest, PluginInstallationTargetOption } from "@vastplan/platform-admin";
import { jsonSchemaDialect, type FormSchema, type WorkbenchFormDefinition } from "@vastplan/workbench-sdk";
import { message } from "./localization.js";

interface InstallationTargetChoice {
  key: string;
  title: string;
  deployment: string;
  unitId: string;
  activeRevision: number;
}

export function installationForm(deployment: PlatformAdminClient, portalID: string, repository?: PlatformAdminClient): WorkbenchFormDefinition {
  return {
    id: "create-plugin-installation",
    schema: installationSchema([], []),
    presentation: {
      layout: "vertical", navigation: "sections",
      sections: [
        { id: "targets", title: message("installation.section.targets", "目标服务"), columns: 1, fields: ["/targets", "/portalTargets"] },
        { id: "change", title: message("installation.section.change", "插件变更"), columns: 2, columnWidths: [45, 55], fields: ["/action", "/pluginId", "/versionPolicy", "/version", "/channel", "/features"] },
      ],
      fields: [
        { pointer: "/targets" }, { pointer: "/portalTargets" }, { pointer: "/action", widget: "select" }, { pointer: "/pluginId", span: 2 },
        { pointer: "/versionPolicy", widget: "select", visibleWhen: { pointer: "/action", in: ["install", "upgrade"] } },
        { pointer: "/version", visibleWhen: { pointer: "/action", in: ["install", "upgrade"] } },
        { pointer: "/channel", widget: "select", visibleWhen: { pointer: "/action", in: ["install", "upgrade"] } },
        { pointer: "/features", span: 2, visibleWhen: { pointer: "/action", in: ["install", "upgrade"] } },
      ],
    },
    workflow: {
      dialogWidth: "lg", title: message("installation.form.title", "创建插件安装预览"),
      description: message("installation.form.description", "每个目标会生成独立候选；先检查 Planner、配置缺口和制品差异，再决定是否提交审批。"),
      submitLabel: message("installation.action.create", "生成预览候选"),
      success: { notify: message("installation.notice.created", "插件安装预览候选已创建"), refreshCollection: true, close: true },
    },
    initialValue: { targets: [], portalTargets: [portalID], action: "install", versionPolicy: "compatible", channel: "stable", features: [] },
    async prepare(_selected, signal) {
      const targetOptions = await deployment.listPluginInstallationTargets();
      let catalog: Awaited<ReturnType<PlatformAdminClient["listArtifactCatalog"]>> | undefined;
      try { catalog = await repository?.listArtifactCatalog({ target: "backend", lifecycle: "active", page: 1, pageSize: 100 }); }
      catch { catalog = undefined; }
      if (signal.aborted) return {};
      const targets = installationTargetChoices(targetOptions);
      return {
        schema: installationSchema(targets, catalog !== undefined && catalog.total <= catalog.items.length ? catalog.items : []),
        context: { targets: Object.fromEntries(targets.map((target) => [target.key, target])) },
      };
    },
    async submit({ value, context }, signal) {
      const requests = buildInstallationRequests(value, context?.targets);
      for (const request of requests) {
        if (signal.aborted) return;
        await deployment.createPluginInstallationCandidate(request);
      }
      return { data: { candidates: requests.length } };
    },
  };
}

export function installationTargetChoices(options: readonly PluginInstallationTargetOption[]): InstallationTargetChoice[] {
  return options.map((option) => {
    const key = `${encodeURIComponent(option.target.deployment)}#${encodeURIComponent(option.target.unitId)}`;
    return { key, title: `${option.target.deployment} · ${option.target.unitId}`, deployment: option.target.deployment, unitId: option.target.unitId, activeRevision: option.activeRevision };
  }).sort((left, right) => left.title.localeCompare(right.title));
}

export function buildInstallationRequests(value: Readonly<Record<string, unknown>>, rawTargets: unknown): PluginInstallationPreviewRequest[] {
  const targetLookup = targetRecord(rawTargets);
  const targetKeys = Array.isArray(value.targets) ? value.targets.filter((item): item is string => typeof item === "string") : [];
  if (targetKeys.length < 1 || targetKeys.length > 20 || new Set(targetKeys).size !== targetKeys.length) throw new Error("请选择 1—20 个不同的目标服务");
  const selectedTargets = targetKeys.map((key) => targetLookup[key]);
  if (selectedTargets.some((target) => target === undefined)) throw new Error("目标服务已变化，请重新打开表单");
  if (new Set(selectedTargets.map((target) => target!.deployment)).size !== selectedTargets.length) throw new Error("同一部署一次只能变更一个服务单元，请分批处理");
  const action = value.action;
  const portalTargets = Array.isArray(value.portalTargets) ? value.portalTargets.filter((item): item is string => typeof item === "string" && item.trim() === item && item !== "") : [];
  if (portalTargets.length > 32 || new Set(portalTargets).size !== portalTargets.length) throw new Error("目标 Portal 必须显式选择且不能重复");
  const pluginId = typeof value.pluginId === "string" ? value.pluginId.trim() : "";
  if (action !== "install" && action !== "upgrade" && action !== "remove" || !/^[a-z0-9]+(?:[.-][a-z0-9]+)+$/.test(pluginId)) throw new Error("插件变更定义无效");
  const requirement = action === "remove" ? undefined : {
    pluginId,
    constraint: versionConstraint(value.versionPolicy, value.version),
    channel: typeof value.channel === "string" && value.channel !== "" ? value.channel : "stable",
    ...(Array.isArray(value.features) ? { features: value.features.filter((item): item is string => typeof item === "string" && item !== "") } : {}),
  };
  return targetKeys.map((key) => {
    const target = targetLookup[key]!;
    return {
      version: 1,
      target: { kernel: "backend", deployment: target.deployment, unitId: target.unitId },
      change: { action, pluginId, ...(requirement === undefined ? {} : { requirement }) },
      portalTargets,
      expectedActiveRevision: target.activeRevision,
    };
  });
}

function installationSchema(targets: readonly InstallationTargetChoice[], catalog: readonly ArtifactCatalogEntry[]): FormSchema {
  const pluginOptions = [...new Map(catalog.map((entry) => [entry.ref.pluginId, entry])).values()]
    .sort((left, right) => left.ref.pluginId.localeCompare(right.ref.pluginId))
    .map((entry) => ({ const: entry.ref.pluginId, title: entry.name === "" ? entry.ref.pluginId : `${entry.name} · ${entry.ref.pluginId}` }));
  return {
    id: "plugin-installation.controller.v1",
    schema: {
      $schema: jsonSchemaDialect, type: "object", additionalProperties: false,
      required: ["targets", "portalTargets", "action", "pluginId"],
      properties: {
        targets: { type: "array", title: "目标服务", minItems: 1, maxItems: 20, uniqueItems: true, items: { type: "string", oneOf: targets.map((target) => ({ const: target.key, title: target.title })) } },
        portalTargets: { type: "array", title: "目标 Portal", maxItems: 32, uniqueItems: true, items: { type: "string", minLength: 1, maxLength: 160, pattern: "^[^/\\\\\\u0000]+$" } },
        action: { type: "string", title: "变更类型", default: "install", oneOf: [{ const: "install", title: "安装" }, { const: "upgrade", title: "升级或调整版本" }, { const: "remove", title: "卸载" }] },
        pluginId: { type: "string", title: "应用插件", pattern: "^[a-z0-9]+(?:[.-][a-z0-9]+)+$", ...(pluginOptions.length === 0 ? {} : { oneOf: pluginOptions }) },
        versionPolicy: { type: "string", title: "版本策略", default: "compatible", oneOf: [{ const: "exact", title: "固定版本" }, { const: "compatible", title: "兼容升级" }] },
        version: { type: "string", title: "基准版本", pattern: "^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$" },
        channel: { type: "string", title: "通道", default: "stable", oneOf: [{ const: "stable", title: "稳定版" }, { const: "preview", title: "预发布" }, { const: "testing", title: "测试版" }] },
        features: { type: "array", title: "启用 Feature", uniqueItems: true, maxItems: 64, items: { type: "string", pattern: "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$" } },
      },
      allOf: [{ if: { properties: { action: { enum: ["install", "upgrade"] } } }, then: { required: ["versionPolicy", "version", "channel"] } }],
    },
    uiSchema: {
      targets: { "ui:help": "可同时选择多个逻辑服务；每个目标生成独立候选，不直接选择物理节点。" },
      portalTargets: { "ui:help": "显式选择需要同代切换该全栈插件的 Portal；空数组表示 Backend-only。" },
      action: { "ui:widget": "select" }, pluginId: pluginOptions.length === 0 ? {} : { "ui:widget": "select" },
      versionPolicy: { "ui:widget": "select" }, channel: { "ui:widget": "select" },
    },
  };
}

function versionConstraint(policy: unknown, version: unknown): string {
  if ((policy !== "exact" && policy !== "compatible") || typeof version !== "string" || !/^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.test(version)) throw new Error("插件版本无效");
  return `${policy === "exact" ? "=" : "^"}${version}`;
}

function targetRecord(value: unknown): Record<string, InstallationTargetChoice> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return {};
  return value as Record<string, InstallationTargetChoice>;
}
