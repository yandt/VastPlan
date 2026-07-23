import type { ArtifactCapacity, ArtifactGCPlan, PlatformAdminClient } from "@vastplan/platform-admin";
import { createBrowserPlatformAdminClient } from "@vastplan/platform-admin";
import { defineCollectionPage, managementServicesFor, message, type CollectionPageDefinition, type WorkbenchFrontendPluginContext } from "@vastplan/workbench-sdk";
import { catalogPage } from "./catalog-page.js";
import { localization } from "./localization.js";
import { migrationPage } from "./migration-page.js";
import { publicationPage } from "./publication-page.js";
import { filterString, formatBytes, lifecycleOptions, namespace, paged, text, type Row } from "./shared.js";

function capacityRows(capacity: ArtifactCapacity): Row[] {
  return capacity.quotas.map((quota) => ({
    id: quota.id,
    scope: [quota.namespace && `namespace=${quota.namespace}`, quota.publisher && `publisher=${quota.publisher}`, quota.channel && `channel=${quota.channel}`].filter(Boolean).join(" · ") || "global",
    artifacts: quota.artifacts,
    bytes: formatBytes(quota.bytes),
    artifactLimit: quota.maxArtifacts ?? "∞",
    byteLimit: quota.maxBytes === undefined ? "∞" : formatBytes(quota.maxBytes),
    state: quota.exceeded ? "exceeded" : "ready",
    exceeded: quota.exceeded,
  }));
}

function capacityPage(client: PlatformAdminClient, id: string, path: string): CollectionPageDefinition<Row> {
  return defineCollectionPage<Row>({
    id, path, title: text("page.capacity.title", "容量与配额"), description: text("page.capacity.description", "查看累积配额、活动用量与隔离占用"),
    navigation: { id, label: text("page.capacity.navigation", "容量与配额"), zone: "settings", groupID: "platform.artifacts", order: 51 },
    collection: {
      id: `${id}.collection`, title: text("panel.quotas", "配额用量"), view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [10, 20, 50] },
      filters: [{ id: "exceeded", label: text("filter.exceeded", "仅超限"), kind: "boolean" }],
      columns: [
        { key: "id", label: text("column.quota", "配额"), defaultVisible: true, minWidth: 160 },
        { key: "scope", label: text("column.scope", "作用范围"), defaultVisible: true, minWidth: 260 },
        { key: "artifacts", label: text("column.artifacts", "制品数"), defaultVisible: true, minWidth: 100 },
        { key: "artifactLimit", label: text("column.artifactLimit", "数量上限"), defaultVisible: true, minWidth: 110 },
        { key: "bytes", label: text("column.bytes", "对象字节"), defaultVisible: true, minWidth: 110 },
        { key: "byteLimit", label: text("column.byteLimit", "字节上限"), defaultVisible: true, minWidth: 110 },
        { key: "state", label: text("column.state", "状态"), defaultVisible: true, minWidth: 110 },
      ],
      preferences: { allowedColumns: ["id", "scope", "artifacts", "artifactLimit", "bytes", "byteLimit", "state"], density: true },
    },
    async load(query, signal) {
      const capacity = await client.artifactRepositoryCapacity();
      if (signal.aborted) throw new DOMException("aborted", "AbortError");
      const onlyExceeded = query.filters.exceeded === true;
      return paged(capacityRows(capacity).filter((row) => !onlyExceeded || row.exceeded === true), query);
    },
    async loadSummary(signal) {
      const capacity = await client.artifactRepositoryCapacity();
      if (signal.aborted) throw new DOMException("aborted", "AbortError");
      return { title: text("panel.capacity", "容量"), metrics: [
        { id: "active", label: text("metric.activeBytes", "活动字节"), value: formatBytes(capacity.activeBytes) },
        { id: "quarantined", label: text("metric.quarantinedBytes", "隔离字节"), value: formatBytes(capacity.quarantinedBytes), tone: capacity.quarantinedArtifacts > 0 ? "warning" : "neutral" },
        { id: "reclaimed", label: text("metric.reclaimedBytes", "已回收"), value: formatBytes(capacity.reclaimedBytes) },
        { id: "stored", label: text("metric.storedBytes", "实际存储"), value: formatBytes(capacity.storedBytes) },
      ] };
    },
  });
}

function referencesPage(client: PlatformAdminClient, id: string, path: string): CollectionPageDefinition<Row> {
  const ownerKinds = ["deployment-active", "assignment-active", "portal-activation", "artifact-lock", "rollback-history", "seed", "last-known-good", "runner-install", "mobile-install"];
  return defineCollectionPage<Row>({
    id, path, title: text("page.references.title", "制品引用"), description: text("page.references.description", "查看阻止垃圾回收的消费者完整快照"),
    navigation: { id, label: text("page.references.navigation", "制品引用"), zone: "settings", groupID: "platform.artifacts", order: 52 },
    collection: {
      id: `${id}.collection`, title: text("panel.references", "引用快照"), view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [10, 20, 50] },
      filters: [{ id: "ownerKind", label: text("filter.ownerKind", "Owner 类型"), kind: "select", options: ownerKinds.map((value) => ({ value, label: text(`owner.${value}`, value) })) }],
      columns: [
        { key: "tenant", label: text("column.tenant", "租户"), defaultVisible: true, minWidth: 120 },
        { key: "ownerKind", label: text("column.ownerKind", "Owner 类型"), defaultVisible: true, minWidth: 170 },
        { key: "ownerId", label: text("column.ownerId", "Owner ID"), defaultVisible: true, minWidth: 260 },
        { key: "publisher", label: text("column.referencePublisher", "可信发布者"), defaultVisible: true, minWidth: 230 },
        { key: "generation", label: text("column.generation", "Generation"), defaultVisible: true, minWidth: 120 },
        { key: "references", label: text("column.referenceCount", "引用数"), defaultVisible: true, minWidth: 100 },
        { key: "expiresAt", label: text("column.expiresAt", "租约到期"), defaultVisible: true, minWidth: 190 },
      ],
      preferences: { allowedColumns: ["tenant", "ownerKind", "ownerId", "publisher", "generation", "references", "expiresAt"], density: true },
    },
    async load(query, signal) {
      const page = await client.listArtifactReferences();
      if (signal.aborted) throw new DOMException("aborted", "AbortError");
      const ownerKind = filterString(query, "ownerKind");
      const rows = page.items.filter((item) => ownerKind === "" || item.value.ownerKind === ownerKind).map((item) => ({
        id: `${item.tenantId}/${item.value.ownerKind}/${item.value.ownerId}`, tenant: item.tenantId, ownerKind: item.value.ownerKind,
        ownerId: item.value.ownerId, publisher: item.publisherId, generation: item.value.generation,
        references: item.value.references.length, expiresAt: item.expiresAt ?? "permanent",
      }));
      return paged(rows, query);
    },
  });
}

function gcBlockerMessage(plan: ArtifactGCPlan): string {
  return (plan.blockers ?? []).map((blocker) => blocker.message).join("；") || "当前没有可隔离制品";
}

function garbageCollectionPage(client: PlatformAdminClient, id: string, path: string): CollectionPageDefinition<Row> {
  return defineCollectionPage<Row>({
    id, path, title: text("page.gc.title", "垃圾回收"), description: text("page.gc.description", "按计划隔离并在宽限期后复核清扫"),
    navigation: { id, label: text("page.gc.navigation", "垃圾回收"), zone: "settings", groupID: "platform.artifacts", order: 53 },
    collection: {
      id: `${id}.collection`, title: text("panel.gc", "隔离与清扫记录"), view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [10, 20, 50] },
      filters: [
        { id: "status", label: text("filter.gcStatus", "回收状态"), kind: "select", options: ["quarantining", "quarantined", "sweeping", "swept"].map((value) => ({ value, label: text(`gc.${value}`, value) })) },
        { id: "lifecycle", label: text("filter.lifecycle", "生命周期"), kind: "select", options: lifecycleOptions.filter((option) => option.value === "yanked" || option.value === "revoked") },
      ],
      columns: [
        { key: "pluginId", label: text("column.plugin", "插件 ID"), defaultVisible: true, minWidth: 270 },
        { key: "version", label: text("column.version", "版本"), defaultVisible: true, minWidth: 150 },
        { key: "channel", label: text("column.channel", "通道"), defaultVisible: true, minWidth: 100 },
        { key: "lifecycle", label: text("column.lifecycle", "生命周期"), defaultVisible: true, minWidth: 110 },
        { key: "status", label: text("column.gcStatus", "回收状态"), defaultVisible: true, minWidth: 130 },
        { key: "size", label: text("column.size", "大小"), defaultVisible: true, minWidth: 100 },
        { key: "sweepAfter", label: text("column.sweepAfter", "最早清扫时间"), defaultVisible: true, minWidth: 190 },
      ],
      actions: [
        { id: "quarantine", label: text("action.quarantine", "隔离当前计划"), icon: "warning", placement: "page.primary", tone: "danger", confirm: text("confirm.quarantine", "将重新生成计划并隔离全部候选，默认宽限期为 72 小时。") },
        { id: "sweep", label: text("action.sweep", "清扫到期制品"), icon: "remove", placement: "page.secondary", tone: "danger", confirm: text("confirm.sweep", "只会删除已超过宽限期且复核无引用的隔离制品。") },
      ],
      preferences: { allowedColumns: ["pluginId", "version", "channel", "lifecycle", "status", "size", "sweepAfter"], density: true },
    },
    async load(query, signal) {
      const status = await client.artifactGarbageCollectionStatus();
      if (signal.aborted) throw new DOMException("aborted", "AbortError");
      const statusFilter = filterString(query, "status"), lifecycle = filterString(query, "lifecycle");
      const rows = (status.items ?? []).filter((item) => (statusFilter === "" || item.status === statusFilter) && (lifecycle === "" || item.lifecycle === lifecycle)).map((item) => ({
        id: `${item.ref.pluginId}@${item.ref.version}/${item.ref.channel}/${item.sha256}`, pluginId: item.ref.pluginId,
        version: item.ref.version, channel: item.ref.channel, lifecycle: item.lifecycle, status: item.status,
        size: formatBytes(item.size), sweepAfter: item.sweepAfter,
      }));
      return paged(rows, query);
    },
    async loadSummary(signal) {
      const plan = await client.planArtifactGarbageCollection();
      if (signal.aborted) throw new DOMException("aborted", "AbortError");
      return { title: text("panel.gcPlan", "当前回收计划"), metrics: [
        { id: "ready", label: text("metric.gcReady", "可执行"), value: plan.ready ? "Ready" : "Blocked", tone: plan.ready ? "success" : "warning" },
        { id: "candidates", label: text("metric.gcCandidates", "候选制品"), value: (plan.candidates ?? []).length },
        { id: "bytes", label: text("metric.gcBytes", "候选字节"), value: formatBytes(plan.bytes) },
        { id: "blockers", label: text("metric.gcBlockers", "阻断原因"), value: plan.ready ? "-" : gcBlockerMessage(plan), tone: plan.ready ? "neutral" : "warning" },
      ] };
    },
    async runAction({ action }) {
      if (action.id === "quarantine") {
        const plan = await client.planArtifactGarbageCollection();
        if (!plan.ready || plan.planId === undefined || (plan.candidates ?? []).length === 0) throw new Error(gcBlockerMessage(plan));
        await client.quarantineArtifacts(plan.planId, 72);
      } else if (action.id === "sweep") {
        await client.sweepArtifacts();
      }
    },
  });
}

export function createArtifactRepositoryPages(client: PlatformAdminClient, serviceID: string, serviceLabel?: string): readonly CollectionPageDefinition<Row>[] {
  const suffix = serviceLabel === undefined ? "" : `/${serviceID}`;
  const base = `/settings/artifacts${suffix}`;
  const idBase = `platform.artifact-repository.${serviceID}`;
  const title = serviceLabel === undefined ? text("page.title", "制品仓库") : message(namespace, "page.titleService", "制品仓库 · {service}", { service: serviceLabel });
  return [
    catalogPage(client, `${idBase}.catalog`, base, title, title),
    capacityPage(client, `${idBase}.capacity`, `${base}/capacity`),
    referencesPage(client, `${idBase}.references`, `${base}/references`),
    garbageCollectionPage(client, `${idBase}.gc`, `${base}/gc`),
    migrationPage(client, `${idBase}.migration`, `${base}/migration`),
    publicationPage(client, `${idBase}.publications`, `${base}/publications`),
  ];
}

export default {
  register(context: WorkbenchFrontendPluginContext) {
    const services = managementServicesFor(context.portal, "platform.artifacts.repository");
    if (services.length === 0) throw new Error("Portal 未绑定 platform.artifacts.repository 服务");
    for (const service of services) {
      const client = createBrowserPlatformAdminClient(context.portal.id, service.id);
      const label = services.length === 1 ? undefined : service.label ?? service.id;
      for (const page of createArtifactRepositoryPages(client, service.id, label)) context.addCollectionPage(page);
    }
  },
  localization,
};
