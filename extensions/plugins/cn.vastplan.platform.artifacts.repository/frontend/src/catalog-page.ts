import type { ArtifactCatalogQuery, PlatformAdminClient } from "@vastplan/platform-admin";
import { defineCollectionPage, type CollectionPageDefinition } from "@vastplan/workbench-sdk";
import { lifecycleForm } from "./lifecycle-form.js";
import { evidenceOverlay, publicationForm } from "./publication-workflow.js";
import { filterString, formatBytes, lifecycleOptions, targetOptions, text, type Row } from "./shared.js";
import type { AssessmentReportDownloader } from "./assessment-report-downloader.js";

export function catalogPage(client: PlatformAdminClient, id: string, path: string, title: ReturnType<typeof text>, navigationLabel: ReturnType<typeof text>, reports?: AssessmentReportDownloader): CollectionPageDefinition<Row> {
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
        { key: "lifecycle", label: text("column.lifecycle", "生命周期"), format: "status", valueLabels: Object.fromEntries(lifecycleOptions.map((option) => [option.value, option.label])), statusTones: { active: "success", deprecated: "warning", yanked: "error", revoked: "error" }, defaultVisible: true, minWidth: 120 },
        { key: "sbom", label: text("column.sbom", "SBOM"), format: "status", valueLabels: { bound: text("sbom.bound", "已绑定"), missing: text("sbom.missing", "未提供") }, statusTones: { bound: "success", missing: "neutral" }, defaultVisible: true, minWidth: 110 },
        { key: "pythonLock", label: text("column.pythonLock", "Python 锁"), format: "status", valueLabels: { bound: text("pythonLock.bound", "已闭合"), missing: text("pythonLock.missing", "不适用") }, statusTones: { bound: "success", missing: "neutral" }, defaultVisible: false, minWidth: 120 },
        { key: "provenance", label: text("column.provenance", "来源证明"), format: "status", valueLabels: { verified: text("provenance.verified", "已验证"), missing: text("provenance.missing", "未提供") }, statusTones: { verified: "success", missing: "neutral" }, defaultVisible: true, minWidth: 120 },
		{ key: "security", label: text("column.security", "安全准入"), format: "status", valueLabels: { passed: text("security.passed", "复扫通过"), failed: text("security.failed", "复扫失败"), stale: text("security.stale", "复扫过期"), admitted: text("security.admitted", "准入通过"), missing: text("security.missing", "未提供") }, statusTones: { passed: "success", failed: "error", stale: "error", admitted: "success", missing: "neutral" }, defaultVisible: true, minWidth: 120 },
        { key: "repositoryRevision", label: text("column.revision", "仓库 Revision"), format: "number", defaultVisible: true, minWidth: 140 },
        { key: "publishedAt", label: text("column.publishedAt", "发布时间"), format: "datetime", defaultVisible: true, minWidth: 190 },
      ],
      actions: [
        { id: "lifecycle", label: text("action.lifecycle", "变更生命周期"), placement: "record.row", form: "lifecycle", requiredPermissions: ["platform.artifacts.lifecycle"], visibleWhen: { not: { pointer: "/lifecycle", equals: "revoked" } } },
        { id: "publication", label: text("action.publication.submit", "提交发布审批"), placement: "record.row", form: "publication", requiredPermissions: ["platform.artifacts.publication.submit"], visibleWhen: { pointer: "/channel", equals: "testing" } },
        { id: "evidence", label: text("action.evidence", "供应链证据"), placement: "record.row", overlay: "evidence", requiredPermissions: ["platform.artifacts.read"] },
        ...(reports === undefined ? [] : [
          { id: "vulnerabilityReport", label: text("action.vulnerabilityReport", "下载漏洞报告"), placement: "record.row" as const, requiredPermissions: ["platform.artifacts.assessment.report.read"], visibleWhen: { not: { pointer: "/security", equals: "missing" } } },
          { id: "licenseReport", label: text("action.licenseReport", "下载许可证报告"), placement: "record.row" as const, requiredPermissions: ["platform.artifacts.assessment.report.read"], visibleWhen: { not: { pointer: "/security", equals: "missing" } } },
        ]),
      ],
      preferences: { allowedColumns: ["pluginId", "version", "channel", "publisher", "targets", "size", "lifecycle", "sbom", "pythonLock", "provenance", "security", "repositoryRevision", "publishedAt"], density: true },
    },
    forms: [lifecycleForm(client), publicationForm(client)],
    overlays: [evidenceOverlay(client)],
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
        id: `${entry.ref.pluginId}@${entry.ref.version}/${entry.ref.channel}`,
        pluginId: entry.ref.pluginId, version: entry.ref.version, channel: entry.ref.channel,
        publisher: entry.publisher, targets: entry.targets.join(", "), size: formatBytes(entry.size),
        sbom: entry.sbom === undefined ? "missing" : "bound",
        pythonLock: entry.pythonLock === undefined ? "missing" : "bound",
        provenance: entry.provenance === undefined ? "missing" : "verified",
		security: entry.securityStatus !== undefined && Date.parse(entry.securityStatus.expiresAt) <= Date.now() ? "stale" : entry.securityStatus?.decision === "fail" ? "failed" : entry.securityStatus?.decision === "pass" ? "passed" : entry.securityAdmission?.decision === "pass" ? "admitted" : "missing",
        lifecycle: entry.lifecycleStatus, lifecycleReason: entry.lifecycleReason ?? "",
        replacementPluginId: entry.replacement?.pluginId ?? "", replacementConstraint: entry.replacement?.constraint ?? "",
        catalogRevision: page.revision, repositoryRevision: entry.repositoryRevision, publishedAt: entry.publishedAt,
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
        { id: "security", label: text("metric.security", "安全准入"), value: status.securityAssessment === undefined ? "-" : status.securityAssessment.alert ? `${status.securityAssessment.rescanFailed + status.securityAssessment.stale + status.securityAssessment.invalid} alerts` : "Ready", tone: status.securityAssessment === undefined ? "neutral" : status.securityAssessment.alert ? "error" : "success" },
        { id: "provider", label: text("metric.provider", "存储 Provider"), value: status.storageProvider ?? "-" },
        { id: "volume", label: text("metric.volume", "活动 Volume"), value: status.storageVolumeId ?? "-" },
        { id: "revision", label: text("metric.catalogRevision", "Catalog Revision"), value: status.catalog?.revision ?? 0 },
        { id: "migration", label: text("metric.migration", "迁移状态"), value: status.migration?.phase ?? "none", tone: status.migration?.lastError ? "error" : "neutral" },
      ] };
    },
    async runAction({ action, selected }) {
      if (reports === undefined || (action.id !== "vulnerabilityReport" && action.id !== "licenseReport")) return;
      const row = selected[0];
      if (row === undefined) return;
      await reports.download({ pluginId: String(row.pluginId), version: String(row.version), channel: String(row.channel) }, action.id === "vulnerabilityReport" ? "vulnerability" : "license");
    },
  });
}
