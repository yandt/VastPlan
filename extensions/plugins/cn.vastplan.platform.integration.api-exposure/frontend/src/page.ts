import type { PlatformAdminClient } from "@vastplan/platform-admin";
import {
  defineCollectionPage,
  message,
  type CollectionPageDefinition,
  type CollectionQuery,
  type WorkbenchFormDefinition,
} from "@vastplan/workbench-sdk";
import {
  apiExposureFormSchema,
  toDraftRequest,
  type APIExposureRow,
} from "./model";

const namespace = "cn.vastplan.platform.integration.api-exposure";
const text = (key: string, fallback: string) => message(namespace, key, fallback);

const statusLabels = {
  Draft: text("status.draft", "草稿"),
  PendingApproval: text("status.pending", "待审批"),
  Approved: text("status.approved", "已批准"),
  Published: text("status.published", "已发布"),
  Superseded: text("status.superseded", "已替换"),
  Retired: text("status.retired", "已退役"),
};

export function createAPIExposurePage(
  client: PlatformAdminClient,
  serviceID: string,
  serviceLabel?: string,
): CollectionPageDefinition<APIExposureRow> {
  return defineCollectionPage<APIExposureRow>({
    id: `platform.api-exposure.${serviceID}`,
    path: `/settings/api-exposures${serviceLabel === undefined ? "" : `/${serviceID}`}`,
    title: serviceLabel === undefined ? text("page.title", "API 暴露") : `${text("page.title", "API 暴露")} · ${serviceLabel}`,
    description: text("page.description", "治理稳定公开地址、认证权限、资源限制与实现换代"),
    navigation: { id: `platform.api-exposure.${serviceID}`, label: text("page.navigation", "HTTP API"), zone: "settings", groupID: "platform.api-exposure", order: 55 },
    collection: {
      id: `platform.api-exposure.${serviceID}.collection`,
      title: text("panel.revisions", "Exposure Revisions"),
      view: "table",
      query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [10, 20, 50] },
      filters: [
        { id: "name", label: text("filter.name", "名称"), kind: "text" },
        { id: "status", label: text("filter.status", "状态"), kind: "select", options: Object.entries(statusLabels).map(([value, label]) => ({ value, label })) },
      ],
      columns: [
        { key: "id", label: "Revision", format: "number", defaultVisible: true },
        { key: "displayName", label: text("column.name", "名称"), defaultVisible: true, minWidth: 180 },
        { key: "routeKey", label: "Route Key", defaultVisible: true, minWidth: 190 },
        { key: "contract", label: text("column.contract", "Contract"), defaultVisible: true, minWidth: 220 },
        { key: "hosts", label: "Hosts", defaultVisible: true, minWidth: 220 },
        { key: "status", label: text("column.status", "状态"), format: "status", valueLabels: statusLabels, statusTones: { Draft: "neutral", PendingApproval: "warning", Approved: "info", Published: "success", Superseded: "neutral", Retired: "error" }, defaultVisible: true },
        { key: "updatedAt", label: text("column.updated", "更新时间"), format: "datetime", defaultVisible: true, minWidth: 180 },
      ],
      selection: "single",
      actions: [
        { id: "create", label: text("action.create", "新建草稿"), icon: "add", placement: "page.primary", tone: "primary", form: "create" },
        { id: "submit", label: text("action.submit", "提交审批"), icon: "upload", placement: "page.secondary", requiresSelection: true, visibleWhen: { pointer: "/status", equals: "Draft" } },
        { id: "approve", label: text("action.approve", "审批"), icon: "success", placement: "page.secondary", requiresSelection: true, visibleWhen: { pointer: "/status", equals: "PendingApproval" } },
        { id: "publish", label: text("action.publish", "发布"), icon: "publish", placement: "page.secondary", tone: "primary", requiresSelection: true, visibleWhen: { pointer: "/status", equals: "Approved" } },
        { id: "retire", label: text("action.retire", "退役"), icon: "remove", placement: "page.secondary", tone: "danger", requiresSelection: true, confirm: text("confirm.retire", "退役后公开 Route Key 永久墓碑化，不会重新分配。"), visibleWhen: { pointer: "/status", equals: "Published" } },
      ],
      preferences: { allowedColumns: ["id", "displayName", "routeKey", "contract", "hosts", "status", "updatedAt"], density: true },
    },
    forms: [createForm(client)],
    load: (query, signal) => loadRows(client, query, signal),
    async runAction({ action, selected }) {
      const row = selected[0];
      if (row === undefined) return;
      if (action.id === "submit") await client.submitAPIExposure(row.id);
      else if (action.id === "approve") await client.approveAPIExposure(row.id);
      else if (action.id === "publish") await client.publishAPIExposure(row.id);
      else if (action.id === "retire") await client.retireAPIExposure(row.exposure.id);
      return { notify: { title: action.label, kind: "success" } };
    },
  });
}

function createForm(client: PlatformAdminClient): WorkbenchFormDefinition<APIExposureRow> {
  return {
    id: "create",
    schema: apiExposureFormSchema,
    presentation: {
      layout: "vertical",
      navigation: "sections",
      sections: [
        { id: "contract", title: text("section.contract", "可信 Contract"), columns: 2, fields: ["/displayName", "/pluginId", "/artifactSha256", "/contributionId"] },
        { id: "binding", title: text("section.binding", "公开绑定"), columns: 2, fields: ["/portalId", "/hosts", "/authenticationProfileId", "/allowAnonymous", "/requiredPermissions"] },
        { id: "limits", title: text("section.limits", "资源与路由"), columns: 2, fields: ["/maxBodyBytes", "/maxResponseBytes", "/requestsPerMinute", "/timeoutMs", "/logicalService", "/routingDomain"] },
      ],
      fields: [],
    },
    workflow: {
      surface: "drawer",
      size: "lg",
      title: text("action.create", "新建 API Exposure 草稿"),
      submitLabel: text("action.save", "创建草稿"),
      success: { notify: text("notice.created", "草稿已创建"), refreshCollection: true, close: true },
    },
    initialValue: { hosts: [], allowAnonymous: false, requiredPermissions: [], maxBodyBytes: 1_048_576, maxResponseBytes: 4_194_304, requestsPerMinute: 60, timeoutMs: 10_000 },
    async submit({ value }) { await client.createAPIExposureDraft(toDraftRequest(value)); },
  };
}

async function loadRows(client: PlatformAdminClient, query: CollectionQuery, signal: AbortSignal) {
  const rows: APIExposureRow[] = (await client.listAPIExposures()).map((row) => ({
    ...row,
    displayName: row.exposure.displayName,
    routeKey: row.exposure.routeKey,
    contract: `${row.exposure.contract.contractId}@${row.exposure.contract.contractVersion}`,
    hosts: row.exposure.hosts.join(", "),
  }));
  if (signal.aborted) throw new DOMException("aborted", "AbortError");
  const name = typeof query.filters.name === "string" ? query.filters.name.toLowerCase() : "";
  const status = typeof query.filters.status === "string" ? query.filters.status : "";
  const filtered = rows.filter((row) => (name === "" || row.exposure.displayName.toLowerCase().includes(name)) && (status === "" || row.status === status));
  const start = (query.page - 1) * query.pageSize;
  return { items: filtered.slice(start, start + query.pageSize), total: filtered.length };
}
