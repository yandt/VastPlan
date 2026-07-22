import type { AuthorizationAuditEvent, PlatformAdminClient } from "@vastplan/platform-admin";
import { defineCollectionPage, message, type CollectionPageDefinition } from "@vastplan/workbench-sdk";
import { namespace, page } from "../model.js";

type AuditRow = AuthorizationAuditEvent & Record<string, unknown>;

export function auditPage(client: PlatformAdminClient): CollectionPageDefinition<AuditRow> {
  return defineCollectionPage<AuditRow>({
    id: "platform.authorization.audit",
    path: "/settings/authorization/audit",
    title: message(namespace, "audit.title", "授权审计"),
    description: message(namespace, "audit.description", "查看角色、主体绑定、撤权和策略快照的不可变操作记录。"),
    requiredPermissions: ["platform.authorization.audit"],
    navigation: { id: "platform.authorization.audit", label: message(namespace, "audit.navigation", "授权审计"), zone: "settings", groupID: "platform.authorization", order: 40 },
    collection: {
      id: "authorization-audit",
      title: message(namespace, "audit.title", "授权审计"),
      view: "table",
      query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20, 50, 100] },
      filters: [{ id: "search", label: message(namespace, "filter.audit", "对象、动作或操作人"), kind: "text" }],
      columns: [
        { key: "action", label: message(namespace, "column.action", "动作"), defaultVisible: true },
        { key: "objectKind", label: message(namespace, "column.objectKind", "对象类型"), defaultVisible: true },
        { key: "objectId", label: message(namespace, "column.objectId", "对象"), defaultVisible: true, minWidth: 240 },
        { key: "revision", label: "Revision", format: "number", defaultVisible: true },
        { key: "subjectId", label: message(namespace, "column.actor", "操作人"), defaultVisible: true },
        { key: "reason", label: message(namespace, "column.reason", "原因"), defaultVisible: true },
        { key: "occurredAt", label: message(namespace, "column.time", "时间"), format: "datetime", defaultVisible: true },
      ],
      preferences: { allowedColumns: ["action", "objectKind", "objectId", "revision", "subjectId", "reason", "occurredAt"], density: true },
    },
    async load(query, signal) {
      const rows = await client.listAuthorizationAudit();
      if (signal.aborted) return { items: [], total: 0 };
      return page(rows as AuditRow[], query, (row, text) => row.action.toLowerCase().includes(text) || row.objectId.toLowerCase().includes(text) || row.subjectId.toLowerCase().includes(text));
    },
  });
}
