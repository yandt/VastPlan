import { type PortalAuditEvent, type PortalControlClient, type PortalRelease } from "@vastplan/ui-primitives";
import { type JSONValue, type WorkbenchOverlayDefinition } from "@vastplan/workbench-sdk";
import type { PortalRow } from "./portal-model";

export function portalOverlays(client: PortalControlClient): WorkbenchOverlayDefinition<PortalRow>[] {
  return [versionHistory(client), versionComparison(client), releaseHistory(), auditHistory(client), configurationPreview()];
}

function versionHistory(client: PortalControlClient): WorkbenchOverlayDefinition<PortalRow> {
  return {
    id: "history", surface: "dialog", size: "lg", title: "版本历史",
    async load(selected) {
      const row = selected[0];
      const entries = row === undefined ? [] : (await client.portalVersionHistory(row.id)).entries;
      return {
        kind: "table", rowKey: "versionId",
        rows: entries.map((entry) => ({ ...entry, versionId: entry.versionRef.versionId, sequence: entry.versionRef.sequence })),
        columns: [
          { key: "sequence", label: "序号", format: "number" },
          { key: "versionId", label: "版本 ID", minWidth: 260 },
          { key: "actorId", label: "提交人" },
          { key: "createdAt", label: "提交时间", format: "datetime" },
        ],
      };
    },
  };
}

function versionComparison(client: PortalControlClient): WorkbenchOverlayDefinition<PortalRow> {
  return {
    id: "compare", surface: "dialog", size: "lg", title: "最近两个版本差异",
    async load(selected) {
      const row = selected[0];
      if (row === undefined) return { kind: "table", rows: [], columns: [] };
      const history = await client.portalVersionHistory(row.id);
      if (history.entries.length < 2) throw new Error("至少需要两个已提交版本才能比较");
      const right = history.entries[0];
      const left = history.entries.slice(1).find((entry) => entry.environmentDigest === right.environmentDigest);
      if (left === undefined) throw new Error("最近历史属于不同环境修订，当前没有可直接比较的版本对");
      const comparison = await client.comparePortalVersions(row.id, left.versionRef.versionId, right.versionRef.versionId);
      const paths = comparison.changedPaths ?? [];
      return {
        kind: "table", rowKey: "path",
        rows: paths.map((path) => ({ path, added: comparison.summary.added, modified: comparison.summary.modified, removed: comparison.summary.removed })),
        columns: [
          { key: "path", label: "变化路径", minWidth: 320 },
          { key: "added", label: "新增", format: "number" },
          { key: "modified", label: "修改", format: "number" },
          { key: "removed", label: "删除", format: "number" },
        ],
      };
    },
  };
}

function releaseHistory(): WorkbenchOverlayDefinition<PortalRow> {
  return {
    id: "releases", surface: "dialog", size: "lg", title: "上线历史",
    async load(selected) {
      return {
        kind: "table", rowKey: "id",
        rows: (selected[0]?.portal.releases ?? []) as Array<PortalRelease & Record<string, unknown>>,
        columns: [
          { key: "id", label: "上线编号", format: "number" },
          { key: "publicationId", label: "Publication", format: "number" },
          { key: "status", label: "状态", format: "status" },
          { key: "createdAt", label: "时间", format: "datetime" },
        ],
      };
    },
  };
}

function auditHistory(client: PortalControlClient): WorkbenchOverlayDefinition<PortalRow> {
  return {
    id: "audit", surface: "drawer", size: "lg", title: "审计记录",
    async load(selected) {
      const row = selected[0];
      const rows = row === undefined || row.publicationId === 0 ? [] : await client.auditPortalPublication(row.id, row.publicationId);
      return {
        kind: "table", rowKey: "id", rows: rows as Array<PortalAuditEvent & Record<string, unknown>>,
        columns: [{ key: "at", label: "时间", format: "datetime" }, { key: "action", label: "动作" }, { key: "actorId", label: "操作者" }],
      };
    },
  };
}

function configurationPreview(): WorkbenchOverlayDefinition<PortalRow> {
  return {
    id: "configuration", surface: "drawer", size: "lg", title: "当前完整配置",
    async load(selected) {
      return { kind: "json", documents: selected[0] === undefined ? [] : [{ value: selected[0].configuration as unknown as JSONValue }] };
    },
  };
}
