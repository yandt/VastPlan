import { createBrowserPlatformAdminClient, type PlatformAdminClient, type PluginConfigurationCandidate, type PluginConfigurationDefinition } from "@vastplan/platform-admin";
import { defineCollectionPage, managementServicesFor, message, type CollectionPageDefinition, type CollectionQuery, type FormSchema, type WorkbenchFormDefinition, type WorkbenchFrontendPluginContext } from "@vastplan/workbench-sdk";
import { createPluginConfigurationResourcePage } from "./resource-page";

const namespace = "cn.vastplan.platform.configuration.plugin-settings";

type ConfigurationRow = PluginConfigurationDefinition & Record<string, unknown> & {
  candidateStatus: string;
  candidateId: string;
  candidateRevision: number;
  candidateExternalStatus: string;
  managedCredentialCount: number;
	  scopeSubjectId: string;
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
		schema: configurationFormSchema(definition),
		initialValue: { scopeSubjectId: "", values: definition.values, secrets: {} },
      };
    },
    async load(selected) {
	  return { scopeSubjectId: selected[0]?.scopeSubjectId ?? "", values: selected[0]?.values ?? {}, secrets: {} };
    },
    async submit({ value, selected }) {
      const definition = selected[0];
      if (definition === undefined) return;
	  const values = asRecord(value.values);
	  const secrets = Object.fromEntries(Object.entries(asRecord(value.secrets)).filter((entry): entry is [string, string] => typeof entry[1] === "string" && entry[1] !== ""));
	  const scopeSubjectId = typeof value.scopeSubjectId === "string" ? value.scopeSubjectId.trim() : "";
	  await client.createPluginConfigurationDraft(definition.id, definition.catalogDigest, values, secrets, scopeSubjectId);
    },
  };

  return defineCollectionPage<ConfigurationRow>({
    id: `platform.plugin-configuration.${serviceID}`,
    path,
    title,
    description: message(namespace, "page.description", "按已验签插件清单创建候选；重启配置走 Deployment，服务级热配置由插件控制器原子提交，两者均需独立审批。"),
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
		{ id: "scopeSubjectId", label: message(namespace, "filter.scopeSubject", "用户主体 ID"), kind: "text" },
      ],
      columns: [
        { key: "pluginName", label: message(namespace, "column.plugin", "插件"), defaultVisible: true, minWidth: 180 },
        { key: "deployment", label: message(namespace, "column.deployment", "部署"), defaultVisible: true, minWidth: 160 },
        { key: "unitId", label: message(namespace, "column.unit", "服务单元"), defaultVisible: true, minWidth: 150 },
        { key: "origin", label: message(namespace, "column.origin", "管理来源"), defaultVisible: true, minWidth: 130 },
        { key: "scope", label: message(namespace, "column.scope", "作用域"), defaultVisible: true, minWidth: 100 },
		{ key: "scopeSubjectId", label: message(namespace, "column.scopeSubject", "目标主体"), defaultVisible: false, minWidth: 180 },
        { key: "applyMode", label: message(namespace, "column.applyMode", "生效方式"), defaultVisible: true, minWidth: 110 },
        { key: "controllerAvailable", label: message(namespace, "column.controller", "热控制器"), format: "boolean", defaultVisible: true, minWidth: 100 },
        { key: "managedCredentialCount", label: message(namespace, "column.credentials", "托管字段"), format: "number", defaultVisible: true, minWidth: 100 },
        { key: "candidateStatus", label: message(namespace, "column.candidate", "候选状态"), format: "status", defaultVisible: true, minWidth: 120 },
        { key: "candidateExternalStatus", label: message(namespace, "column.external", "外部发布"), format: "status", defaultVisible: true, minWidth: 130 },
      ],
      actions: [
        { id: "draft", label: message(namespace, "action.edit", "编辑配置草稿"), icon: "edit", placement: "page.secondary", requiresSelection: true, form: "draft", requiredPermissions: ["platform.plugin-configuration.write"], visibleWhen: { pointer: "/candidateStatus", in: ["None", "Ready", "Failed", "RolledBack"] } },
        { id: "discard", label: message(namespace, "action.discard", "放弃草稿"), icon: "remove", placement: "page.secondary", tone: "danger", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.write"], confirm: message(namespace, "confirm.discard", "放弃所选草稿？该操作使用候选 revision CAS。"), visibleWhen: { pointer: "/candidateStatus", equals: "Draft" } },
        { id: "submit", label: message(namespace, "action.submit", "提交审批"), icon: "publish", placement: "page.primary", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.publish"], confirm: message(namespace, "confirm.submit", "提交后将创建独立服务修订，并由另一位审批人审批。"), visibleWhen: { all: [{ pointer: "/candidateStatus", equals: "Draft" }, { pointer: "/applyPath", equals: "application-deployment" }] } },
        { id: "activate", label: message(namespace, "action.activate", "发布并激活"), icon: "publish", placement: "page.primary", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.publish"], confirm: message(namespace, "confirm.activate", "发布已审批修订；readiness 失败将自动创建单调回滚修订。"), visibleWhen: { all: [{ pointer: "/candidateExternalStatus", equals: "Approved" }, { pointer: "/applyPath", equals: "application-deployment" }] } },
        { id: "submit-profile", label: message(namespace, "action.submitProfile", "提交平台配置审批"), icon: "publish", placement: "page.primary", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.profile.publish"], confirm: message(namespace, "confirm.submitProfile", "该变更会修改目标服务绑定的 Platform Profile，必须由另一位授权主体审批。"), visibleWhen: { all: [{ pointer: "/candidateStatus", equals: "Draft" }, { pointer: "/applyPath", equals: "platform-profile" }] } },
        { id: "approve-profile", label: message(namespace, "action.approveProfile", "批准平台配置"), icon: "success", placement: "page.primary", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.profile.publish"], confirm: message(namespace, "confirm.approveProfile", "确认以不同主体批准该 Platform Profile 配置候选？"), visibleWhen: { all: [{ pointer: "/candidateExternalStatus", equals: "PendingApproval" }, { pointer: "/applyPath", equals: "platform-profile" }] } },
        { id: "activate-profile", label: message(namespace, "action.activateProfile", "发布平台配置"), icon: "publish", placement: "page.primary", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.profile.publish"], confirm: message(namespace, "confirm.activateProfile", "将依次激活 Catalog、发布 Deployment 并等待 readiness；失败会执行双重单调回滚。"), visibleWhen: { all: [{ pointer: "/candidateExternalStatus", equals: "Approved" }, { pointer: "/applyPath", equals: "platform-profile" }] } },
        { id: "abort-profile", label: message(namespace, "action.abortProfile", "放弃平台配置"), icon: "remove", placement: "page.secondary", tone: "danger", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.profile.publish"], confirm: message(namespace, "confirm.abortProfile", "放弃候选并释放目标服务的 Platform Profile 激活锁？"), visibleWhen: { all: [{ pointer: "/candidateStatus", equals: "Publishing" }, { pointer: "/applyPath", equals: "platform-profile" }] } },
        { id: "submit-hot", label: message(namespace, "action.submitHot", "准备热配置"), icon: "publish", placement: "page.primary", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.hot.publish"], confirm: message(namespace, "confirm.submitHot", "将托管凭证置为候选并要求目标插件准备新配置；当前活动配置不会改变。"), visibleWhen: { all: [{ pointer: "/candidateStatus", equals: "Draft" }, { pointer: "/applyPath", equals: "hot-service" }, { pointer: "/controllerAvailable", equals: true }] } },
        { id: "approve-hot", label: message(namespace, "action.approveHot", "批准热配置"), icon: "success", placement: "page.primary", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.hot.publish"], confirm: message(namespace, "confirm.approveHot", "确认以不同主体批准已由目标插件准备的服务级热配置？"), visibleWhen: { all: [{ pointer: "/candidateExternalStatus", equals: "PendingApproval" }, { pointer: "/applyPath", equals: "hot-service" }] } },
        { id: "activate-hot", label: message(namespace, "action.activateHot", "激活热配置"), icon: "publish", placement: "page.primary", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.hot.publish"], confirm: message(namespace, "confirm.activateHot", "由目标插件先原子切换配置，再激活候选凭证；中断后会依据已提交事实恢复。"), visibleWhen: { all: [{ pointer: "/candidateExternalStatus", equals: "Approved" }, { pointer: "/applyPath", equals: "hot-service" }] } },
        { id: "abort-hot", label: message(namespace, "action.abortHot", "放弃热配置"), icon: "remove", placement: "page.secondary", tone: "danger", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.hot.publish"], confirm: message(namespace, "confirm.abortHot", "放弃已准备但未提交的热配置，并回滚候选凭证？"), visibleWhen: { all: [{ pointer: "/candidateStatus", equals: "Publishing" }, { pointer: "/applyPath", equals: "hot-service" }] } },
		{ id: "submit-scoped", label: message(namespace, "action.submitScoped", "提交范围配置"), icon: "publish", placement: "page.primary", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.scoped.publish"], confirm: message(namespace, "confirm.submitScoped", "提交 Tenant/User 范围配置进入异人审批；当前 Active 不会改变。"), visibleWhen: { all: [{ pointer: "/candidateStatus", equals: "Draft" }, { pointer: "/applyPath", equals: "hot-scoped" }] } },
		{ id: "approve-scoped", label: message(namespace, "action.approveScoped", "批准范围配置"), icon: "success", placement: "page.primary", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.scoped.publish"], confirm: message(namespace, "confirm.approveScoped", "确认以不同主体批准该 Tenant/User 范围配置？"), visibleWhen: { all: [{ pointer: "/candidateExternalStatus", equals: "PendingApproval" }, { pointer: "/applyPath", equals: "hot-scoped" }] } },
		{ id: "activate-scoped", label: message(namespace, "action.activateScoped", "激活范围配置"), icon: "publish", placement: "page.primary", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.scoped.publish"], confirm: message(namespace, "confirm.activateScoped", "以 Active revision/digest CAS 原子切换，并通知运行时重新 resolve。"), visibleWhen: { all: [{ pointer: "/candidateExternalStatus", equals: "Approved" }, { pointer: "/applyPath", equals: "hot-scoped" }] } },
		{ id: "abort-scoped", label: message(namespace, "action.abortScoped", "放弃范围配置"), icon: "remove", placement: "page.secondary", tone: "danger", requiresSelection: true, requiredPermissions: ["platform.plugin-configuration.scoped.publish"], confirm: message(namespace, "confirm.abortScoped", "放弃该 Tenant/User 范围候选？"), visibleWhen: { all: [{ pointer: "/candidateStatus", equals: "Publishing" }, { pointer: "/applyPath", equals: "hot-scoped" }] } },
      ],
    },
    forms: [form],
    async load(query: CollectionQuery, signal) {
      const [baseDefinitions, candidates] = await Promise.all([client.listPluginConfigurationDefinitions(), client.listPluginConfigurationCandidates()]);
      if (signal.aborted) return { items: [], total: 0 };
	  const scopeSubjectId = typeof query.filters.scopeSubjectId === "string" ? query.filters.scopeSubjectId.trim() : "";
	  const definitions = scopeSubjectId === "" ? baseDefinitions : await Promise.all(baseDefinitions.map((definition) =>
		definition.scope === "user" ? client.getPluginConfigurationDefinition(definition.id, definition.catalogDigest, scopeSubjectId) : definition,
	  ));
	  if (signal.aborted) return { items: [], total: 0 };
	  const latest = latestCandidates(candidates, scopeSubjectId);
      const keyword = typeof query.filters.keyword === "string" ? query.filters.keyword.trim().toLowerCase() : "";
      const applyMode = typeof query.filters.applyMode === "string" ? query.filters.applyMode : "";
	  const rows = definitions.map((definition) => configurationRow(definition, latest.get(definition.id), scopeSubjectId)).filter((row) =>
        (keyword === "" || `${row.pluginName} ${row.pluginId} ${row.deployment} ${row.unitId}`.toLowerCase().includes(keyword)) &&
        (applyMode === "" || row.applyMode === applyMode),
      );
      const start = Math.max(0, (query.page - 1) * query.pageSize);
      return { items: rows.slice(start, start + query.pageSize), total: rows.length };
    },
    async runAction({ action, selected }) {
      const row = selected[0];
      if (row === undefined || row.candidateId === "") return;
      if (action.id === "discard") await client.discardPluginConfigurationDraft(row.candidateId, row.candidateRevision);
      if (action.id === "submit") await client.submitPluginConfigurationDraft(row.candidateId, row.candidateRevision);
      if (action.id === "activate") await client.activatePluginConfigurationCandidate(row.candidateId, row.candidateRevision);
      if (action.id === "submit-profile") await client.submitPlatformProfileConfigurationDraft(row.candidateId, row.candidateRevision);
      if (action.id === "approve-profile") await client.approvePlatformProfileConfigurationCandidate(row.candidateId, row.candidateRevision);
      if (action.id === "activate-profile") await client.activatePlatformProfileConfigurationCandidate(row.candidateId, row.candidateRevision);
      if (action.id === "abort-profile") await client.abortPlatformProfileConfigurationCandidate(row.candidateId, row.candidateRevision);
      if (action.id === "submit-hot") await client.submitHotServiceConfigurationDraft(row.candidateId, row.candidateRevision);
      if (action.id === "approve-hot") await client.approveHotServiceConfigurationCandidate(row.candidateId, row.candidateRevision);
      if (action.id === "activate-hot") await client.activateHotServiceConfigurationCandidate(row.candidateId, row.candidateRevision);
      if (action.id === "abort-hot") await client.abortHotServiceConfigurationCandidate(row.candidateId, row.candidateRevision);
	  if (action.id === "submit-scoped") await client.submitScopedConfigurationDraft(row.candidateId, row.candidateRevision);
	  if (action.id === "approve-scoped") await client.approveScopedConfigurationCandidate(row.candidateId, row.candidateRevision);
	  if (action.id === "activate-scoped") await client.activateScopedConfigurationCandidate(row.candidateId, row.candidateRevision);
	  if (action.id === "abort-scoped") await client.abortScopedConfigurationCandidate(row.candidateId, row.candidateRevision);
    },
  });
}

function configurationFormSchema(definition: PluginConfigurationDefinition): FormSchema {
  const secretProperties = Object.fromEntries((definition.managedCredentials ?? []).map((field) => [field.id, {
	type: "string", format: "vastplan-secret-material", writeOnly: true, title: field.title,
	...(field.description === undefined ? {} : { description: field.description }),
  }]));
  const configured = new Set((definition.credentialStates ?? []).filter((state) => state.configured).map((state) => state.fieldId));
  const requiredSecrets = (definition.managedCredentials ?? []).filter((field) => field.required === true && !configured.has(field.id)).map((field) => field.id);
  const secretsSchema: Record<string, unknown> = { type: "object", additionalProperties: false, properties: secretProperties };
  if (requiredSecrets.length > 0) secretsSchema.required = requiredSecrets;
  return {
	id: `plugin-configuration.${definition.id}`,
	schema: {
	  type: "object", additionalProperties: false,
	  properties: {
		scopeSubjectId: definition.scope === "user" ? { type: "string", title: "目标用户主体 ID", minLength: 1, maxLength: 256 } : { type: "string", readOnly: true },
		values: definition.schema, secrets: secretsSchema,
	  },
	  required: [...(definition.scope === "user" ? ["scopeSubjectId"] : []), "values", ...(requiredSecrets.length === 0 ? [] : ["secrets"])],
	} as FormSchema["schema"],
  };
}

function asRecord(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

function latestCandidates(candidates: readonly PluginConfigurationCandidate[], scopeSubjectId = ""): Map<string, PluginConfigurationCandidate> {
  const latest = new Map<string, PluginConfigurationCandidate>();
  for (const candidate of candidates) {
	if (candidate.applyPath === "hot-scoped" && (candidate.scopeSubjectId ?? "") !== scopeSubjectId) continue;
    const current = latest.get(candidate.configurationId);
    if (current === undefined || candidate.updatedAt > current.updatedAt) latest.set(candidate.configurationId, candidate);
  }
  return latest;
}

function configurationRow(definition: PluginConfigurationDefinition, candidate: PluginConfigurationCandidate | undefined, scopeSubjectId = ""): ConfigurationRow {
  return {
    ...definition,
    candidateStatus: candidate?.status ?? "None",
    candidateId: candidate?.id ?? "",
    candidateRevision: candidate?.revision ?? 0,
    candidateExternalStatus: candidate?.externalStatus ?? "None",
	  scopeSubjectId: candidate?.scopeSubjectId ?? (definition.scope === "user" ? scopeSubjectId : ""),
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
      context.addRecordPage(createPluginConfigurationResourcePage(createBrowserPlatformAdminClient(context.portal.id, service.id), service.id, `/settings/plugin-configuration-resources${suffix}`));
    }
  },
  localization: { defaultLocale: "zh-CN", messages: {
    "zh-CN": {
      "page.title":"插件配置","page.titleService":"插件配置 · {service}","page.description":"按已验签插件清单创建候选；重启配置走 Deployment，服务级热配置由插件控制器原子提交，两者均需独立审批。",
      "filter.keyword":"插件或服务","filter.applyMode":"生效方式","mode.restart":"重启发布","mode.hot":"热配置",
      "column.plugin":"插件","column.deployment":"部署","column.unit":"服务单元","column.origin":"管理来源","column.scope":"作用域","column.applyMode":"生效方式","column.controller":"热控制器","column.credentials":"托管字段","column.candidate":"候选状态","column.external":"外部发布",
      "form.title":"编辑配置草稿","form.description":"保存只创建候选草稿，不会立即重启服务或改变活动配置。","action.saveDraft":"保存草稿","notice.draftSaved":"配置草稿已保存","action.edit":"编辑配置草稿","action.discard":"放弃草稿","confirm.discard":"放弃所选草稿？该操作使用候选 revision CAS。","action.submit":"提交审批","confirm.submit":"提交后将创建独立服务修订，并由另一位审批人审批。","action.activate":"发布并激活","confirm.activate":"发布已审批修订；readiness 失败将自动创建单调回滚修订。","action.submitProfile":"提交平台配置审批","confirm.submitProfile":"该变更会修改目标服务绑定的 Platform Profile，必须由另一位授权主体审批。","action.approveProfile":"批准平台配置","confirm.approveProfile":"确认以不同主体批准该 Platform Profile 配置候选？","action.activateProfile":"发布平台配置","confirm.activateProfile":"将依次激活 Catalog、发布 Deployment 并等待 readiness；失败会执行双重单调回滚。","action.abortProfile":"放弃平台配置","confirm.abortProfile":"放弃候选并释放目标服务的 Platform Profile 激活锁？","action.submitHot":"准备热配置","confirm.submitHot":"将托管凭证置为候选并要求目标插件准备新配置；当前活动配置不会改变。","action.approveHot":"批准热配置","confirm.approveHot":"确认以不同主体批准已由目标插件准备的服务级热配置？","action.activateHot":"激活热配置","confirm.activateHot":"由目标插件先原子切换配置，再激活候选凭证；中断后会依据已提交事实恢复。","action.abortHot":"放弃热配置","confirm.abortHot":"放弃已准备但未提交的热配置，并回滚候选凭证？"
    },
    "en-US": {
      "page.title":"Plugin configuration","page.titleService":"Plugin configuration · {service}","page.description":"Create candidates from signed contracts. Restart changes use Deployment; service hot changes use an atomic plugin controller. Both require independent approval.",
      "filter.keyword":"Plugin or service","filter.applyMode":"Apply mode","mode.restart":"Restart publication","mode.hot":"Hot configuration",
      "column.plugin":"Plugin","column.deployment":"Deployment","column.unit":"Service unit","column.origin":"Managed by","column.scope":"Scope","column.applyMode":"Apply mode","column.controller":"Hot controller","column.credentials":"Managed fields","column.candidate":"Candidate status","column.external":"External publication",
      "form.title":"Edit configuration draft","form.description":"Saving creates a candidate draft only; it does not restart services or change active configuration.","action.saveDraft":"Save draft","notice.draftSaved":"Configuration draft saved","action.edit":"Edit draft","action.discard":"Discard draft","confirm.discard":"Discard the selected draft using its candidate revision?","action.submit":"Submit for approval","confirm.submit":"Submission creates a separate service revision for another subject to approve.","action.activate":"Publish and activate","confirm.activate":"Publish the approved revision; readiness failure creates a monotonic rollback revision.","action.submitProfile":"Submit platform configuration","confirm.submitProfile":"This changes the target binding's Platform Profile and requires approval by another authorized subject.","action.approveProfile":"Approve platform configuration","confirm.approveProfile":"Approve this Platform Profile candidate as a different subject?","action.activateProfile":"Publish platform configuration","confirm.activateProfile":"Activate the Catalog, publish the Deployment, and wait for readiness; failure performs monotonic compensation.","action.abortProfile":"Abort platform configuration","confirm.abortProfile":"Abort the candidate and release the target Platform Profile activation lock?","action.submitHot":"Prepare hot configuration","confirm.submitHot":"Stage managed credentials and ask the target plugin to prepare the new configuration without changing the active generation.","action.approveHot":"Approve hot configuration","confirm.approveHot":"Approve the service hot configuration as a different subject after the target plugin has prepared it?","action.activateHot":"Activate hot configuration","confirm.activateHot":"Atomically switch the target plugin configuration first, then activate candidate credentials; interruptions recover from the committed fact.","action.abortHot":"Abort hot configuration","confirm.abortHot":"Abort the prepared configuration and roll back staged credentials?"
    }
  } },
};
