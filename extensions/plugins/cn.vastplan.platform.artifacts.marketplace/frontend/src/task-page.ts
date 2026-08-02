import type { PlatformAdminClient, PluginInstallationCandidate } from "@vastplan/platform-admin";
import { defineCollectionPage, type CollectionQuery, type LocalizedText } from "@vastplan/workbench-sdk";
import { installationApprovalForm } from "./approval-form.js";

export interface TaskRow extends Record<string, unknown> { id: string; pluginId: string; action: string; status: string; updatedAt: string; candidate: PluginInstallationCandidate; }

export function createTaskPage(deployment: PlatformAdminClient, path: string, message: Message) {
  const actions = {
    submit: (id: string) => deployment.submitSelfServicePluginInstallationCandidate(id),
    activate: (id: string) => deployment.activateSelfServicePluginInstallationCandidate(id), cancel: (id: string) => deployment.cancelSelfServicePluginInstallationCandidate(id), rollback: (id: string) => deployment.rollbackSelfServicePluginInstallationCandidate(id),
  };
  return defineCollectionPage<TaskRow>({
    id: "platform.service-plugin-tasks", path, title: message("tasks.title", "安装任务"), description: message("tasks.description", "查看当前服务的插件候选、审批与 Generation 滚动状态"), requiredPermissions: ["platform.deployment.plugin.preview"],
    navigation: { id: "platform.service-plugin-tasks", label: message("tasks.title", "安装任务"), semanticID: "platform.operations.deployment", zone: "primary", order: 12 },
    collection: { id: "platform.service-plugin-tasks", title: message("tasks.title", "安装任务"), view: "table", selection: "single", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20, 50] },
      columns: [{ key: "pluginId", label: "插件 ID", defaultVisible: true, minWidth: 240 }, { key: "action", label: "变更", defaultVisible: true, minWidth: 90 }, { key: "status", label: message("tasks.status", "状态"), format: "status", defaultVisible: true, minWidth: 120 }, { key: "updatedAt", label: message("tasks.updated", "更新时间"), format: "datetime", defaultVisible: true, minWidth: 180 }],
      actions: [
        { id: "submit", label: message("action.submit", "提交"), icon: "upload", placement: "record.row", requiredPermissions: ["platform.deployment.plugin.request"], visibleWhen: { pointer: "/status", equals: "Planned" } },
        { id: "approve", label: message("action.approve", "批准"), icon: "success", placement: "record.row", requiredPermissions: ["platform.deployment.plugin.approve"], visibleWhen: { pointer: "/status", equals: "PendingApproval" }, form: "approve-service-plugin-installation" },
        { id: "activate", label: message("action.activate", "激活"), icon: "publish", placement: "record.row", requiredPermissions: ["platform.deployment.plugin.activate"], visibleWhen: { pointer: "/status", equals: "Approved" } },
        { id: "cancel", label: message("action.cancel", "取消"), icon: "remove", placement: "record.row", tone: "danger", requiredPermissions: ["platform.deployment.plugin.request"], visibleWhen: { pointer: "/status", equals: "Planned" } },
        { id: "rollback", label: message("action.rollback", "回滚"), icon: "refresh", placement: "record.row", tone: "danger", requiredPermissions: ["platform.deployment.plugin.activate"], visibleWhen: { pointer: "/status", equals: "Ready" } },
      ] }, forms: [installationApprovalForm(deployment, message)],
    async load(query: CollectionQuery, signal) { const items = (await deployment.listSelfServicePluginInstallationCandidates()).map(taskRow); if (signal.aborted) return { items: [], total: 0 }; const start = (query.page - 1) * query.pageSize; return { items: items.slice(start, start + query.pageSize), total: items.length }; },
    async runAction({ action, selected }) { const row = selected[0], handler = actions[action.id as keyof typeof actions]; if (row === undefined || handler === undefined) return; await handler(row.id); return { notify: { title: action.label, kind: "success" } }; },
  });
}

function taskRow(candidate: PluginInstallationCandidate): TaskRow { return { id: candidate.id, pluginId: candidate.preview.pluginId, action: candidate.preview.action, status: candidate.status, updatedAt: candidate.updatedAt, candidate }; }
type Message = (key: string, fallback: string) => LocalizedText;
