import { type PortalControlClient, type PortalRelease } from "@vastplan/ui-primitives";
import { defineCollectionPage, message, type CollectionPageDefinition, type CollectionQuery } from "@vastplan/workbench-sdk";
import { portalForms } from "./portal-workspace-forms";
import { type PortalRow, statusLabels, toPortalRow, versionControlLabels } from "./portal-model";
import { portalOverlays } from "./portal-overlays";

const namespace = "cn.vastplan.platform.configuration.portal-composer";

export function createPortalPage(client: PortalControlClient): CollectionPageDefinition<PortalRow> {
  return defineCollectionPage<PortalRow>({
    id: "platform.portal-composer",
    path: "/settings/portals",
    title: message(namespace, "page.title", "Portal 管理"),
    description: message(namespace, "page.description", "管理 Portal 工作副本、发布审批、上线记录和可选版本历史"),
    navigation: { id: "platform.portal-composer", label: message(namespace, "page.navigation", "Portal 管理"), zone: "settings", order: 11 },
    collection: {
      id: "portals", title: message(namespace, "collection.title", "Portal"), view: "table",
      query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [10, 20, 50] },
      filterPanel: { fields: [
        { id: "portal", label: "Portal", kind: "text" },
        { id: "status", label: "状态", kind: "select", options: Object.entries(statusLabels).map(([value, label]) => ({ value, label })) },
      ] },
      columns: [
        { key: "id", label: "Portal", defaultVisible: true, minWidth: 180 },
        { key: "status", label: "配置状态", format: "status", valueLabels: statusLabels, statusTones: { Draft: "neutral", PendingApproval: "warning", Approved: "info", Published: "success" }, defaultVisible: true },
        { key: "route", label: "访问路径", defaultVisible: true },
        { key: "renderer", label: "UI 框架", defaultVisible: true },
        { key: "layout", label: "布局", defaultVisible: true },
        { key: "versionControlAvailability", label: "版本控制", format: "status", valueLabels: versionControlLabels, statusTones: { disabled: "neutral", available: "success", unavailable: "error" }, defaultVisible: true },
        { key: "currentReleaseId", label: "当前上线", format: "number", defaultVisible: true },
        { key: "updatedAt", label: "更新时间", format: "datetime", defaultVisible: true, minWidth: 180 },
      ],
      selection: "single",
      preferences: { allowedColumns: ["id", "status", "route", "renderer", "layout", "versionControlAvailability", "currentReleaseId", "updatedAt"], density: true },
      actions: portalActions(),
    },
    pageActions: [{ id: "portal.create", label: message(namespace, "action.createPortal", "新建 Portal"), icon: "add", tone: "primary", form: "create" }],
    forms: portalForms(client),
    overlays: portalOverlays(client),
    async load(query, signal) { return loadPortals(client, query, signal); },
    async loadSummary() {
      const { portals } = await client.governance();
      return { title: "Portal 状态", metrics: [
        { id: "portals", label: "Portal", value: portals.length },
        { id: "online", label: "已上线", value: portals.filter((portal) => portal.currentReleaseId !== undefined).length, tone: "success" },
      ] };
    },
    async runAction({ action, selected }) {
      const row = selected[0];
      if (row === undefined) return;
      if (action.id === "portal.submit") await client.submitPortalPublication(row.id, row.workingRevision);
      else if (action.id === "portal.approve") await client.approvePortalPublication(row.id, row.publicationId);
      else if (action.id === "portal.publish") await client.publishPortalPublication(row.id, row.publicationId);
      else if (action.id === "portal.release") {
        failRelease(await client.releasePortalPublication(row.id, {
          publicationId: row.releasePublicationId, expectedCurrentReleaseId: row.currentReleaseId, reason: "管理员从 Portal 管理页上线",
        }));
      } else if (action.id === "portal.rollback") {
        const source = (row.portal.releases ?? []).find((release) => release.status === "Superseded");
        if (source === undefined) throw new Error("没有可回滚的历史上线记录");
        failRelease(await client.rollbackPortalRelease(row.id, source.id, row.currentReleaseId, "管理员从 Portal 管理页回滚"));
      } else return;
      return { notify: { title: action.label, kind: "success" } };
    },
  });
}

function portalActions() {
  return [
    { id: "portal.edit", label: "编辑", icon: "edit" as const, placement: "record.row" as const, form: "edit", visibleWhen: { pointer: "/canEdit", equals: true } },
    { id: "portal.submit", label: "提交审批", icon: "upload" as const, placement: "record.row" as const, visibleWhen: { pointer: "/canSubmit", equals: true } },
    { id: "portal.approve", label: "批准", icon: "success" as const, placement: "record.row" as const, tone: "primary" as const, visibleWhen: { pointer: "/canApprove", equals: true } },
    { id: "portal.publish", label: "发布", icon: "publish" as const, placement: "record.row" as const, tone: "primary" as const, confirm: "发布只冻结该候选，不会改变线上 Portal。", visibleWhen: { pointer: "/canPublish", equals: true } },
    { id: "portal.release", label: "上线", icon: "publish" as const, placement: "record.row" as const, tone: "primary" as const, confirm: "将最近尚未上线的 Published Publication 上线。", visibleWhen: { pointer: "/releaseAvailable", equals: true } },
    { id: "portal.newWorkingCopy", label: "编辑", icon: "edit" as const, placement: "record.row" as const, form: "new-working-copy", visibleWhen: { pointer: "/canCreateWorkingCopy", equals: true } },
    { id: "portal.rollback", label: "回滚上线", icon: "refresh" as const, placement: "record.row" as const, tone: "danger" as const, confirm: "从历史 Release 创建一条新的上线记录。", visibleWhen: { pointer: "/hasRollback", equals: true } },
    { id: "portal.history", label: "版本历史", icon: "info" as const, placement: "record.row" as const, overlay: "history", visibleWhen: { pointer: "/historyAvailable", equals: true } },
    { id: "portal.compare", label: "比较最近版本", icon: "search" as const, placement: "record.row" as const, overlay: "compare", visibleWhen: { pointer: "/diffAvailable", equals: true } },
    { id: "portal.restore", label: "恢复历史版本", icon: "refresh" as const, placement: "record.row" as const, form: "restore", visibleWhen: { pointer: "/restoreAvailable", equals: true } },
    { id: "portal.releases", label: "上线历史", icon: "info" as const, placement: "record.row" as const, overlay: "releases" },
    { id: "portal.audit", label: "审计记录", icon: "info" as const, placement: "record.row" as const, overlay: "audit", visibleWhen: { pointer: "/auditAvailable", equals: true } },
    { id: "portal.preview", label: "当前完整配置", icon: "search" as const, placement: "record.row" as const, overlay: "configuration" },
  ];
}

async function loadPortals(client: PortalControlClient, query: CollectionQuery, signal: AbortSignal) {
  const { portals } = await client.governance();
  const rows = portals.flatMap(toPortalRow);
  const portal = typeof query.filters.portal === "string" ? query.filters.portal.trim().toLowerCase() : "";
  const status = typeof query.filters.status === "string" ? query.filters.status : "";
  const filtered = rows.filter((row) => (portal === "" || row.id.toLowerCase().includes(portal)) && (status === "" || row.status === status));
  if (signal.aborted) return { items: [], total: 0 };
  const start = (query.page - 1) * query.pageSize;
  return { items: filtered.slice(start, start + query.pageSize), total: filtered.length };
}

function failRelease(release: PortalRelease): void {
  if (release.status === "Failed") throw new Error(release.phases.find((phase) => phase.status === "Failed")?.message ?? "Portal 上线失败");
}
