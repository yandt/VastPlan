import type { PlatformAdminClient } from "@vastplan/platform-admin";
import {
  defineCollectionPage,
  message,
  type CollectionPageDefinition,
  type CollectionQuery,
  type WorkbenchFormDefinition,
} from "@vastplan/workbench-sdk";
import {
  dataPlaneExposureFormSchema,
  toDataPlaneDraftRequest,
  type DataPlaneExposureRow,
} from "./model";

const namespace = "cn.vastplan.platform.integration.api-exposure";
const text = (key: string, fallback: string) => message(namespace, key, fallback);
const statusLabels = { Draft: "草稿", PendingApproval: "待审批", Approved: "已批准", Published: "已发布", Superseded: "已替换", Retired: "已退役" };

export function createDataPlaneExposurePage(client: PlatformAdminClient, serviceID: string): CollectionPageDefinition<DataPlaneExposureRow> {
  return defineCollectionPage<DataPlaneExposureRow>({
    id: `platform.data-plane-exposure.${serviceID}`,
    path: `/settings/api-exposures/${serviceID}/data-planes`,
    title: text("dataPlane.title", "数据面暴露"),
    description: text("dataPlane.description", "治理独立 HTTPS 数据面、短时 Endpoint Lease 与一次性 Ticket"),
    navigation: { id: `platform.data-plane-exposure.${serviceID}`, label: text("dataPlane.navigation", "数据面暴露"), zone: "settings", groupID: "platform.api-exposure", order: 56 },
    collection: {
      id: `platform.data-plane-exposure.${serviceID}.collection`, title: "Data Plane Revisions", view: "table",
      query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [10, 20, 50] },
      filters: [
        { id: "routeKey", label: "Route Key", kind: "text" },
        { id: "status", label: "状态", kind: "select", options: Object.entries(statusLabels).map(([value, label]) => ({ value, label })) },
      ],
      columns: [
        { key: "id", label: "Revision", format: "number", defaultVisible: true },
        { key: "routeKey", label: "Route Key", minWidth: 190, defaultVisible: true },
        { key: "service", label: "Data Plane Service", minWidth: 240, defaultVisible: true },
        { key: "modes", label: "模式", minWidth: 220, defaultVisible: true },
        { key: "hosts", label: "Hosts", minWidth: 220, defaultVisible: true },
        { key: "status", label: "状态", format: "status", valueLabels: statusLabels, defaultVisible: true },
        { key: "updatedAt", label: "更新时间", format: "datetime", minWidth: 180, defaultVisible: true },
      ],
      selection: "single",
      actions: [
        { id: "create", label: "新建草稿", placement: "page.primary", tone: "primary", form: "create" },
        { id: "submit", label: "提交审批", placement: "page.secondary", requiresSelection: true, visibleWhen: { pointer: "/status", equals: "Draft" } },
        { id: "approve", label: "审批", placement: "page.secondary", requiresSelection: true, visibleWhen: { pointer: "/status", equals: "PendingApproval" } },
        { id: "publish", label: "发布", placement: "page.secondary", tone: "primary", requiresSelection: true, visibleWhen: { pointer: "/status", equals: "Approved" } },
        { id: "retire", label: "退役", placement: "page.secondary", tone: "danger", requiresSelection: true, confirm: "退役会立即撤销 Lease 和未消费 Ticket，并永久墓碑化 Route Key。", visibleWhen: { pointer: "/status", equals: "Published" } },
      ],
      preferences: { allowedColumns: ["id", "routeKey", "service", "modes", "hosts", "status", "updatedAt"], density: true },
    },
    forms: [createForm(client)],
    load: (query, signal) => loadRows(client, query, signal),
    async runAction({ action, selected }) {
      const row = selected[0];
      if (row === undefined) return;
      if (action.id === "submit") await client.submitDataPlaneExposure(row.id);
      else if (action.id === "approve") await client.approveDataPlaneExposure(row.id);
      else if (action.id === "publish") await client.publishDataPlaneExposure(row.id);
      else if (action.id === "retire") await client.retireDataPlaneExposure(row.exposure.id);
      return { notify: { title: action.label, kind: "success" } };
    },
  });
}

function createForm(client: PlatformAdminClient): WorkbenchFormDefinition<DataPlaneExposureRow> {
  return {
    id: "create", schema: dataPlaneExposureFormSchema,
    presentation: { layout: "vertical", navigation: "sections", sections: [
      { id: "service", title: "可信数据面服务", columns: 2, fields: ["/pluginId", "/artifactSha256", "/contributionId"] },
      { id: "binding", title: "入口与权限", columns: 2, fields: ["/hosts", "/allowedModes", "/allowedEndpointOrigins", "/tlsIdentityPrefix", "/authenticationProfileId", "/allowAnonymous", "/requiredPermissions", "/maxObjectBytes"] },
    ], fields: [] },
    workflow: { surface: "drawer", size: "lg", title: "新建 Data Plane Exposure 草稿", submitLabel: "创建草稿", success: { notify: "数据面草稿已创建", refreshCollection: true, close: true } },
    initialValue: { hosts: [], allowedModes: ["ticket-redirect"], allowedEndpointOrigins: [], allowAnonymous: false, requiredPermissions: [], maxObjectBytes: 268_435_456 },
    async submit({ value }) { await client.createDataPlaneExposureDraft(toDataPlaneDraftRequest(value)); },
  };
}

async function loadRows(client: PlatformAdminClient, query: CollectionQuery, signal: AbortSignal) {
  const rows: DataPlaneExposureRow[] = (await client.listDataPlaneExposures()).map((row) => ({ ...row, routeKey: row.exposure.routeKey, service: `${row.exposure.service.pluginId}/${row.exposure.service.contributionId}`, modes: row.exposure.allowedModes.join(", "), hosts: row.exposure.hosts.join(", ") }));
  if (signal.aborted) throw new DOMException("aborted", "AbortError");
  const routeKey = typeof query.filters.routeKey === "string" ? query.filters.routeKey.toLowerCase() : "";
  const status = typeof query.filters.status === "string" ? query.filters.status : "";
  const filtered = rows.filter((row) => (routeKey === "" || row.exposure.routeKey.includes(routeKey)) && (status === "" || row.status === status));
  const start = (query.page - 1) * query.pageSize;
  return { items: filtered.slice(start, start + query.pageSize), total: filtered.length };
}
