import type { ArtifactCatalogQuery, ArtifactCapacity, ArtifactGCPlan, PlatformAdminClient } from "@vastplan/platform-admin";
import { createBrowserPlatformAdminClient } from "@vastplan/platform-admin";
import { defineCollectionPage, type CollectionPageDefinition, type CollectionQuery } from "@vastplan/workbench-sdk";
import { managementServicesFor, message, type FrontendPluginContext } from "@vastplan/ui-primitives";

const namespace = "cn.vastplan.platform.artifacts.repository";
type Row = Record<string, unknown>;

const text = (key: string, fallback: string) => message(namespace, key, fallback);
const targetOptions = ["backend", "frontend", "runner", "mobile"].map((value) => ({ value, label: text(`target.${value}`, value) }));
const lifecycleOptions = ["active", "deprecated", "yanked", "revoked"].map((value) => ({ value, label: text(`lifecycle.${value}`, value) }));

function filterString(query: CollectionQuery, key: string): string {
  const value = query.filters[key];
  return typeof value === "string" ? value.trim() : "";
}

function paged(items: readonly Row[], query: CollectionQuery) {
  const start = (query.page - 1) * query.pageSize;
  return { items: items.slice(start, start + query.pageSize), total: items.length };
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value < 0) return "-";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let amount = value, unit = 0;
  while (amount >= 1024 && unit < units.length - 1) { amount /= 1024; unit++; }
  return `${amount >= 10 || unit === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[unit]}`;
}

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

function catalogPage(client: PlatformAdminClient, id: string, path: string, title: ReturnType<typeof text>, navigationLabel: ReturnType<typeof text>): CollectionPageDefinition<Row> {
  return defineCollectionPage<Row>({
    id, path, title, description: text("page.catalog.description", "查询可信制品、发布者、目标内核与生命周期"),
    navigation: { id, label: navigationLabel, zone: "settings", groupID: "platform.artifacts", order: 50 },
    collection: {
      id: `${id}.collection`, title: text("panel.catalog", "制品目录"), view: "table",
      query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [10, 20, 50, 100] },
      filters: [
        { id: "pluginPrefix", label: text("filter.plugin", "插件 ID / 命名空间"), kind: "text" },
        { id: "namespace", label: text("filter.namespace", "命名空间"), kind: "text" },
        { id: "publisher", label: text("filter.publisher", "发布者"), kind: "text" },
        { id: "channel", label: text("filter.channel", "通道"), kind: "select", options: ["stable", "testing"].map((value) => ({ value, label: text(`channel.${value}`, value) })) },
        { id: "target", label: text("filter.target", "目标内核"), kind: "select", options: targetOptions },
        { id: "lifecycle", label: text("filter.lifecycle", "生命周期"), kind: "select", options: lifecycleOptions },
      ],
      columns: [
        { key: "pluginId", label: text("column.plugin", "插件 ID"), defaultVisible: true, minWidth: 270 },
        { key: "version", label: text("column.version", "版本"), defaultVisible: true, minWidth: 150 },
        { key: "channel", label: text("column.channel", "通道"), defaultVisible: true, minWidth: 100 },
        { key: "publisher", label: text("column.publisher", "发布者"), defaultVisible: true, minWidth: 120 },
        { key: "targets", label: text("column.targets", "目标内核"), defaultVisible: true, minWidth: 150 },
        { key: "size", label: text("column.size", "大小"), defaultVisible: true, minWidth: 100 },
        { key: "lifecycle", label: text("column.lifecycle", "生命周期"), defaultVisible: true, minWidth: 120 },
        { key: "revision", label: text("column.revision", "仓库 Revision"), defaultVisible: true, minWidth: 140 },
        { key: "publishedAt", label: text("column.publishedAt", "发布时间"), defaultVisible: true, minWidth: 190 },
      ],
      preferences: { allowedColumns: ["pluginId", "version", "channel", "publisher", "targets", "size", "lifecycle", "revision", "publishedAt"], density: true },
    },
    async load(query, signal) {
      const targetValue = filterString(query, "target");
      const lifecycleValue = filterString(query, "lifecycle");
      const target = targetValue === "" ? undefined : targetValue as NonNullable<ArtifactCatalogQuery["target"]>;
      const lifecycle = lifecycleValue === "" ? undefined : lifecycleValue as NonNullable<ArtifactCatalogQuery["lifecycle"]>;
      const page = await client.listArtifactCatalog({
        pluginPrefix: filterString(query, "pluginPrefix"), namespace: filterString(query, "namespace"), publisher: filterString(query, "publisher"),
        channel: filterString(query, "channel"), ...(target === undefined ? {} : { target }), ...(lifecycle === undefined ? {} : { lifecycle }),
        page: query.page, pageSize: query.pageSize,
      });
      if (signal.aborted) throw new DOMException("aborted", "AbortError");
      return { total: page.total, items: page.items.map((entry) => ({
        id: `${entry.ref.pluginId}@${entry.ref.version}/${entry.ref.channel}`, pluginId: entry.ref.pluginId, version: entry.ref.version,
        channel: entry.ref.channel, publisher: entry.publisher, targets: entry.targets.join(", "), size: formatBytes(entry.size),
        lifecycle: entry.lifecycleStatus, revision: entry.repositoryRevision, publishedAt: entry.publishedAt,
      })) };
    },
    async loadSummary(signal) {
      const [status, capacity] = await Promise.all([client.artifactRepositoryStatus(), client.artifactRepositoryCapacity()]);
      if (signal.aborted) throw new DOMException("aborted", "AbortError");
      const exceeded = capacity.quotas.filter((quota) => quota.exceeded).length;
      return { title: text("panel.overview", "仓库概览"), metrics: [
        { id: "ready", label: text("metric.status", "服务状态"), value: status.ready ? "Ready" : "Unavailable", tone: status.ready ? "success" : "error" },
        { id: "active", label: text("metric.activeArtifacts", "活动制品"), value: capacity.activeArtifacts },
        { id: "stored", label: text("metric.storedBytes", "实际存储"), value: formatBytes(capacity.storedBytes) },
        { id: "quota", label: text("metric.quota", "配额状态"), value: exceeded === 0 ? "Ready" : `${exceeded} exceeded`, tone: exceeded === 0 ? "success" : "error" },
        { id: "provider", label: text("metric.provider", "存储 Provider"), value: status.storageProvider ?? "-" },
        { id: "volume", label: text("metric.volume", "活动 Volume"), value: status.storageVolumeId ?? "-" },
        { id: "revision", label: text("metric.catalogRevision", "Catalog Revision"), value: status.catalog?.revision ?? 0 },
        { id: "migration", label: text("metric.migration", "迁移状态"), value: status.migration?.phase ?? "none", tone: status.migration?.lastError ? "error" : "neutral" },
      ] };
    },
  });
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
        { id: "quarantine", label: text("action.quarantine", "隔离当前计划"), placement: "page.primary", tone: "danger", confirm: text("confirm.quarantine", "将重新生成计划并隔离全部候选，默认宽限期为 72 小时。") },
        { id: "sweep", label: text("action.sweep", "清扫到期制品"), placement: "page.secondary", tone: "danger", confirm: text("confirm.sweep", "只会删除已超过宽限期且复核无引用的隔离制品。") },
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
  ];
}

export default {
  register(context: FrontendPluginContext) {
    const services = managementServicesFor(context.portal, "platform.artifacts.repository");
    if (services.length === 0) throw new Error("Portal 未绑定 platform.artifacts.repository 服务");
    for (const service of services) {
      const client = createBrowserPlatformAdminClient(context.portal.id, service.id);
      const label = services.length === 1 ? undefined : service.label ?? service.id;
      for (const page of createArtifactRepositoryPages(client, service.id, label)) context.addCollectionPage(page);
    }
  },
  localization: { defaultLocale: "zh-CN", messages: {
    "zh-CN": {
      "page.title":"制品仓库","page.titleService":"制品仓库 · {service}","page.catalog.description":"查询可信制品、发布者、目标内核与生命周期","page.capacity.title":"容量与配额","page.capacity.navigation":"容量与配额","page.capacity.description":"查看累积配额、活动用量与隔离占用","page.references.title":"制品引用","page.references.navigation":"制品引用","page.references.description":"查看阻止垃圾回收的消费者完整快照","page.gc.title":"垃圾回收","page.gc.navigation":"垃圾回收","page.gc.description":"按计划隔离并在宽限期后复核清扫",
      "panel.catalog":"制品目录","panel.overview":"仓库概览","panel.quotas":"配额用量","panel.capacity":"容量","panel.references":"引用快照","panel.gc":"隔离与清扫记录","panel.gcPlan":"当前回收计划",
      "filter.plugin":"插件 ID / 命名空间","filter.namespace":"命名空间","filter.publisher":"发布者","filter.channel":"通道","filter.target":"目标内核","filter.lifecycle":"生命周期","filter.exceeded":"仅超限","filter.ownerKind":"Owner 类型","filter.gcStatus":"回收状态",
      "column.plugin":"插件 ID","column.version":"版本","column.channel":"通道","column.publisher":"发布者","column.targets":"目标内核","column.size":"大小","column.lifecycle":"生命周期","column.revision":"仓库 Revision","column.publishedAt":"发布时间","column.quota":"配额","column.scope":"作用范围","column.artifacts":"制品数","column.artifactLimit":"数量上限","column.bytes":"对象字节","column.byteLimit":"字节上限","column.state":"状态","column.tenant":"租户","column.ownerKind":"Owner 类型","column.ownerId":"Owner ID","column.referencePublisher":"可信发布者","column.generation":"Generation","column.referenceCount":"引用数","column.expiresAt":"租约到期","column.gcStatus":"回收状态","column.sweepAfter":"最早清扫时间",
      "metric.status":"服务状态","metric.activeArtifacts":"活动制品","metric.storedBytes":"实际存储","metric.quota":"配额状态","metric.provider":"存储 Provider","metric.volume":"活动 Volume","metric.catalogRevision":"Catalog Revision","metric.migration":"迁移状态","metric.activeBytes":"活动字节","metric.quarantinedBytes":"隔离字节","metric.reclaimedBytes":"已回收","metric.gcReady":"可执行","metric.gcCandidates":"候选制品","metric.gcBytes":"候选字节","metric.gcBlockers":"阻断原因",
      "action.quarantine":"隔离当前计划","action.sweep":"清扫到期制品","confirm.quarantine":"将重新生成计划并隔离全部候选，默认宽限期为 72 小时。","confirm.sweep":"只会删除已超过宽限期且复核无引用的隔离制品。"
    },
    "en-US": {
      "page.title":"Artifact repository","page.titleService":"Artifact repository · {service}","page.catalog.description":"Query trusted artifacts, publishers, targets, and lifecycle","page.capacity.title":"Capacity and quotas","page.capacity.navigation":"Capacity and quotas","page.capacity.description":"Inspect cumulative quotas, active usage, and quarantine storage","page.references.title":"Artifact references","page.references.navigation":"Artifact references","page.references.description":"Inspect complete consumer snapshots that protect artifacts from GC","page.gc.title":"Garbage collection","page.gc.navigation":"Garbage collection","page.gc.description":"Quarantine by plan and sweep only after the grace period",
      "panel.catalog":"Artifact catalog","panel.overview":"Repository overview","panel.quotas":"Quota usage","panel.capacity":"Capacity","panel.references":"Reference snapshots","panel.gc":"Quarantine and sweep records","panel.gcPlan":"Current GC plan",
      "filter.plugin":"Plugin ID / namespace","filter.namespace":"Namespace","filter.publisher":"Publisher","filter.channel":"Channel","filter.target":"Target kernel","filter.lifecycle":"Lifecycle","filter.exceeded":"Exceeded only","filter.ownerKind":"Owner kind","filter.gcStatus":"GC status",
      "column.plugin":"Plugin ID","column.version":"Version","column.channel":"Channel","column.publisher":"Publisher","column.targets":"Target kernels","column.size":"Size","column.lifecycle":"Lifecycle","column.revision":"Repository revision","column.publishedAt":"Published","column.quota":"Quota","column.scope":"Scope","column.artifacts":"Artifacts","column.artifactLimit":"Artifact limit","column.bytes":"Object bytes","column.byteLimit":"Byte limit","column.state":"Status","column.tenant":"Tenant","column.ownerKind":"Owner kind","column.ownerId":"Owner ID","column.referencePublisher":"Trusted publisher","column.generation":"Generation","column.referenceCount":"References","column.expiresAt":"Lease expiry","column.gcStatus":"GC status","column.sweepAfter":"Earliest sweep",
      "metric.status":"Service status","metric.activeArtifacts":"Active artifacts","metric.storedBytes":"Stored bytes","metric.quota":"Quota status","metric.provider":"Storage provider","metric.volume":"Active volume","metric.catalogRevision":"Catalog revision","metric.migration":"Migration","metric.activeBytes":"Active bytes","metric.quarantinedBytes":"Quarantined bytes","metric.reclaimedBytes":"Reclaimed","metric.gcReady":"Executable","metric.gcCandidates":"Candidates","metric.gcBytes":"Candidate bytes","metric.gcBlockers":"Blockers",
      "action.quarantine":"Quarantine current plan","action.sweep":"Sweep due artifacts","confirm.quarantine":"The plan will be regenerated and all candidates quarantined with a 72-hour grace period.","confirm.sweep":"Only artifacts past their grace period and still unreferenced will be deleted."
    }
  } },
};
