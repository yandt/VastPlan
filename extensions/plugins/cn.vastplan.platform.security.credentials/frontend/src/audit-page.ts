import type { ManagedCredentialAuditEvent, PlatformAdminClient } from "@vastplan/platform-admin";
import { defineCollectionPage, message, type CollectionPageDefinition, type CollectionQuery } from "@vastplan/workbench-sdk";

const namespace = "cn.vastplan.platform.security.credentials";

type AuditRow = ManagedCredentialAuditEvent & Record<string, unknown>;

export function createCredentialAuditPage(client: PlatformAdminClient, serviceID: string, path: string): CollectionPageDefinition<AuditRow> {
  return defineCollectionPage<AuditRow>({
    id: `platform.credentials.audit.${serviceID}`,
    path,
    title: message(namespace, "audit.title", "托管凭证审计"),
    description: message(namespace, "audit.description", "查看不含 handle、stage ID、authority、密文或明文的托管凭证生命周期事件"),
    requiredPermissions: ["platform.credentials.audit"],
    navigation: { id: `platform.credentials.audit.${serviceID}`, label: message(namespace, "audit.title", "托管凭证审计"), zone: "settings", order: 31 },
    collection: {
      id: `platform.credentials.audit.${serviceID}`,
      title: message(namespace, "audit.title", "托管凭证审计"),
      view: "table",
      query: { mode: "cursor", defaultPageSize: 50, pageSizeOptions: [20, 50, 100] },
      filters: [],
      columns: [
        { key: "occurredAt", label: message(namespace, "audit.column.time", "发生时间"), format: "datetime", defaultVisible: true, minWidth: 180 },
        { key: "credentialFingerprint", label: message(namespace, "audit.column.fingerprint", "凭证指纹"), defaultVisible: true, minWidth: 220 },
        { key: "action", label: message(namespace, "audit.column.action", "动作"), defaultVisible: true, minWidth: 180 },
        { key: "state", label: message(namespace, "audit.column.state", "状态"), format: "status", statusTones: { Active: "success", Candidate: "warning", Preparing: "warning", Aborted: "neutral", Retired: "neutral" }, defaultVisible: true, minWidth: 110 },
        { key: "owner", label: message(namespace, "audit.column.owner", "Owner"), defaultVisible: true, minWidth: 260 },
        { key: "purpose", label: message(namespace, "audit.column.purpose", "用途"), defaultVisible: true, minWidth: 180 },
        { key: "resource", label: message(namespace, "audit.column.resource", "资源"), defaultVisible: true, minWidth: 220 },
        { key: "candidateId", label: message(namespace, "audit.column.candidate", "候选"), defaultVisible: false, minWidth: 260 },
      ],
      actions: [],
    },
    async load(query: CollectionQuery, signal) {
      const beforeId = query.cursor === undefined ? undefined : Number(query.cursor);
      const response = await client.listManagedCredentialAudit(beforeId, query.pageSize);
      if (signal.aborted) return { items: [] };
      return { items: response.items as AuditRow[], ...(response.nextBeforeId === undefined ? {} : { nextCursor: String(response.nextBeforeId) }) };
    },
    async loadSummary(signal) {
      const response = await client.listManagedCredentialAudit(undefined, 1);
      if (signal.aborted) return { metrics: [] };
      const counts = response.maintenance.counts;
      return { metrics: [
        { id: "managed", label: message(namespace, "audit.summary.managed", "当前托管记录"), value: Object.values(counts).reduce((total, value) => total + value, 0) },
        { id: "preparing", label: message(namespace, "audit.summary.preparing", "等待完成"), value: counts.Preparing ?? 0, tone: (counts.Preparing ?? 0) > 0 ? "warning" : "neutral" },
        { id: "autoAborted", label: message(namespace, "audit.summary.autoAborted", "累计自动终止"), value: response.maintenance.autoAborted },
        { id: "collected", label: message(namespace, "audit.summary.collected", "累计回收"), value: response.maintenance.collected },
      ] };
    },
  });
}
