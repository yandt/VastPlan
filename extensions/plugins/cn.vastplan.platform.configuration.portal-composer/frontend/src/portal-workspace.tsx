import { PortalControlClient, type Portal, type PortalAuditEvent, type PortalConfiguration, type PortalRelease, type PortalVersion } from "@vastplan/ui-primitives";
import { defineCollectionPage, message, type CollectionPageDefinition, type CollectionQuery, type JSONValue, type WorkbenchFormDefinition } from "@vastplan/workbench-sdk";
import { buildPortalConfiguration, configurationToForm, portalConfigurationSchema, profileSummary } from "./portal-form";

const namespace = "cn.vastplan.platform.configuration.portal-composer";
type PortalRow = Record<string, unknown> & { id: string; portal: Portal; version: PortalVersion; versionId: number; versionNumber: number; status: PortalVersion["status"]; route: string; renderer: string; layout: string; currentReleaseId: number; releaseVersionId: number; releaseAvailable: boolean; hasRollback: boolean; updatedAt: string };

const statusLabels = { Draft: "草稿", PendingApproval: "待审批", Approved: "已批准", Published: "已发布" };

export function createPortalPage(client: PortalControlClient): CollectionPageDefinition<PortalRow> {
  return defineCollectionPage<PortalRow>({
    id: "platform.portal-composer",
    path: "/settings/portals",
    title: message(namespace, "page.title", "Portal 管理"),
    description: message(namespace, "page.description", "一个 Portal 管理完整配置、版本历史与上线记录"),
    navigation: { id: "platform.portal-composer", label: message(namespace, "page.navigation", "Portal 管理"), zone: "settings", order: 11 },
    collection: {
      id: "portals", title: message(namespace, "collection.title", "Portal"), view: "table",
      query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [10, 20, 50] },
      filterPanel: { fields: [{ id: "portal", label: "Portal", kind: "text" }, { id: "status", label: "状态", kind: "select", options: Object.entries(statusLabels).map(([value, label]) => ({ value, label })) }] },
      columns: [
        { key: "id", label: "Portal", defaultVisible: true, minWidth: 180 },
        { key: "versionNumber", label: "版本", format: "number", defaultVisible: true },
        { key: "status", label: "版本状态", format: "status", valueLabels: statusLabels, statusTones: { Draft: "neutral", PendingApproval: "warning", Approved: "info", Published: "success" }, defaultVisible: true },
        { key: "route", label: "访问路径", defaultVisible: true },
        { key: "renderer", label: "UI 框架", defaultVisible: true },
        { key: "layout", label: "布局", defaultVisible: true },
        { key: "currentReleaseId", label: "当前上线", format: "number", defaultVisible: true },
        { key: "updatedAt", label: "更新时间", format: "datetime", defaultVisible: true, minWidth: 180 },
      ],
      selection: "single",
      preferences: { allowedColumns: ["id", "versionNumber", "status", "route", "renderer", "layout", "currentReleaseId", "updatedAt"], density: true },
      actions: portalActions(),
    },
    pageActions: [{ id: "portal.create", label: message(namespace, "action.createPortal", "新建 Portal"), icon: "add", tone: "primary", form: "create" }],
    forms: [portalForm(client, "create"), portalForm(client, "edit"), portalForm(client, "new-version")],
    overlays: [versionHistory(), releaseHistory(), auditHistory(client), configurationPreview()],
    async load(query, signal) { return loadPortals(client, query, signal); },
    async loadSummary() { const { portals } = await client.governance(); return { title: "Portal 状态", metrics: [{ id: "portals", label: "Portal", value: portals.length }, { id: "online", label: "已上线", value: portals.filter((portal) => portal.currentReleaseId !== undefined).length, tone: "success" }] }; },
    async runAction({ action, selected }) {
      const row = selected[0];
      if (row === undefined) return;
      if (action.id === "portal.submit") await client.transitionPortalVersion(row.id, row.versionId, "submit");
      else if (action.id === "portal.approve") await client.transitionPortalVersion(row.id, row.versionId, "approve");
      else if (action.id === "portal.publish") await client.transitionPortalVersion(row.id, row.versionId, "publish");
      else if (action.id === "portal.delete") await client.deletePortalVersion(row.id, row.versionId);
      else if (action.id === "portal.release") {
        const release = await client.releasePortalVersion(row.id, { portalVersionId: row.releaseVersionId, expectedCurrentReleaseId: row.currentReleaseId, reason: "管理员从 Portal 管理页上线" });
        failRelease(release);
      } else if (action.id === "portal.rollback") {
        const source = row.portal.releases.find((release) => release.status === "Superseded");
        if (source === undefined) throw new Error("没有可回滚的历史上线记录");
        failRelease(await client.rollbackPortalRelease(row.id, source.id, row.currentReleaseId, "管理员从 Portal 管理页回滚"));
      } else return;
      return { notify: { title: action.label, kind: "success" } };
    },
  });
}

function portalForm(client: PortalControlClient, kind: "create" | "edit" | "new-version"): WorkbenchFormDefinition<PortalRow> {
  return {
    id: kind,
    schema: portalConfigurationSchema,
    presentation: { layout: "vertical", navigation: "sections", sections: [
      { id: "identity", title: "Portal", columns: 2, fields: ["/portalId", "/route", "/domains", "/audience"] },
      { id: "platform", title: "平台与界面", columns: 2, fields: ["/defaultRenderer", "/allowedRenderers", "/userSelectableRenderer", "/defaultTemplate", "/pageBodyWidth", "/navigationGroups"] },
      { id: "application", title: "功能与服务", columns: 1, fields: ["/applicationPlugins", "/branding", "/config", "/services"] },
    ], fields: [{ pointer: "/applicationPlugins" }, { pointer: "/branding" }, { pointer: "/config" }, { pointer: "/services" }] },
    workflow: { size: "lg", title: kind === "create" ? "新建 Portal" : kind === "edit" ? "编辑 Portal 草稿" : "创建 Portal 新版本", submitLabel: kind === "edit" ? "保存" : "创建", success: { notify: "Portal 配置已保存", refreshCollection: true, close: true } },
    async prepare(selected, signal) {
      const row = selected[0];
      if (row !== undefined) return { initialValue: configurationToForm(row.id, row.version.configuration) };
      const { portals } = await client.governance();
      if (signal.aborted) throw new DOMException("Portal template request cancelled", "AbortError");
      const source = portals.flatMap((portal) => portal.versions).sort((a, b) => b.id - a.id)[0];
      if (source === undefined) throw new Error("当前没有可复制的平台配置模板");
      return { initialValue: { ...configurationToForm("", source.configuration), portalId: "", route: "/" } };
    },
    async submit({ value, selected }) {
      const row = selected[0];
      const base = row?.version.configuration ?? await latestConfiguration(client);
      const configuration = buildPortalConfiguration(base, value);
      const portalId = typeof value.portalId === "string" ? value.portalId : row?.id ?? "";
      if (kind === "create") await client.createPortal(portalId, configuration);
      else if (kind === "new-version" && row !== undefined) await client.createPortalVersion(row.id, configuration);
      else if (row !== undefined) await client.updatePortalVersion(row.id, row.versionId, configuration);
    },
  };
}

function portalActions() {
  return [
    { id: "portal.edit", label: "编辑", icon: "edit" as const, placement: "record.row" as const, form: "edit", visibleWhen: { pointer: "/status", equals: "Draft" } },
    { id: "portal.delete", label: "删除草稿", icon: "remove" as const, placement: "record.row" as const, tone: "danger" as const, confirm: "确定删除这个未提交版本？", visibleWhen: { pointer: "/status", equals: "Draft" } },
    { id: "portal.submit", label: "提交审批", icon: "upload" as const, placement: "record.row" as const, visibleWhen: { pointer: "/status", equals: "Draft" } },
    { id: "portal.approve", label: "批准", icon: "success" as const, placement: "record.row" as const, tone: "primary" as const, visibleWhen: { pointer: "/status", equals: "PendingApproval" } },
    { id: "portal.publish", label: "发布版本", icon: "publish" as const, placement: "record.row" as const, tone: "primary" as const, confirm: "发布只冻结该版本，不会改变线上 Portal。", visibleWhen: { pointer: "/status", equals: "Approved" } },
    { id: "portal.release", label: "上线", icon: "publish" as const, placement: "record.row" as const, tone: "primary" as const, confirm: "将最近一个尚未上线的已发布版本上线。", visibleWhen: { pointer: "/releaseAvailable", equals: true } },
    { id: "portal.newVersion", label: "新建版本", icon: "copy" as const, placement: "record.row" as const, form: "new-version", visibleWhen: { pointer: "/status", equals: "Published" } },
    { id: "portal.rollback", label: "回滚", icon: "refresh" as const, placement: "record.row" as const, tone: "danger" as const, confirm: "从最近的历史版本创建一条新上线记录。", visibleWhen: { pointer: "/hasRollback", equals: true } },
    { id: "portal.versions", label: "版本历史", icon: "info" as const, placement: "record.row" as const, overlay: "versions" },
    { id: "portal.releases", label: "上线历史", icon: "info" as const, placement: "record.row" as const, overlay: "releases" },
    { id: "portal.audit", label: "审计记录", icon: "info" as const, placement: "record.row" as const, overlay: "audit" },
    { id: "portal.preview", label: "完整配置", icon: "search" as const, placement: "record.row" as const, overlay: "configuration" },
  ];
}

function versionHistory() { return { id: "versions", surface: "dialog" as const, size: "lg" as const, title: "版本历史", async load(selected: readonly PortalRow[]) { return { kind: "table" as const, rowKey: "id", rows: (selected[0]?.portal.versions ?? []) as Array<PortalVersion & Record<string, unknown>>, columns: [{ key: "number", label: "版本", format: "number" as const }, { key: "status", label: "状态", format: "status" as const, valueLabels: statusLabels }, { key: "updatedAt", label: "更新时间", format: "datetime" as const }] }; } }; }
function releaseHistory() { return { id: "releases", surface: "dialog" as const, size: "lg" as const, title: "上线历史", async load(selected: readonly PortalRow[]) { return { kind: "table" as const, rowKey: "id", rows: (selected[0]?.portal.releases ?? []) as Array<PortalRelease & Record<string, unknown>>, columns: [{ key: "id", label: "上线编号", format: "number" as const }, { key: "portalVersionId", label: "PortalVersion", format: "number" as const }, { key: "status", label: "状态", format: "status" as const }, { key: "createdAt", label: "时间", format: "datetime" as const }] }; } }; }
function auditHistory(client: PortalControlClient) { return { id: "audit", surface: "drawer" as const, size: "lg" as const, title: "审计记录", async load(selected: readonly PortalRow[]) { const row = selected[0]; const rows = row === undefined ? [] : await client.auditPortalVersion(row.id, row.versionId); return { kind: "table" as const, rowKey: "id", rows: rows as Array<PortalAuditEvent & Record<string, unknown>>, columns: [{ key: "at", label: "时间", format: "datetime" as const }, { key: "action", label: "动作" }, { key: "actorId", label: "操作者" }] }; } }; }
function configurationPreview() { return { id: "configuration", surface: "drawer" as const, size: "lg" as const, title: "完整配置", async load(selected: readonly PortalRow[]) { return { kind: "json" as const, documents: selected[0] === undefined ? [] : [{ value: selected[0].version.configuration as unknown as JSONValue }] }; } }; }

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
function toPortalRow(portal: Portal): PortalRow[] {
  const version = portal.versions[0];
  if (version === undefined) return [];
  const summary = profileSummary(version.configuration.platform);
  const current = portal.releases.find((release) => release.status === "Current");
  const releaseVersion = portal.versions.find((candidate) => candidate.status === "Published" && candidate.id !== current?.portalVersionId);
  return [{
    id: portal.id,
    portal,
    version,
    versionId: version.id,
    versionNumber: version.number,
    status: version.status,
    route: version.configuration.application.route,
    renderer: summary.renderer,
    layout: summary.layout,
    currentReleaseId: portal.currentReleaseId ?? 0,
    releaseVersionId: releaseVersion?.id ?? 0,
    releaseAvailable: releaseVersion !== undefined,
    hasRollback: portal.releases.some((release) => release.status === "Superseded"),
    updatedAt: portal.updatedAt,
  }];
}
async function latestConfiguration(client: PortalControlClient): Promise<PortalConfiguration> { const { portals } = await client.governance(); const version = portals.flatMap((portal) => portal.versions).sort((a, b) => b.id - a.id)[0]; if (version === undefined) throw new Error("当前没有可复制的平台配置模板"); return version.configuration; }
function failRelease(release: PortalRelease): void { if (release.status === "Failed") throw new Error(release.phases.find((phase) => phase.status === "Failed")?.message ?? "Portal 上线失败"); }
