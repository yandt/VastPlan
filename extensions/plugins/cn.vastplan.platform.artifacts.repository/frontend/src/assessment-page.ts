import type { PlatformAdminClient } from "@vastplan/platform-admin";
import { defineCollectionPage, type CollectionPageDefinition } from "@vastplan/workbench-sdk";
import { paged, text, type Row } from "./shared.js";

export function assessmentPage(client: PlatformAdminClient, id: string, path: string): CollectionPageDefinition<Row> {
  return defineCollectionPage<Row>({
    id, path, title: text("page.assessment.title", "安全评估"),
    description: text("page.assessment.description", "查看仓库已接受的扫描数据库 revision 与报告归档状态；Source/Provider/Controller 配置统一在插件配置中审批发布"),
    requiredPermissions: ["platform.artifacts.read"],
    navigation: { id, label: text("page.assessment.navigation", "安全评估"), zone: "settings", groupID: "platform.artifacts", order: 54 },
    collection: {
      id: `${id}.collection`, title: text("panel.assessmentRevisions", "已使用数据库 Revision"), view: "table",
      query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [10, 20, 50, 100] },
      columns: [
        { key: "databaseRevision", label: text("column.databaseRevision", "数据库 Revision"), defaultVisible: true, minWidth: 360 },
        { key: "artifacts", label: text("column.artifacts", "制品数"), format: "number", defaultVisible: true, minWidth: 100 },
        { key: "current", label: text("column.current", "当前有效"), format: "number", defaultVisible: true, minWidth: 100 },
        { key: "failed", label: text("column.failed", "失败"), format: "number", defaultVisible: true, minWidth: 90 },
        { key: "stale", label: text("column.stale", "过期"), format: "number", defaultVisible: true, minWidth: 90 },
        { key: "invalid", label: text("column.invalid", "无效"), format: "number", defaultVisible: true, minWidth: 90 },
        { key: "lastEvaluatedAt", label: text("column.lastEvaluatedAt", "最近评估"), format: "datetime", defaultVisible: true, minWidth: 190 },
      ],
      preferences: { allowedColumns: ["databaseRevision", "artifacts", "current", "failed", "stale", "invalid", "lastEvaluatedAt"], density: true },
    },
    async load(query, signal) {
      const inventory = await client.artifactAssessmentInventory();
      if (signal.aborted) throw new DOMException("aborted", "AbortError");
      return paged(inventory.revisions.map((revision) => ({ id: revision.databaseRevision, ...revision })), query);
    },
    async loadSummary(signal) {
      const [inventory, repository] = await Promise.all([client.artifactAssessmentInventory(), client.artifactRepositoryStatus()]);
      if (signal.aborted) throw new DOMException("aborted", "AbortError");
      const alerts = inventory.revisions.reduce((total, revision) => total + revision.failed + revision.stale + revision.invalid, 0);
      return { title: text("panel.assessmentOverview", "评估证据状态"), metrics: [
        { id: "archive", label: text("metric.reportArchive", "报告归档"), value: inventory.reportArchiveReady ? "Ready" : "Unavailable", tone: inventory.reportArchiveReady ? "success" : "error" },
        { id: "revisions", label: text("metric.databaseRevisions", "数据库 Revision"), value: inventory.revisions.length, tone: inventory.truncated ? "warning" : "neutral" },
        { id: "alerts", label: text("metric.assessmentAlerts", "异常评估"), value: alerts, tone: alerts === 0 ? "success" : "error" },
        { id: "unassessed", label: text("metric.unassessed", "未评估制品"), value: repository.securityAssessment?.unassessed ?? 0, tone: (repository.securityAssessment?.unassessed ?? 0) === 0 ? "success" : "warning" },
      ] };
    },
  });
}
