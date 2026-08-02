import type { PlatformAdminClient, PluginInstallationCandidate } from "@vastplan/platform-admin";
import { defineCollectionPage, type CollectionPageDefinition, type CollectionQuery, type JSONValue, type LocalizedText } from "@vastplan/workbench-sdk";
import { installationForm } from "./installation-form.js";
import { message } from "./localization.js";

export interface InstallationRow extends Record<string, unknown> {
  id: string;
  status: PluginInstallationCandidate["status"];
  rolloutStatus: string;
  rolloutReplicas: string;
  hasRollout: boolean;
  deployment: string;
  unitId: string;
  pluginId: string;
  action: string;
  version: string;
  serviceRevisionId: number;
  requestedBy: string;
  updatedAt: string;
  candidate: PluginInstallationCandidate;
}

export function createPluginInstallationPage(deployment: PlatformAdminClient, repository: PlatformAdminClient | undefined, serviceID: string, path: string, title: LocalizedText): CollectionPageDefinition<InstallationRow> {
  const statusLabels = {
    Planned: message("installation.status.planned", "已规划"), PendingApproval: message("installation.status.pending", "待审批"),
    Approved: message("installation.status.approved", "已批准"), Activating: message("installation.status.activating", "激活中"),
    Ready: message("installation.status.ready", "已激活"), Stale: message("installation.status.stale", "已失效"),
    Cancelled: message("installation.status.cancelled", "已取消"), RolledBack: message("installation.status.rolledBack", "已回滚"),
    Superseded: message("installation.status.superseded", "已被替代"),
  };
  const rolloutLabels = {
    Pending: message("installation.rollout.pending", "滚动中"), Blocked: message("installation.rollout.blocked", "受阻"),
    Ready: message("installation.rollout.ready", "副本就绪"), Degraded: message("installation.rollout.degraded", "部分降级"),
    DependencyLost: message("installation.rollout.dependencyLost", "依赖异常"), Failed: message("installation.rollout.failed", "失败"),
    Stopped: message("installation.rollout.stopped", "已停止"), Unknown: message("installation.rollout.unknown", "待观察"),
  };
  const handlers: Record<string, (id: string) => Promise<unknown>> = {
    submit: (id) => deployment.submitPluginInstallationCandidate(id),
    approve: (id) => deployment.approvePluginInstallationCandidate(id),
    activate: (id) => deployment.activatePluginInstallationCandidate(id),
    cancel: (id) => deployment.cancelPluginInstallationCandidate(id),
    rollback: (id) => deployment.rollbackPluginInstallationCandidate(id),
  };
  return defineCollectionPage<InstallationRow>({
    id: `platform.plugin-installations.${serviceID}`, path, title,
    description: message("installation.page.description", "跨逻辑服务生成插件变更预览，并复用统一审批、Generation 激活和单调回滚链"),
    requiredPermissions: ["platform.deployment.plugin.preview"],
    navigation: { id: `platform.plugin-installations.${serviceID}`, label: title, parentMenuRef: { pluginID: "cn.vastplan.platform.infrastructure.deployment-manager", nodeID: "operations" }, order: 12 },
    pageActions: [{ id: "create", label: message("installation.action.new", "创建安装预览"), icon: "add", tone: "primary", form: "create-plugin-installation", requiredPermissions: ["platform.deployment.plugin.request"] }],
    collection: {
      id: `platform.plugin-installations.${serviceID}`, title, view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20, 50, 100] },
      filterPanel: { fields: [
        { id: "deployment", label: message("column.deployment", "部署"), kind: "text" },
        { id: "pluginId", label: message("form.pluginId", "插件 ID"), kind: "text" },
        { id: "status", label: message("column.status", "状态"), kind: "select", options: Object.entries(statusLabels).map(([value, label]) => ({ value, label })) },
      ] },
      columns: [
        { key: "deployment", label: message("column.deployment", "部署"), defaultVisible: true, minWidth: 160 },
        { key: "unitId", label: message("column.unit", "服务单元"), defaultVisible: true, minWidth: 120 },
        { key: "pluginId", label: message("form.pluginId", "插件 ID"), defaultVisible: true, minWidth: 240 },
        { key: "action", label: message("installation.column.change", "变更"), valueLabels: { install: message("installation.action.install", "安装"), upgrade: message("installation.action.upgrade", "升级"), remove: message("installation.action.remove", "卸载") }, defaultVisible: true, minWidth: 90 },
        { key: "version", label: message("form.version", "版本要求"), defaultVisible: true, minWidth: 110 },
        { key: "status", label: message("column.status", "候选状态"), format: "status", valueLabels: statusLabels, statusTones: { Planned: "info", PendingApproval: "warning", Approved: "info", Activating: "warning", Ready: "success", Stale: "error", Cancelled: "neutral", RolledBack: "neutral", Superseded: "neutral" }, defaultVisible: true, minWidth: 110 },
        { key: "rolloutStatus", label: message("installation.column.rollout", "滚动状态"), format: "status", valueLabels: rolloutLabels, statusTones: { Pending: "warning", Blocked: "warning", Ready: "success", Degraded: "warning", DependencyLost: "error", Failed: "error", Stopped: "neutral", Unknown: "neutral" }, defaultVisible: true, minWidth: 110 },
        { key: "rolloutReplicas", label: message("installation.column.replicas", "就绪副本"), defaultVisible: true, minWidth: 100 },
        { key: "serviceRevisionId", label: "Revision", format: "number", defaultVisible: false, minWidth: 90 },
        { key: "requestedBy", label: message("installation.column.requestedBy", "申请人"), defaultVisible: false, minWidth: 120 },
        { key: "updatedAt", label: message("column.updated", "更新时间"), format: "datetime", defaultVisible: true, minWidth: 180 },
      ],
      selection: "single",
      actions: installationActions(),
    },
    forms: [installationForm(deployment, repository)],
    overlays: [
      { id: "preview", surface: "drawer", width: "lg", title: message("installation.overlay.preview", "插件变更与配置预览"), async load(selected) {
        return { kind: "json", documents: [{ title: message("installation.document.preview", "候选预览"), value: (selected[0]?.candidate.preview ?? {}) as unknown as JSONValue }] };
      } },
      { id: "rollout", surface: "drawer", width: "lg", title: message("installation.overlay.rollout", "服务滚动状态"), async load(selected) {
        const rollout = selected[0]?.candidate.rollout;
        return { kind: "table", rowKey: "id", rows: (rollout?.units ?? []) as Array<Record<string, unknown>>, columns: [
          { key: "id", label: message("column.unit", "服务单元") }, { key: "status", label: message("column.status", "状态"), format: "status", valueLabels: rolloutLabels },
          { key: "desired_replicas", label: message("installation.column.desired", "期望副本"), format: "number" },
          { key: "replicas", label: message("installation.column.observed", "已观察副本"), format: "number" },
          { key: "ready_replicas", label: message("installation.column.ready", "就绪副本"), format: "number" },
        ] };
      } },
    ],
    async load(query: CollectionQuery, signal) {
      const deploymentFilter = filterText(query.filters.deployment);
      const pluginFilter = filterText(query.filters.pluginId);
      const status = typeof query.filters.status === "string" ? query.filters.status : "";
      const rows = (await deployment.listPluginInstallationCandidates()).map(installationRow).filter((row) =>
        (deploymentFilter === "" || row.deployment.toLowerCase().includes(deploymentFilter))
        && (pluginFilter === "" || row.pluginId.toLowerCase().includes(pluginFilter))
        && (status === "" || row.status === status));
      if (signal.aborted) return { items: [], total: 0 };
      const start = Math.max(0, (query.page - 1) * query.pageSize);
      return { items: rows.slice(start, start + query.pageSize), total: rows.length };
    },
    async runAction({ action, selected }) {
      const row = selected[0];
      const handler = handlers[action.id];
      if (row === undefined || handler === undefined) return;
      await handler(row.id);
      return { notify: { title: action.label, kind: "success" } };
    },
  });
}

export function installationRow(candidate: PluginInstallationCandidate): InstallationRow {
  const units = candidate.rollout?.units ?? [];
  const ready = units.reduce((sum, unit) => sum + unit.ready_replicas, 0);
  const desired = units.reduce((sum, unit) => sum + unit.desired_replicas, 0);
  return {
    id: candidate.id, status: candidate.status, rolloutStatus: candidate.rollout?.status ?? "Unknown", hasRollout: candidate.rollout !== undefined,
    rolloutReplicas: units.length === 0 ? "—" : `${ready}/${desired}`,
    deployment: candidate.preview.target.deployment, unitId: candidate.preview.target.unitId,
    pluginId: candidate.preview.pluginId, action: candidate.preview.action,
    version: candidate.preview.artifactLock?.roots.find((root) => root.pluginId === candidate.preview.pluginId)?.constraint ?? "—",
    serviceRevisionId: candidate.serviceRevisionId, requestedBy: candidate.requestedBy, updatedAt: candidate.updatedAt, candidate,
  };
}

function installationActions() {
  return [
    { id: "submit", label: message("action.submit", "提交审批"), icon: "upload", placement: "record.row", requiredPermissions: ["platform.deployment.plugin.request"], visibleWhen: { pointer: "/status", equals: "Planned" }, confirm: message("installation.confirm.submit", "提交前会重新解析 Planner；Catalog、计划或活动修订漂移会使候选失效。") },
    { id: "approve", label: message("action.approve", "批准"), icon: "success", placement: "record.row", tone: "primary", requiredPermissions: ["platform.deployment.plugin.approve"], visibleWhen: { pointer: "/status", equals: "PendingApproval" }, confirm: message("installation.confirm.approve", "审批人必须与提交人不同。") },
    { id: "activate", label: message("installation.action.activate", "激活"), icon: "publish", placement: "record.row", tone: "primary", requiredPermissions: ["platform.deployment.plugin.activate"], visibleWhen: { pointer: "/status", equals: "Approved" }, confirm: message("installation.confirm.activate", "激活将创建新的服务 Generation，由 Scheduler 滚动到目标节点。") },
    { id: "cancel", label: message("installation.action.cancel", "取消候选"), icon: "remove", placement: "record.row", tone: "danger", requiredPermissions: ["platform.deployment.plugin.request"], visibleWhen: { pointer: "/status", equals: "Planned" }, confirm: message("installation.confirm.cancel", "取消会移除尚未提交的服务草稿，但保留候选审计记录。") },
    { id: "rollback", label: message("action.rollback", "回滚"), icon: "refresh", placement: "record.row", tone: "danger", requiredPermissions: ["platform.deployment.plugin.activate"], visibleWhen: { pointer: "/status", equals: "Ready" }, confirm: message("installation.confirm.rollback", "回滚会创建更高的单调服务修订，不会覆盖历史版本。") },
    { id: "preview", label: message("installation.action.preview", "查看预览"), icon: "search", placement: "record.row", requiredPermissions: ["platform.deployment.plugin.preview"], overlay: "preview" },
    { id: "rollout", label: message("installation.action.rollout", "滚动状态"), icon: "info", placement: "record.row", requiredPermissions: ["platform.deployment.plugin.preview"], overlay: "rollout", visibleWhen: { pointer: "/hasRollout", equals: true } },
  ] as const;
}

function filterText(value: unknown): string { return typeof value === "string" ? value.trim().toLowerCase() : ""; }
