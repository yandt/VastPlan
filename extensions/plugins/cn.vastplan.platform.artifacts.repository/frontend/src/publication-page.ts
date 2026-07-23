import type { PlatformAdminClient } from "@vastplan/platform-admin";
import { defineCollectionPage, type CollectionPageDefinition } from "@vastplan/workbench-sdk";
import { paged, text, type Row } from "./shared.js";

export function publicationPage(client: PlatformAdminClient, id: string, path: string): CollectionPageDefinition<Row> {
  return defineCollectionPage<Row>({
    id, path, title: text("page.publication.title", "发布审批"), description: text("page.publication.description", "以双人分离审批将已验签 testing 制品晋级到 stable"),
    navigation: { id, label: text("page.publication.navigation", "发布审批"), zone: "settings", groupID: "platform.artifacts", order: 55 },
    collection: {
      id: `${id}.collection`, title: text("panel.publications", "发布申请"), view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [10, 20, 50] },
      filters: [{ id: "status", label: text("filter.publicationStatus", "审批状态"), kind: "select", options: ["PendingApproval", "Approved", "Published"].map((value) => ({ value, label: text(`publication.${value}`, value) })) }],
      columns: [
        { key: "pluginId", label: text("column.plugin", "插件 ID"), defaultVisible: true, minWidth: 260 },
        { key: "version", label: text("column.version", "版本"), defaultVisible: true, minWidth: 150 },
        { key: "status", label: text("column.publicationStatus", "审批状态"), format: "status", valueLabels: { PendingApproval: text("publication.PendingApproval", "待批准"), Approved: text("publication.Approved", "已批准"), Published: text("publication.Published", "已发布") }, statusTones: { PendingApproval: "warning", Approved: "info", Published: "success" }, defaultVisible: true, minWidth: 120 },
        { key: "publisher", label: text("column.publisher", "发布者"), defaultVisible: true, minWidth: 120 },
        { key: "keyId", label: text("column.keyId", "签名 Key"), defaultVisible: true, minWidth: 140 },
        { key: "reason", label: text("column.publicationReason", "发布原因"), defaultVisible: true, minWidth: 220 },
        { key: "sha256", label: "SHA-256", defaultVisible: false, minWidth: 240 },
        { key: "submittedBy", label: text("column.submittedBy", "提交人"), defaultVisible: true, minWidth: 120 },
        { key: "approvedBy", label: text("column.approvedBy", "批准人"), defaultVisible: true, minWidth: 120 },
        { key: "submittedAt", label: text("column.submittedAt", "提交时间"), format: "datetime", defaultVisible: true, minWidth: 190 },
        { key: "publishedAt", label: text("column.publicationPublishedAt", "发布时间"), format: "datetime", defaultVisible: false, minWidth: 190 },
      ],
      actions: [{ id: "approve", label: text("action.publication.approve", "批准"), placement: "record.row", confirm: text("confirm.publication.approve", "确认该 testing 制品可晋级 stable？系统会强制提交人与批准人分离。"), requiredPermissions: ["platform.artifacts.publication.approve"], visibleWhen: { pointer: "/status", equals: "PendingApproval" } }],
      preferences: { allowedColumns: ["pluginId", "version", "status", "publisher", "keyId", "reason", "sha256", "submittedBy", "approvedBy", "submittedAt", "publishedAt"], density: true },
    },
    async load(query, signal) {
      const page = await client.listArtifactPublications();
      if (signal.aborted) throw new DOMException("aborted", "AbortError");
      const status = typeof query.filters.status === "string" ? query.filters.status : "";
      const rows = page.items.filter((item) => status === "" || item.status === status).map((item) => ({ id: item.id, pluginId: item.target.pluginId, version: item.target.version, status: item.status, publisher: item.publisher, keyId: item.keyId, sha256: item.sha256, reason: item.reason, submittedBy: item.submittedBy, approvedBy: item.approvedBy ?? "-", submittedAt: item.submittedAt, publishedAt: item.publishedAt ?? "", publicationRevision: page.revision }));
      return paged(rows, query);
    },
    async runAction({ action, selected }) {
      const row = selected[0]; if (action.id !== "approve" || row === undefined) return;
      await client.approveArtifactPublication(String(row.id), Number(row.publicationRevision));
      return { notify: { title: text("notice.publicationApproved", "发布审批已批准"), kind: "success" } };
    },
  });
}
