import {
  type PlatformAdminClient,
  type PluginConfigurationCandidate,
  type PluginConfigurationDefinition,
  type PluginConfigurationResourceCollection,
  type PluginConfigurationResourceItem,
} from "@vastplan/platform-admin";
import {
  defineMasterDetailPage,
  message,
  type CollectionQuery,
  type FormSchema,
  type MasterDetailPageDefinition,
  type WorkbenchFormDefinition,
} from "@vastplan/workbench-sdk";

const namespace = "cn.vastplan.platform.configuration.plugin-settings";

type ResourceRow = Record<string, unknown> & {
  id: string; name: string; pluginName: string; collectionTitle: string; status: string; tone: string;
  updatedAt: string; valuesSummary: string; values: Record<string, unknown>;
  definition: PluginConfigurationDefinition; collection: PluginConfigurationResourceCollection;
  item?: PluginConfigurationResourceItem; candidate?: PluginConfigurationCandidate;
};

type ResourceCatalogEntry = { key: string; definition: PluginConfigurationDefinition; collection: PluginConfigurationResourceCollection };

export function createPluginConfigurationResourcePage(client: PlatformAdminClient, serviceID: string, path: string): MasterDetailPageDefinition<ResourceRow> {
  const cache = new Map<string, ResourceRow>();
  const createForm: WorkbenchFormDefinition<ResourceRow> = {
    id: "resource-create", schema: emptyFormSchema("resource-create"),
    workflow: { surface: "drawer", title: message(namespace, "resource.create", "新增 Profile"), size: "lg", submitLabel: message(namespace, "resource.saveDraft", "保存草稿"), success: { notify: message(namespace, "resource.draftSaved", "Profile 草稿已保存"), refreshCollection: true, close: true } },
    async prepare() {
      const catalog = await loadResourceCatalog(client);
      if (catalog.length === 0) throw new Error("没有可管理的 Profile 资源集合");
      return { schema: createResourceSchema(catalog), initialValue: { target: catalog[0]!.key, values: {}, secrets: {} } };
    },
    async submit({ value }) {
      const catalog = await loadResourceCatalog(client);
      const entry = catalog.find((candidate) => candidate.key === value.target);
      if (entry === undefined) throw new Error("Profile 资源集合已变化，请刷新后重试");
      await client.createPluginConfigurationResourceDraft(entry.definition.id, entry.collection.id, entry.definition.catalogDigest, asRecord(value.values), secretValues(value.secrets));
    },
  };
  const editor: WorkbenchFormDefinition<ResourceRow> = {
    id: "resource-editor", schema: emptyFormSchema("resource-editor"),
    workflow: { surface: "page", title: message(namespace, "resource.edit", "编辑 Profile"), submitLabel: message(namespace, "resource.saveDraft", "保存草稿"), success: { notify: message(namespace, "resource.draftSaved", "Profile 草稿已保存"), refreshCollection: true } },
    async prepare(selected) {
      const row = selected[0];
      if (row?.item === undefined) throw new Error("只有 Active Profile 可以创建更新草稿");
      return { schema: editResourceSchema(row.collection, row.item), initialValue: { values: row.item.values, secrets: {} } };
    },
    async load(selected) { return { values: selected[0]?.item?.values ?? {}, secrets: {} }; },
    async submit({ value, selected }) {
      const row = selected[0];
      if (row?.item === undefined || (row.candidate && !terminal(row.candidate.status))) throw new Error("Profile 已有未完成候选");
      await client.updatePluginConfigurationResourceDraft(row.definition.id, row.collection.id, row.id, row.definition.catalogDigest, asRecord(value.values), secretValues(value.secrets));
    },
  };

  return defineMasterDetailPage<ResourceRow>({
    id: `platform.plugin-configuration.resources.${serviceID}`, path, pattern: "master-detail",
    title: message(namespace, "resource.page", "插件 Profile"), description: message(namespace, "resource.description", "每个 Profile 独立版本、审批和托管秘密；根启动配置与动态资源互不混用。"),
    requiredPermissions: ["platform.plugin-configuration.read"], navigation: { id: `platform.plugin-configuration.resources.${serviceID}`, label: message(namespace, "resource.page", "插件 Profile"), zone: "settings", order: 26 },
    master: {
      id: `plugin-configuration-resources.${serviceID}`, title: message(namespace, "resource.list", "Profile 列表"), keyField: "id", titleField: "name", subtitleField: "pluginName",
      status: { labelField: "status", toneField: "tone" }, query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20, 50, 100] },
      filters: [{ id: "keyword", label: message(namespace, "filter.keyword", "插件或 Profile"), kind: "text" }, { id: "status", label: message(namespace, "resource.status", "状态"), kind: "select", options: ["Active", "Draft", "Publishing", "Activating", "Failed", "RolledBack"].map((value) => ({ value, label: value })) }],
    },
    detail: {
      titleKey: "name", subtitleKey: "pluginName", status: { labelKey: "status", toneKey: "tone" }, emptyTitle: message(namespace, "resource.empty", "请选择一个 Profile"),
      sections: [
        { id: "identity", title: message(namespace, "resource.identity", "资源身份"), columns: 2, fields: [
          { key: "id", label: message(namespace, "resource.id", "资源 ID") }, { key: "collectionTitle", label: message(namespace, "resource.collection", "资源集合") },
          { key: "pluginName", label: message(namespace, "column.plugin", "插件") }, { key: "updatedAt", label: message(namespace, "resource.updated", "更新时间"), format: "datetime" },
        ] },
        { id: "values", title: message(namespace, "resource.values", "非敏感配置"), columns: 1, fields: [{ key: "valuesSummary", label: message(namespace, "resource.values", "非敏感配置") }] },
      ],
    },
    editor, forms: [createForm],
    actions: [
      { id: "resource-create", label: message(namespace, "resource.create", "新增 Profile"), icon: "add", placement: "page.primary", form: "resource-create", requiredPermissions: ["platform.plugin-configuration.write"] },
      { id: "resource-delete", label: message(namespace, "resource.delete", "删除 Profile"), icon: "remove", placement: "record.detail", tone: "danger", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.write"], confirm: message(namespace, "resource.deleteConfirm", "创建删除候选？Active Profile 只会在独立审批和提交后删除。"), visibleWhen: { pointer: "/status", equals: "Active" } },
      { id: "resource-submit", label: message(namespace, "resource.submit", "提交审批"), icon: "publish", placement: "record.detail", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.resource.publish"], visibleWhen: { pointer: "/status", equals: "Draft" } },
      { id: "resource-approve", label: message(namespace, "resource.approve", "批准候选"), icon: "success", placement: "record.detail", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.resource.publish"], visibleWhen: { pointer: "/status", equals: "Publishing" } },
      { id: "resource-activate", label: message(namespace, "resource.activate", "激活候选"), icon: "publish", placement: "record.detail", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.resource.publish"], visibleWhen: { pointer: "/candidate/externalStatus", equals: "Approved" } },
      { id: "resource-abort", label: message(namespace, "resource.abort", "放弃候选"), icon: "remove", placement: "record.detail", tone: "danger", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.resource.publish"], visibleWhen: { pointer: "/status", equals: "Publishing" } },
    ],
    async loadMaster(query: CollectionQuery, signal) {
      const rows = await loadResourceRows(client);
      if (signal.aborted) return { items: [], total: 0 };
      cache.clear(); for (const row of rows) cache.set(row.id, row);
      const keyword = typeof query.filters.keyword === "string" ? query.filters.keyword.trim().toLowerCase() : "";
      const status = typeof query.filters.status === "string" ? query.filters.status : "";
      const filtered = rows.filter((row) => (keyword === "" || `${row.name} ${row.pluginName} ${row.id}`.toLowerCase().includes(keyword)) && (status === "" || row.status === status));
      const start = (query.page - 1) * query.pageSize;
      return { items: filtered.slice(start, start + query.pageSize), total: filtered.length };
    },
    async loadRecord(key) { return cache.get(key) ?? (await loadResourceRows(client)).find((row) => row.id === key); },
    async runAction({ action, record }) {
      if (record === undefined) return;
      if (action.id === "resource-delete") await client.deletePluginConfigurationResourceDraft(record.definition.id, record.collection.id, record.id, record.definition.catalogDigest);
      const candidate = record.candidate;
      if (candidate === undefined) return;
      if (action.id === "resource-submit") await client.submitPluginConfigurationResourceDraft(candidate.id, candidate.revision);
      if (action.id === "resource-approve") await client.approvePluginConfigurationResourceCandidate(candidate.id, candidate.revision);
      if (action.id === "resource-activate") await client.activatePluginConfigurationResourceCandidate(candidate.id, candidate.revision);
      if (action.id === "resource-abort") await client.abortPluginConfigurationResourceCandidate(candidate.id, candidate.revision);
    },
  });
}

async function loadResourceCatalog(client: PlatformAdminClient): Promise<ResourceCatalogEntry[]> {
  const definitions = await client.listPluginConfigurationDefinitions();
  return definitions.flatMap((definition) => (definition.resourceCollections ?? []).map((collection) => ({ key: `${definition.id}/${collection.id}`, definition, collection })));
}

async function loadResourceRows(client: PlatformAdminClient): Promise<ResourceRow[]> {
  const [catalog, candidates] = await Promise.all([loadResourceCatalog(client), client.listPluginConfigurationCandidates()]);
  const latest = new Map<string, PluginConfigurationCandidate>();
  for (const candidate of candidates.filter((item) => item.applyPath === "resource-profile" && item.resourceId)) {
    const current = latest.get(candidate.resourceId!);
    if (current === undefined || candidate.updatedAt > current.updatedAt) latest.set(candidate.resourceId!, candidate);
  }
  const rows: ResourceRow[] = [];
  for (const entry of catalog) {
    const page = await client.listPluginConfigurationResources(entry.definition.id, entry.collection.id, entry.definition.catalogDigest, undefined, entry.collection.maxItems);
    if (page.nextCursor !== undefined) throw new Error("Profile 数量超过签名集合上限");
    for (const item of page.items) rows.push(resourceRow(entry, item, latest.get(item.resourceId)));
    for (const candidate of latest.values()) if (candidate.resourceCollectionId === entry.collection.id && candidate.resourceAction === "create" && !page.items.some((item) => item.resourceId === candidate.resourceId)) rows.push(resourceRow(entry, undefined, candidate));
  }
  return rows.sort((left, right) => left.name.localeCompare(right.name));
}

function resourceRow(entry: ResourceCatalogEntry, item: PluginConfigurationResourceItem | undefined, candidate: PluginConfigurationCandidate | undefined): ResourceRow {
  const values = item?.values ?? candidate?.values ?? {};
  const status = candidate && !terminal(candidate.status) ? candidate.status : item ? "Active" : candidate?.status ?? "Unknown";
  return { id: item?.resourceId ?? candidate?.resourceId ?? "", name: String(values.displayName ?? item?.resourceId ?? candidate?.resourceId ?? "Profile"), pluginName: entry.definition.pluginName, collectionTitle: entry.collection.title, status, tone: status === "Active" ? "success" : status === "Failed" ? "error" : status === "Draft" ? "info" : "warning", updatedAt: item?.updatedAt ?? candidate?.updatedAt ?? "", valuesSummary: JSON.stringify(values), values, definition: entry.definition, collection: entry.collection, ...(item ? { item } : {}), ...(candidate ? { candidate } : {}) };
}

function createResourceSchema(catalog: ResourceCatalogEntry[]): FormSchema {
  return { id: "plugin-configuration.resource.create", schema: { type: "object", additionalProperties: false, oneOf: catalog.map((entry) => ({ title: `${entry.definition.pluginName} · ${entry.collection.title}`, properties: { target: { const: entry.key, title: "资源集合" }, values: entry.collection.schema, secrets: secretsSchema(entry.collection, undefined) }, required: ["target", "values", ...(requiredSecretIDs(entry.collection, undefined).length === 0 ? [] : ["secrets"])] })) } as FormSchema["schema"] };
}

function editResourceSchema(collection: PluginConfigurationResourceCollection, item: PluginConfigurationResourceItem): FormSchema {
  const required = requiredSecretIDs(collection, item);
  return { id: `plugin-configuration.resource.${item.resourceId}`, schema: { type: "object", additionalProperties: false, properties: { values: collection.schema, secrets: secretsSchema(collection, item) }, required: ["values", ...(required.length === 0 ? [] : ["secrets"])] } as FormSchema["schema"] };
}

function secretsSchema(collection: PluginConfigurationResourceCollection, item: PluginConfigurationResourceItem | undefined): Record<string, unknown> {
  const required = requiredSecretIDs(collection, item);
  return { type: "object", additionalProperties: false, properties: Object.fromEntries((collection.managedCredentials ?? []).map((field) => [field.id, { type: "string", format: "vastplan-secret-material", writeOnly: true, title: field.title, ...(field.description ? { description: field.description } : {}) }])), ...(required.length === 0 ? {} : { required }) };
}

function requiredSecretIDs(collection: PluginConfigurationResourceCollection, item: PluginConfigurationResourceItem | undefined): string[] {
  const configured = new Set((item?.credentialStates ?? []).filter((state) => state.configured).map((state) => state.fieldId));
  return (collection.managedCredentials ?? []).filter((field) => field.required === true && !configured.has(field.id)).map((field) => field.id);
}

function emptyFormSchema(id: string): FormSchema { return { id, schema: { type: "object", additionalProperties: false, properties: {} } }; }
function terminal(status: PluginConfigurationCandidate["status"]): boolean { return status === "Ready" || status === "Failed" || status === "RolledBack"; }
function asRecord(value: unknown): Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : {}; }
function secretValues(value: unknown): Record<string, string> { return Object.fromEntries(Object.entries(asRecord(value)).filter((entry): entry is [string, string] => typeof entry[1] === "string" && entry[1] !== "")); }
