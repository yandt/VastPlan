import type { ArtifactCatalogEntry, PlatformAdminClient, PluginMarketplaceSource } from "@vastplan/platform-admin";
import { defineCollectionPage, jsonSchemaDialect, type CollectionPageDefinition, type CollectionQuery, type JSONValue, type LocalizedText, type WorkbenchFormDefinition } from "@vastplan/workbench-sdk";

export interface MarketRow extends Record<string, unknown> {
  id: string; sourceId: string; sourceLabel: string; pluginId: string; name: string; version: string; channel: string; publisher: string; publishedAt: string;
  installable: boolean; availability: string;
  entry: ArtifactCatalogEntry;
}

export function createMarketPage(marketplace: PlatformAdminClient, deployment: PlatformAdminClient, path: string, message: Message): CollectionPageDefinition<MarketRow> {
  const text = (key: string, fallback: string) => message(key, fallback);
  return defineCollectionPage<MarketRow>({
    id: "platform.service-plugin-marketplace", path, title: text("market.title", "服务插件市场"), description: text("market.description", "从平台配置的多个受信市场发现插件，并为当前服务创建安装候选"),
    requiredPermissions: ["platform.artifacts.marketplace.read", "platform.deployment.plugin.preview"],
    navigation: { id: "platform.service-plugin-marketplace", label: text("market.title", "服务插件市场"), parentMenuRef: { pluginID: "cn.vastplan.platform.artifacts.marketplace", nodeID: "marketplace" }, order: 11 },
    collection: {
      id: "platform.service-plugin-marketplace", title: text("market.title", "服务插件市场"), view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20, 50, 100] }, selection: "single",
      filterPanel: { fields: [{ id: "source", label: text("market.source", "市场"), kind: "text" }, { id: "plugin", label: text("market.plugin", "插件 ID"), kind: "text" }] },
      columns: [
        { key: "sourceLabel", label: text("market.source", "市场"), defaultVisible: true, minWidth: 140 }, { key: "name", label: text("market.name", "名称"), defaultVisible: true, minWidth: 180 },
        { key: "pluginId", label: text("market.plugin", "插件 ID"), defaultVisible: true, minWidth: 240 }, { key: "version", label: text("market.version", "版本"), defaultVisible: true, minWidth: 100 },
        { key: "channel", label: text("market.channel", "通道"), defaultVisible: true, minWidth: 90 }, { key: "publisher", label: text("market.publisher", "发布者"), defaultVisible: true, minWidth: 110 },
        { key: "availability", label: text("market.availability", "安装状态"), format: "status", valueLabels: { installable: text("market.installable", "可安装"), importRequired: text("market.importRequired", "需导入受信仓库"), platformManaged: text("market.platformManaged", "由平台基线管理") }, statusTones: { installable: "success", importRequired: "warning", platformManaged: "neutral" }, defaultVisible: true, minWidth: 120 },
        { key: "publishedAt", label: text("market.updated", "发布时间"), format: "datetime", defaultVisible: true, minWidth: 180 },
      ],
      actions: [{ id: "install", label: text("market.install", "申请安装"), icon: "download", placement: "record.row", tone: "primary", requiredPermissions: ["platform.deployment.plugin.request"], visibleWhen: { pointer: "/installable", equals: true }, form: "market-install" }],
    },
    forms: [installForm(deployment, text)],
    async load(query: CollectionQuery, signal) {
      const sources = (await marketplace.listPluginMarketplaceSources()).sources;
      const sourceFilter = filter(query.filters.source), pluginFilter = filter(query.filters.plugin);
      const selected = sources.filter((source) => sourceFilter === "" || source.id.toLowerCase().includes(sourceFilter) || source.label.toLowerCase().includes(sourceFilter));
      const pages = await Promise.all(selected.map(async (source) => {
        try { return { source, page: await marketplace.listPluginMarketplaceCatalog({ sourceId: source.id, target: "backend", lifecycle: "active", page: 1, pageSize: 100 }) }; }
        catch { return undefined; }
      }));
      if (signal.aborted) return { items: [], total: 0 };
      const available = pages.filter((item): item is { source: PluginMarketplaceSource; page: Awaited<ReturnType<PlatformAdminClient["listPluginMarketplaceCatalog"]>> } => item !== undefined);
      if (selected.length > 0 && available.length === 0) throw new Error("所有插件市场当前均不可用");
      const rows = available.flatMap(({ source, page }) => page.items.map((entry) => marketRow(source, entry))).filter((row) => pluginFilter === "" || row.pluginId.toLowerCase().includes(pluginFilter) || row.name.toLowerCase().includes(pluginFilter));
      rows.sort((left, right) => left.pluginId.localeCompare(right.pluginId) || right.version.localeCompare(left.version) || left.sourceId.localeCompare(right.sourceId));
      const start = Math.max(0, (query.page - 1) * query.pageSize);
      return { items: rows.slice(start, start + query.pageSize), total: rows.length };
    },
    async runAction() { return; },
  });
}

function installForm(deployment: PlatformAdminClient, text: (key: string, fallback: string) => ReturnType<Message>): WorkbenchFormDefinition<MarketRow> {
  return {
    id: "market-install",
    schema: installSchema(), presentation: { preset: "compact", layout: "horizontal", fields: [{ pointer: "/pluginId" }, { pointer: "/version" }, { pointer: "/versionPolicy", widget: "select" }] },
    workflow: { title: text("market.installTitle", "安装服务插件"), description: text("market.installDescription", "目标服务由 Portal 管理绑定确定，不能在此表单中更改。"), submitLabel: text("market.install", "申请安装"), success: { notify: text("market.created", "已创建插件安装候选"), close: true } },
    async prepare(selected) {
      const row = selected[0]; if (row === undefined) throw new Error("请选择一个插件");
      return { initialValue: { pluginId: row.pluginId, version: row.version, channel: row.channel, versionPolicy: "exact" } };
    },
    async submit({ value, selected }) {
      const row = selected[0]; if (row === undefined || value.pluginId !== row.pluginId || value.version !== row.version || value.channel !== row.channel) throw new Error("市场条目已变化，请重新选择");
      const constraint = value.versionPolicy === "compatible" ? `^${row.version}` : `=${row.version}`;
      return { data: await deployment.createSelfServicePluginInstallationCandidate({ version: 1, change: { action: "install", pluginId: row.pluginId, requirement: { pluginId: row.pluginId, constraint, channel: row.channel } } }) as unknown as JSONValue };
    },
  };
}

function installSchema() {
  return { id: "plugin-marketplace.install.v1", schema: { $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["pluginId", "version", "channel", "versionPolicy"], properties: {
    pluginId: { type: "string", title: "插件 ID", readOnly: true }, version: { type: "string", title: "版本", readOnly: true }, channel: { type: "string", title: "通道", readOnly: true }, versionPolicy: { type: "string", title: "版本策略", oneOf: [{ const: "exact", title: "固定当前版本" }, { const: "compatible", title: "允许兼容升级" }] },
  } }, uiSchema: { versionPolicy: { "ui:widget": "select" } } } as const;
}

function marketRow(source: PluginMarketplaceSource, entry: ArtifactCatalogEntry): MarketRow {
  const local = source.url.startsWith("vastplan://");
  const application = isApplicationCandidate(entry);
  const installable = local && application;
  return { id: `${source.id}:${entry.ref.pluginId}@${entry.ref.version}/${entry.ref.channel}`, sourceId: source.id, sourceLabel: source.label, pluginId: entry.ref.pluginId, name: entry.name || entry.ref.pluginId, version: entry.ref.version, channel: entry.ref.channel, publisher: entry.publisher, publishedAt: entry.publishedAt, installable, availability: !local ? "importRequired" : application ? "installable" : "platformManaged", entry };
}
// This is a display-only projection. Deployment Manager remains the canonical
// enforcement point and reclassifies the signed Manifest before creating a candidate.
function isApplicationCandidate(entry: ArtifactCatalogEntry): boolean {
  if (!entry.ref.pluginId.startsWith("cn.vastplan.")) return entry.publisher !== "vastplan";
  if (entry.publisher !== "vastplan") return false;
  return entry.ref.pluginId.startsWith("cn.vastplan.product.") || entry.ref.pluginId.startsWith("cn.vastplan.integration.");
}
function filter(value: unknown): string { return typeof value === "string" ? value.trim().toLowerCase() : ""; }
type Message = (key: string, fallback: string) => LocalizedText;
