import { createBrowserPlatformAdminClient, type PlatformAdminClient, type PluginConfigurationCandidate, type PluginConfigurationDefinition } from "@vastplan/platform-admin";
import { defineCollectionPage, managementServicesFor, message, type CollectionPageDefinition, type CollectionQuery, type FormSchema, type WorkbenchFormDefinition, type WorkbenchFrontendPluginContext } from "@vastplan/workbench-sdk";

const namespace = "cn.vastplan.platform.configuration.plugin-settings";

type ConfigurationRow = PluginConfigurationDefinition & Record<string, unknown> & {
  candidateStatus: string;
  candidateId: string;
  candidateRevision: number;
  managedCredentialCount: number;
};

const placeholderSchema: FormSchema = { id: "plugin-configuration.loading", schema: { type: "object", additionalProperties: false, properties: {} } };

export function createPluginConfigurationPage(client: PlatformAdminClient, serviceID: string, path: string, title: ReturnType<typeof message>): CollectionPageDefinition<ConfigurationRow> {
  const form: WorkbenchFormDefinition<ConfigurationRow> = {
    id: "draft",
    schema: placeholderSchema,
    workflow: {
      surface: "drawer",
      title: message(namespace, "form.title", "编辑配置草稿"),
      description: message(namespace, "form.description", "保存只创建候选草稿，不会立即重启服务或改变活动配置。"),
      size: "lg",
      submitLabel: message(namespace, "action.saveDraft", "保存草稿"),
      success: { notify: message(namespace, "notice.draftSaved", "配置草稿已保存"), refreshCollection: true, close: true },
    },
    async prepare(selected) {
      const definition = selected[0];
      if (definition === undefined) throw new Error("未选择插件配置");
      return {
        schema: { id: `plugin-configuration.${definition.id}`, schema: definition.schema as FormSchema["schema"] },
        initialValue: definition.values,
      };
    },
    async load(selected) {
      return selected[0]?.values ?? {};
    },
    async submit({ value, selected }) {
      const definition = selected[0];
      if (definition === undefined) return;
      await client.createPluginConfigurationDraft(definition.id, definition.catalogDigest, { ...value });
    },
  };

  return defineCollectionPage<ConfigurationRow>({
    id: `platform.plugin-configuration.${serviceID}`,
    path,
    title,
    description: message(namespace, "page.description", "按已验签插件清单查看配置，并以候选事务安全变更。当前阶段只开放 Draft，不会伪装为已生效。"),
    requiredPermissions: ["platform.plugin-configuration.read"],
    navigation: { id: `platform.plugin-configuration.${serviceID}`, label: title, zone: "settings", order: 25 },
    collection: {
      id: `platform.plugin-configuration.${serviceID}`,
      title,
      view: "table",
      selection: "single",
      query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20, 50, 100] },
      filters: [
        { id: "keyword", label: message(namespace, "filter.keyword", "插件或服务"), kind: "text" },
        { id: "applyMode", label: message(namespace, "filter.applyMode", "生效方式"), kind: "select", options: [
          { value: "restart", label: message(namespace, "mode.restart", "重启发布") }, { value: "hot", label: message(namespace, "mode.hot", "热配置") },
        ] },
      ],
      columns: [
        { key: "pluginName", label: message(namespace, "column.plugin", "插件"), defaultVisible: true, minWidth: 180 },
        { key: "deployment", label: message(namespace, "column.deployment", "部署"), defaultVisible: true, minWidth: 160 },
        { key: "unitId", label: message(namespace, "column.unit", "服务单元"), defaultVisible: true, minWidth: 150 },
        { key: "origin", label: message(namespace, "column.origin", "管理来源"), defaultVisible: true, minWidth: 130 },
        { key: "scope", label: message(namespace, "column.scope", "作用域"), defaultVisible: true, minWidth: 100 },
        { key: "applyMode", label: message(namespace, "column.applyMode", "生效方式"), defaultVisible: true, minWidth: 110 },
        { key: "managedCredentialCount", label: message(namespace, "column.credentials", "托管字段"), format: "number", defaultVisible: true, minWidth: 100 },
        { key: "candidateStatus", label: message(namespace, "column.candidate", "候选状态"), format: "status", defaultVisible: true, minWidth: 120 },
      ],
      actions: [
        { id: "draft", label: message(namespace, "action.edit", "编辑配置草稿"), icon: "edit", placement: "page.secondary", requiresSelection: true, form: "draft", requiredPermissions: ["platform.plugin-configuration.write"], visibleWhen: { pointer: "/candidateStatus", in: ["None", "Ready", "Failed", "RolledBack"] } },
        { id: "discard", label: message(namespace, "action.discard", "放弃草稿"), icon: "remove", placement: "page.secondary", tone: "danger", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.write"], confirm: message(namespace, "confirm.discard", "放弃所选草稿？该操作使用候选 revision CAS。"), visibleWhen: { pointer: "/candidateStatus", equals: "Draft" } },
      ],
    },
    forms: [form],
    async load(query: CollectionQuery, signal) {
      const [definitions, candidates] = await Promise.all([client.listPluginConfigurationDefinitions(), client.listPluginConfigurationCandidates()]);
      if (signal.aborted) return { items: [], total: 0 };
      const latest = latestCandidates(candidates);
      const keyword = typeof query.filters.keyword === "string" ? query.filters.keyword.trim().toLowerCase() : "";
      const applyMode = typeof query.filters.applyMode === "string" ? query.filters.applyMode : "";
      const rows = definitions.map((definition) => configurationRow(definition, latest.get(definition.id))).filter((row) =>
        (keyword === "" || `${row.pluginName} ${row.pluginId} ${row.deployment} ${row.unitId}`.toLowerCase().includes(keyword)) &&
        (applyMode === "" || row.applyMode === applyMode),
      );
      const start = Math.max(0, (query.page - 1) * query.pageSize);
      return { items: rows.slice(start, start + query.pageSize), total: rows.length };
    },
    async runAction({ action, selected }) {
      const row = selected[0];
      if (action.id !== "discard" || row === undefined || row.candidateId === "") return;
      await client.discardPluginConfigurationDraft(row.candidateId, row.candidateRevision);
    },
  });
}

function latestCandidates(candidates: readonly PluginConfigurationCandidate[]): Map<string, PluginConfigurationCandidate> {
  const latest = new Map<string, PluginConfigurationCandidate>();
  for (const candidate of candidates) {
    const current = latest.get(candidate.configurationId);
    if (current === undefined || candidate.updatedAt > current.updatedAt) latest.set(candidate.configurationId, candidate);
  }
  return latest;
}

function configurationRow(definition: PluginConfigurationDefinition, candidate: PluginConfigurationCandidate | undefined): ConfigurationRow {
  return {
    ...definition,
    candidateStatus: candidate?.status ?? "None",
    candidateId: candidate?.id ?? "",
    candidateRevision: candidate?.revision ?? 0,
    managedCredentialCount: definition.managedCredentials?.length ?? 0,
  };
}

export default {
  register(context: WorkbenchFrontendPluginContext) {
    const services = managementServicesFor(context.portal, "platform.plugin-configuration");
    if (services.length === 0) throw new Error("Portal 未绑定 platform.plugin-configuration 服务");
    for (const service of services) {
      const suffix = services.length === 1 ? "" : `/${service.id}`;
      const title = context.i18n.message(services.length === 1 ? "page.title" : "page.titleService", services.length === 1 ? "插件配置" : "插件配置 · {service}", { service: service.label ?? service.id });
      context.addCollectionPage(createPluginConfigurationPage(createBrowserPlatformAdminClient(context.portal.id, service.id), service.id, `/settings/plugin-configurations${suffix}`, title));
    }
  },
  localization: { defaultLocale: "zh-CN", messages: {
    "zh-CN": {
      "page.title":"插件配置","page.titleService":"插件配置 · {service}","page.description":"按已验签插件清单查看配置，并以候选事务安全变更。当前阶段只开放 Draft，不会伪装为已生效。",
      "filter.keyword":"插件或服务","filter.applyMode":"生效方式","mode.restart":"重启发布","mode.hot":"热配置",
      "column.plugin":"插件","column.deployment":"部署","column.unit":"服务单元","column.origin":"管理来源","column.scope":"作用域","column.applyMode":"生效方式","column.credentials":"托管字段","column.candidate":"候选状态",
      "form.title":"编辑配置草稿","form.description":"保存只创建候选草稿，不会立即重启服务或改变活动配置。","action.saveDraft":"保存草稿","notice.draftSaved":"配置草稿已保存","action.edit":"编辑配置草稿","action.discard":"放弃草稿","confirm.discard":"放弃所选草稿？该操作使用候选 revision CAS。"
    },
    "en-US": {
      "page.title":"Plugin configuration","page.titleService":"Plugin configuration · {service}","page.description":"Inspect signed plugin configuration contracts and create governed candidates. Drafts are never presented as active.",
      "filter.keyword":"Plugin or service","filter.applyMode":"Apply mode","mode.restart":"Restart publication","mode.hot":"Hot configuration",
      "column.plugin":"Plugin","column.deployment":"Deployment","column.unit":"Service unit","column.origin":"Managed by","column.scope":"Scope","column.applyMode":"Apply mode","column.credentials":"Managed fields","column.candidate":"Candidate status",
      "form.title":"Edit configuration draft","form.description":"Saving creates a candidate draft only; it does not restart services or change active configuration.","action.saveDraft":"Save draft","notice.draftSaved":"Configuration draft saved","action.edit":"Edit draft","action.discard":"Discard draft","confirm.discard":"Discard the selected draft using its candidate revision?"
    }
  } },
};
