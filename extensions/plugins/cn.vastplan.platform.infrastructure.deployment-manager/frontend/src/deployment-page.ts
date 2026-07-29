import type { PlatformAdminClient, ServiceAuditEvent, ServiceRevision } from "@vastplan/platform-admin";
import {
  defineCollectionPage,
  type CollectionPageDefinition,
  type CollectionQuery,
  type JSONValue,
  type LocalizedText,
  type WorkbenchFormDefinition,
} from "@vastplan/workbench-sdk";
import { buildBackendIntent, intentEditorValue, serviceIntentSchema } from "./intent-form.js";
import { message } from "./localization.js";
import { dependencyGraphContent, deploymentRow, resolutionContent, type DeploymentRow } from "./resolution-view.js";

export function createDeploymentPage(client: PlatformAdminClient, serviceID: string, path: string, title: LocalizedText): CollectionPageDefinition<DeploymentRow> {
  const form = intentForm(client, "create");
  const edit = intentForm(client, "edit");
  const statusLabels = {
    Draft: message("status.draft", "草稿"), PendingApproval: message("status.pendingApproval", "待审批"), Approved: message("status.approved", "已批准"),
    Publishing: message("status.publishing", "发布中"), Published: message("status.published", "已发布"),
  };
  const planLabels = {
    Resolved: message("plan.resolved", "已解析"), NeedsConfiguration: message("plan.needsConfiguration", "待配置"),
    Invalid: message("plan.invalid", "无效"), Legacy: message("plan.legacy", "历史组合"),
  };
  const kindLabels = { Intent: message("kind.intent", "Application Intent"), Legacy: message("kind.legacy", "历史 Composition") };
  const actionHandlers: Record<string, (revisionID: number) => Promise<unknown>> = {
    "refresh-plan": (revisionID) => client.refreshIntentDraft(revisionID),
    submit: (revisionID) => client.submitServiceDraft(revisionID),
    approve: (revisionID) => client.approveServiceRevision(revisionID),
    publish: (revisionID) => client.publishServiceRevision(revisionID),
    rollback: (revisionID) => client.rollbackServiceRevision(revisionID),
  };
  return defineCollectionPage<DeploymentRow>({
    id: `platform.deployment.${serviceID}`, path, title,
    description: message("page.description", "声明应用意图，由 Planner 派生依赖与运行策略，经审批后发布到 Node Agent 集群"),
    navigation: { id: `platform.deployment.${serviceID}`, label: title, zone: "settings", order: 60 },
    pageActions: [{ id: "create", label: message("action.new", "新建应用意图"), icon: "add", tone: "primary", form: "create" }],
    collection: {
      id: `platform.deployment.${serviceID}`, title, view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20, 50, 100] },
      filterPanel: { fields: [
        { id: "deployment", label: message("column.deployment", "部署"), kind: "text" },
        { id: "status", label: message("column.status", "修订状态"), kind: "select", options: Object.entries(statusLabels).map(([value, label]) => ({ value, label })) },
        { id: "planStatus", label: message("column.planStatus", "规划状态"), kind: "select", options: Object.entries(planLabels).map(([value, label]) => ({ value, label })) },
      ] },
      columns: [
        { key: "id", label: "Revision", format: "number", defaultVisible: true, minWidth: 90 },
        { key: "deployment", label: message("column.deployment", "部署"), defaultVisible: true, minWidth: 180 },
        { key: "status", label: message("column.status", "修订状态"), format: "status", valueLabels: statusLabels, statusTones: { Draft: "neutral", PendingApproval: "warning", Approved: "info", Publishing: "warning", Published: "success" }, defaultVisible: true, minWidth: 110 },
        { key: "planStatus", label: message("column.planStatus", "规划状态"), format: "status", valueLabels: planLabels, statusTones: { Resolved: "success", NeedsConfiguration: "warning", Invalid: "error", Legacy: "neutral" }, defaultVisible: true, minWidth: 120 },
        { key: "planningStale", label: message("column.stale", "计划失效"), format: "boolean", defaultVisible: true, minWidth: 90 },
        { key: "revisionKind", label: message("column.kind", "输入类型"), valueLabels: kindLabels, defaultVisible: false, minWidth: 150 },
        { key: "active", label: message("column.active", "活动"), format: "boolean", defaultVisible: true, minWidth: 80 },
        { key: "updatedAt", label: message("column.updated", "更新时间"), format: "datetime", defaultVisible: true, minWidth: 180 },
      ],
      selection: "single",
      actions: deploymentActions(),
    },
    forms: [form, edit],
    overlays: [
      { id: "resolution", surface: "drawer", size: "lg", title: message("dialog.resolution", "Planner 只读解析结果"), async load(selected) { return resolutionContent(selected[0]); } },
      { id: "graph", surface: "drawer", size: "lg", title: message("dialog.graph", "派生服务依赖图"), async load(selected) { return dependencyGraphContent(selected[0]); } },
      { id: "preview", surface: "dialog", size: "lg", title: message("dialog.preview", "内核校验后的 Deployment v2"), async load(selected) { return { kind: "json", documents: [{ value: (selected[0]?.preview ?? {}) as JSONValue }] }; } },
      { id: "audit", surface: "drawer", size: "lg", title: message("dialog.audit", "服务修订审计"), async load(selected) {
        const rows = selected[0] === undefined ? [] : await client.listServiceRevisionAudit(selected[0].id);
        return { kind: "table", rowKey: "id", rows: rows as Array<ServiceAuditEvent & Record<string, unknown>>, columns: [
          { key: "at", label: message("column.time", "时间"), format: "datetime" }, { key: "action", label: message("column.action", "动作") },
          { key: "actorId", label: message("column.actor", "操作者") }, { key: "planDigest", label: "Plan digest", minWidth: 260 }, { key: "previewDigest", label: "Preview digest", minWidth: 260 },
        ] };
      } },
    ],
    async load(query: CollectionQuery, signal) {
      const deployment = typeof query.filters.deployment === "string" ? query.filters.deployment.trim().toLowerCase() : "";
      const status = typeof query.filters.status === "string" ? query.filters.status : "";
      const planStatus = typeof query.filters.planStatus === "string" ? query.filters.planStatus : "";
      const rows = (await client.listServiceRevisions()).map(deploymentRow).filter((item) =>
        (deployment === "" || item.deployment.toLowerCase().includes(deployment)) && (status === "" || item.status === status) && (planStatus === "" || item.planStatus === planStatus));
      if (signal.aborted) return { items: [], total: 0 };
      const start = Math.max(0, (query.page - 1) * query.pageSize);
      return { items: rows.slice(start, start + query.pageSize), total: rows.length };
    },
    async runAction({ action, selected }) {
      const item = selected[0];
      if (item === undefined) return;
      const handler = actionHandlers[action.id];
      if (handler === undefined) return;
      await handler(item.id);
      return { notify: { title: action.label, kind: "success" } };
    },
  });
}

function intentForm(client: PlatformAdminClient, id: "create" | "edit"): WorkbenchFormDefinition<DeploymentRow> {
  return {
    id,
    schema: serviceIntentSchema([]),
    presentation: {
      layout: "vertical", navigation: "sections",
      sections: [{ id: "intent", title: message("panel.intent", "应用意图"), columns: 1, fields: ["/deployment", "/services"] }],
      fields: [{ pointer: "/deployment" }, { pointer: "/services" }],
    },
    workflow: {
      size: "lg", title: message(id === "create" ? "panel.new" : "panel.edit", id === "create" ? "新建应用意图草稿" : "编辑应用意图草稿"),
      submitLabel: message(id === "create" ? "action.create" : "action.save", id === "create" ? "创建草稿" : "保存草稿"),
      success: { notify: message(id === "create" ? "notice.created" : "notice.saved", id === "create" ? "应用意图草稿已创建" : "应用意图草稿已保存"), refreshCollection: true, close: true },
    },
    async prepare(_selected, signal) {
      const targets = await client.listDeploymentTargets();
      if (signal.aborted) return {};
      return { schema: serviceIntentSchema(targets), ...(id === "create" ? { initialValue: { deployment: targets[0]?.deploymentName, services: [] } } : {}) };
    },
    ...(id === "edit" ? { async load(selected: readonly DeploymentRow[]) { return selected[0] === undefined ? { services: [] } : intentEditorValue(selected[0]); } } : {}),
    async submit({ value, selected }) {
      const current = selected[0];
      const intent = buildBackendIntent(value, current?.intent?.revision ?? 1);
      if (id === "create") await client.createIntentDraft(intent);
      else if (current !== undefined) await client.updateIntentDraft(current.id, intent);
    },
  };
}

function deploymentActions() {
  const intentDraft = { all: [{ pointer: "/status", equals: "Draft" }, { pointer: "/revisionKind", equals: "Intent" }] } as const;
  const intentPending = { all: [{ pointer: "/status", equals: "PendingApproval" }, { pointer: "/revisionKind", equals: "Intent" }] } as const;
  return [
    { id: "edit", label: message("action.edit", "编辑"), icon: "edit", placement: "record.row", form: "edit", visibleWhen: intentDraft },
    { id: "refresh-plan", label: message("action.refreshPlan", "重新解析计划"), icon: "refresh", placement: "record.row", visibleWhen: intentDraft },
    { id: "submit", label: message("action.submit", "提交审批"), icon: "upload", placement: "record.row", confirm: message("confirm.submit", "提交前会再次调用 Planner；只有 Resolved 且未失效的计划才能进入审批。"), visibleWhen: { all: [...intentDraft.all, { pointer: "/planStatus", equals: "Resolved" }, { pointer: "/planningStale", equals: false }] } },
    { id: "approve", label: message("action.approve", "批准"), icon: "success", placement: "record.row", tone: "primary", confirm: message("confirm.approve", "审批人与提交人必须不同，审批将绑定当前计划摘要。"), visibleWhen: intentPending },
    { id: "publish", label: message("action.publish", "发布"), icon: "publish", placement: "record.row", tone: "primary", confirm: message("confirm.publish", "发布前会再次校验计划摘要，之后 Controller 才会调度 Node Agent。"), visibleWhen: { all: [{ pointer: "/status", in: ["Approved", "Publishing"] }, { pointer: "/revisionKind", equals: "Intent" }, { pointer: "/configurationCandidateId", exists: false }] } },
    { id: "rollback", label: message("action.rollback", "回滚到此版本"), icon: "refresh", placement: "record.row", tone: "danger", confirm: message("confirm.rollback", "回滚会从历史 Intent 创建并发布新的单调 revision。"), visibleWhen: { all: [{ pointer: "/status", equals: "Published" }, { pointer: "/active", equals: false }, { pointer: "/revisionKind", equals: "Intent" }] } },
    { id: "resolution", label: message("action.resolution", "解析结果"), icon: "info", placement: "record.row", overlay: "resolution", visibleWhen: { pointer: "/revisionKind", equals: "Intent" } },
    { id: "graph", label: message("action.graph", "依赖图"), icon: "info", placement: "record.row", overlay: "graph", visibleWhen: { pointer: "/revisionKind", equals: "Intent" } },
    { id: "preview", label: message("action.preview", "最终部署预览"), icon: "search", placement: "record.row", overlay: "preview" },
    { id: "audit", label: message("action.audit", "审计记录"), icon: "search", placement: "record.row", overlay: "audit" },
  ] as const;
}
