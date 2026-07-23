import type { ArtifactRepositoryMigration, PlatformAdminClient } from "@vastplan/platform-admin";
import { defineCollectionPage, jsonSchemaDialect, type CollectionPageDefinition, type FormSchema, type WorkbenchFormDefinition } from "@vastplan/workbench-sdk";
import { formatBytes, text, type Row } from "./shared.js";

const prepareSchema: FormSchema = {
  id: "artifact-migration-prepare.v1",
  schema: {
    $schema: jsonSchemaDialect, type: "object", additionalProperties: false,
    required: ["migrationId", "targetProvider", "targetVolumeId"],
    properties: {
      migrationId: { type: "string", title: "迁移 ID", pattern: "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$", maxLength: 96 },
      targetProvider: { type: "string", title: "目标 Provider", pattern: "^platform\\.artifacts\\.storage\\.[a-z0-9]+(?:[._-][a-z0-9]+)*$", maxLength: 160 },
      targetVolumeId: { type: "string", title: "目标 Volume", pattern: "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$", maxLength: 80 },
    },
  },
  localization: {
    "/properties/migrationId/title": text("form.migration.id", "迁移 ID"),
    "/properties/targetProvider/title": text("form.migration.provider", "目标 Provider"),
    "/properties/targetVolumeId/title": text("form.migration.volume", "目标 Volume"),
  },
};

const cutoverSchema: FormSchema = {
  id: "artifact-migration-cutover.v1",
  schema: {
    $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["observationSeconds"],
    properties: { observationSeconds: { type: "integer", title: "观察期（秒）", minimum: 60, maximum: 604800, default: 300 } },
  },
  localization: { "/properties/observationSeconds/title": text("form.migration.observation", "观察期（秒）") },
};

function migrationRow(state: ArtifactRepositoryMigration): Row {
  return {
    id: state.migrationId ?? "current",
    migrationId: state.migrationId ?? "-",
    phase: state.phase ?? "none",
    source: state.sourceProvider === undefined ? "-" : `${state.sourceProvider} / ${state.sourceVolumeId ?? "-"}`,
    target: state.targetProvider === undefined ? "-" : `${state.targetProvider} / ${state.targetVolumeId ?? "-"}`,
    sourceProvider: state.sourceProvider ?? "",
    sourceVolumeId: state.sourceVolumeId ?? "",
    targetProvider: state.targetProvider ?? "",
    targetVolumeId: state.targetVolumeId ?? "",
    files: state.files ?? 0,
    bytes: formatBytes(state.bytes ?? 0),
    digest: state.digest ?? "-",
    observationUntil: state.observationUntil ?? "",
    configuredActive: state.configuredActive,
    canRollback: state.canRollback,
    canFinalize: state.canFinalize,
    canRelease: state.canRelease,
    lastError: state.lastError ?? "-",
  };
}

function prepareForm(client: PlatformAdminClient): WorkbenchFormDefinition<Row> {
  return {
    id: "prepare", schema: prepareSchema,
    presentation: { layout: "vertical", navigation: "sections", sections: [{ id: "target", title: text("form.migration.target", "迁移目标"), columns: 2, fields: ["/migrationId", "/targetProvider", "/targetVolumeId"] }], fields: [{ pointer: "/migrationId", span: 2 }] },
    workflow: { surface: "drawer", size: "md", title: text("form.migration.prepareTitle", "准备存储迁移"), description: text("form.migration.prepareDescription", "V1 只支持同一 File Provider 下的空目标 Volume。准备阶段不会切换活动仓库。"), submitLabel: text("action.migration.prepare", "准备迁移"), confirmBeforeSubmit: text("confirm.migration.prepare", "系统将探测并绑定目标 Volume，请确认目标为空且未被其他服务使用。"), success: { notify: text("notice.migrationPrepared", "迁移已准备"), refreshCollection: true, close: true } },
    async prepare() {
      const status = await client.artifactRepositoryStatus();
      return { initialValue: { migrationId: `repository-${Date.now()}`, targetProvider: status.storageProvider ?? "", targetVolumeId: "" } };
    },
    async submit({ value }) {
      await client.prepareArtifactMigration({ migrationId: String(value.migrationId), targetProvider: String(value.targetProvider), targetVolumeId: String(value.targetVolumeId) });
    },
  };
}

function cutoverForm(client: PlatformAdminClient): WorkbenchFormDefinition<Row> {
  return {
    id: "cutover", schema: cutoverSchema, initialValue: { observationSeconds: 300 },
    presentation: { layout: "vertical", fields: [{ pointer: "/observationSeconds", widget: "number" }] },
    workflow: { surface: "dialog", size: "sm", title: text("form.migration.cutoverTitle", "切换活动 Volume"), description: text("form.migration.cutoverDescription", "系统先做最终增量同步，再原子切换并进入双写观察期。"), submitLabel: text("action.migration.cutover", "确认切换"), confirmBeforeSubmit: text("confirm.migration.cutover", "切换会短暂冻结发布；读取不会中断。"), success: { notify: text("notice.migrationCutover", "仓库已进入观察期"), refreshCollection: true, close: true } },
    async submit({ value, selected }) {
      const id = selected[0]?.migrationId;
      if (typeof id !== "string" || id === "-") return;
      await client.cutoverArtifactMigration(id, Number(value.observationSeconds));
    },
  };
}

export function migrationPage(client: PlatformAdminClient, id: string, path: string): CollectionPageDefinition<Row> {
  return defineCollectionPage<Row>({
    id, path, title: text("page.migration.title", "存储迁移"), description: text("page.migration.description", "按阶段准备、同步、切换、观察、回滚并安全释放旧 Volume"),
    navigation: { id, label: text("page.migration.navigation", "存储迁移"), zone: "settings", groupID: "platform.artifacts", order: 54 },
    collection: {
      id: `${id}.collection`, title: text("panel.migration", "当前迁移"), view: "table", query: { mode: "page", defaultPageSize: 10, pageSizeOptions: [10] }, selection: "single",
      columns: [
        { key: "migrationId", label: text("column.migrationId", "迁移 ID"), defaultVisible: true, minWidth: 190 },
        { key: "phase", label: text("column.migrationPhase", "阶段"), format: "status", valueLabels: { none: text("migration.none", "未开始"), prepared: text("migration.prepared", "已准备"), synced: text("migration.synced", "已同步"), observing: text("migration.observing", "观察中"), finalized: text("migration.finalized", "已确认"), "rolled-back": text("migration.rolledBack", "已回滚"), released: text("migration.released", "已释放") }, statusTones: { none: "neutral", prepared: "info", synced: "info", observing: "warning", finalized: "success", "rolled-back": "warning", released: "success" }, defaultVisible: true, minWidth: 120 },
        { key: "source", label: text("column.migrationSource", "源存储"), defaultVisible: true, minWidth: 220 },
        { key: "target", label: text("column.migrationTarget", "目标存储"), defaultVisible: true, minWidth: 220 },
        { key: "files", label: text("column.migrationFiles", "文件数"), format: "number", defaultVisible: true, minWidth: 100 },
        { key: "bytes", label: text("column.migrationBytes", "已同步"), defaultVisible: true, minWidth: 110 },
        { key: "digest", label: text("column.migrationDigest", "同步摘要"), defaultVisible: false, minWidth: 220 },
        { key: "observationUntil", label: text("column.migrationObservation", "观察期结束"), format: "datetime", defaultVisible: true, minWidth: 190 },
        { key: "configuredActive", label: text("column.migrationConfigured", "配置已切换"), format: "boolean", defaultVisible: true, minWidth: 120 },
        { key: "lastError", label: text("column.migrationError", "最近错误"), defaultVisible: true, minWidth: 160 },
      ],
      actions: [
        { id: "prepare", label: text("action.migration.prepare", "准备迁移"), icon: "add", placement: "page.primary", tone: "primary", form: "prepare", requiredPermissions: ["platform.artifacts.migrate"] },
        { id: "sync", label: text("action.migration.sync", "增量同步"), placement: "record.row", requiredPermissions: ["platform.artifacts.migrate"], visibleWhen: { pointer: "/phase", in: ["prepared", "synced"] } },
        { id: "cutover", label: text("action.migration.cutover", "切换"), placement: "record.row", form: "cutover", requiredPermissions: ["platform.artifacts.migrate"], visibleWhen: { pointer: "/phase", in: ["prepared", "synced"] } },
        { id: "rollback", label: text("action.migration.rollback", "回滚"), placement: "record.row", tone: "danger", confirm: text("confirm.migration.rollback", "确认回滚到源 Volume？观察期内的新写入已双写，可安全回退。"), requiredPermissions: ["platform.artifacts.migrate"], visibleWhen: { pointer: "/canRollback", equals: true } },
        { id: "finalize", label: text("action.migration.finalize", "确认迁移"), placement: "record.row", confirm: text("confirm.migration.finalize", "确认结束双写观察期？完成后不能再回滚。"), requiredPermissions: ["platform.artifacts.migrate"], visibleWhen: { pointer: "/canFinalize", equals: true } },
        { id: "release", label: text("action.migration.release", "释放旧 Volume"), placement: "record.row", tone: "danger", confirm: text("confirm.migration.release", "确认配置已指向目标 Volume，并隔离释放旧 Volume？"), requiredPermissions: ["platform.artifacts.migrate"], visibleWhen: { pointer: "/canRelease", equals: true } },
      ],
      preferences: { allowedColumns: ["migrationId", "phase", "source", "target", "files", "bytes", "digest", "observationUntil", "configuredActive", "lastError"], density: true },
    },
    forms: [prepareForm(client), cutoverForm(client)],
    async load(_query, signal) {
      const state = await client.artifactMigrationStatus();
      if (signal.aborted) throw new DOMException("aborted", "AbortError");
      return { items: [migrationRow(state)], total: 1 };
    },
    async loadSummary(signal) {
      const state = await client.artifactMigrationStatus();
      if (signal.aborted) throw new DOMException("aborted", "AbortError");
      return { title: text("panel.migrationStatus", "迁移状态"), metrics: [
        { id: "phase", label: text("metric.migrationPhase", "当前阶段"), value: state.phase ?? "none", tone: state.lastError ? "error" : "neutral" },
        { id: "files", label: text("metric.migrationFiles", "已校验文件"), value: state.files ?? 0 },
        { id: "bytes", label: text("metric.migrationBytes", "已校验字节"), value: formatBytes(state.bytes ?? 0) },
        { id: "rollback", label: text("metric.migrationRollback", "可回滚"), value: state.canRollback ? "Yes" : "No", tone: state.canRollback ? "success" : "neutral" },
        { id: "configuration", label: text("metric.migrationConfiguration", "运行配置"), value: state.configuredActive ? "Target active" : "Source active", tone: state.configuredActive ? "success" : "warning" },
      ] };
    },
    async runAction({ action, selected }) {
      const migrationId = selected[0]?.migrationId;
      if (typeof migrationId !== "string" || migrationId === "-") return;
      if (action.id === "sync") await client.syncArtifactMigration(migrationId);
      else if (action.id === "rollback") await client.rollbackArtifactMigration(migrationId);
      else if (action.id === "finalize") await client.finalizeArtifactMigration(migrationId);
      else if (action.id === "release") await client.releaseArtifactMigration(migrationId);
      return { notify: { title: action.label, kind: "success" } };
    },
  });
}
