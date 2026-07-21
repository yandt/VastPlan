import {
  createBrowserPlatformAdminClient,
  type BackendApplicationComposition,
  type BackendPluginRef,
  type BackendServiceUnit,
  type DeploymentTarget,
  type PlatformAdminClient,
  type ServiceAuditEvent,
  type ServiceRevision,
} from "@vastplan/platform-admin";
import {
  defineCollectionPage,
  jsonSchemaDialect,
  managementServicesFor,
  message as localizedMessage,
  type CollectionPageDefinition,
  type CollectionQuery,
  type FormSchema,
  type JSONValue,
  type WorkbenchFormDefinition,
  type WorkbenchFrontendPluginContext,
} from "@vastplan/workbench-sdk";

const namespace = "cn.vastplan.platform.infrastructure.deployment-manager";

type EditorValue = Record<string, unknown>;

export function serviceCompositionSchema(targets: DeploymentTarget[]): FormSchema {
  return {
    id: "backend-service-composition.v1",
    schema: {
      $schema: jsonSchemaDialect,
      title: "Backend Application Composition",
      type: "object",
      additionalProperties: false,
      required: ["deployment", "units"],
      properties: {
        deployment: {
          type: "string", title: "部署目标", minLength: 1,
          oneOf: targets.map((target) => ({ const: target.deploymentName, title: target.deploymentName })),
        },
        units: {
          type: "array", title: "应用服务", minItems: 1,
          items: {
            type: "object", additionalProperties: false, required: ["serviceClass", "id", "plugins", "replicas"],
            properties: {
              serviceClass: { type: "string", title: "服务分类", default: "application.backend", pattern: "^[a-z][a-z0-9._-]{0,127}$" },
              id: { type: "string", title: "服务 ID", pattern: "^[a-z][a-z0-9._-]{0,127}$" },
              logicalService: { type: "string", title: "逻辑服务名", pattern: "^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$" },
              plugins: {
                type: "array", title: "应用插件", minItems: 1,
                items: {
                  type: "object", additionalProperties: false, required: ["id", "version"],
                  properties: {
                    id: { type: "string", title: "插件 ID", pattern: "^[a-z0-9]+(?:[.-][a-z0-9]+)+$" },
                    version: { type: "string", title: "精确版本", pattern: "^\\d+\\.\\d+\\.\\d+(?:[-+][0-9A-Za-z.-]+)?$" },
                    channel: { type: "string", title: "通道", default: "stable", oneOf: [{ const: "stable", title: "稳定版" }, { const: "preview", title: "预发布" }] },
                  },
                },
              },
              replicas: { type: "integer", title: "实例数", minimum: 1, maximum: 100, default: 1 },
              instancePolicy: { type: "string", title: "实例策略", default: "active-active", oneOf: [{ const: "active-active", title: "多活" }, { const: "leader", title: "单主" }, { const: "partitioned", title: "分区" }] },
              stateModel: { type: "string", title: "状态模型", default: "external-shared", oneOf: [{ const: "external-shared", title: "外部共享状态" }, { const: "leader-owned", title: "主节点状态" }, { const: "partition-owned", title: "分区状态" }, { const: "local-ephemeral", title: "本地临时状态" }] },
              routing: { type: "string", title: "路由", default: "queue", oneOf: [{ const: "queue", title: "队列" }, { const: "leader", title: "主节点" }, { const: "partition", title: "分区" }, { const: "direct", title: "直连" }] },
              routingDomain: { type: "string", title: "路由域" },
              partitionKeys: { type: "array", title: "分区键", uniqueItems: true, items: { type: "string", minLength: 1 } },
              dependsOn: { type: "array", title: "依赖服务 ID", uniqueItems: true, items: { type: "string", minLength: 1 } },
              nodeSelector: { type: "object", title: "节点标签选择", additionalProperties: { type: "string" }, default: {} },
              config: { type: "object", title: "非敏感配置", additionalProperties: true, default: {} },
            },
          },
        },
      },
    },
    uiSchema: {
      deployment: { "ui:widget": "select", "ui:help": "目标及 Platform Profile 由平台运维预授权，不能在此修改。" },
      units: {
        "ui:help": "这里只能组合应用插件；内核会注入基础插件并校验制品来源、依赖 DAG、实例策略和节点调度。",
        items: {
          plugins: { items: { channel: { "ui:widget": "select" } } },
          instancePolicy: { "ui:widget": "select" }, stateModel: { "ui:widget": "select" }, routing: { "ui:widget": "select" },
          config: { "ui:help": "密码、令牌等敏感值必须使用凭证引用，不能写入配置。" },
        },
      },
    },
    localization: {
      "/properties/deployment/title":localizedMessage(namespace,"form.deployment","部署目标"), "/properties/units/title":localizedMessage(namespace,"form.units","应用服务"), "/properties/units/items/properties/serviceClass/title":localizedMessage(namespace,"form.serviceClass","服务分类"), "/properties/units/items/properties/id/title":localizedMessage(namespace,"form.serviceId","服务 ID"), "/properties/units/items/properties/logicalService/title":localizedMessage(namespace,"form.logicalService","逻辑服务名"), "/properties/units/items/properties/plugins/title":localizedMessage(namespace,"form.plugins","应用插件"), "/properties/units/items/properties/plugins/items/properties/id/title":localizedMessage(namespace,"form.pluginId","插件 ID"), "/properties/units/items/properties/plugins/items/properties/version/title":localizedMessage(namespace,"form.version","精确版本"), "/properties/units/items/properties/plugins/items/properties/channel/title":localizedMessage(namespace,"form.channel","通道"), "/properties/units/items/properties/plugins/items/properties/channel/oneOf/0/title":localizedMessage(namespace,"form.stable","稳定版"), "/properties/units/items/properties/plugins/items/properties/channel/oneOf/1/title":localizedMessage(namespace,"form.preview","预发布"), "/properties/units/items/properties/replicas/title":localizedMessage(namespace,"form.replicas","实例数"), "/properties/units/items/properties/instancePolicy/title":localizedMessage(namespace,"form.instancePolicy","实例策略"), "/properties/units/items/properties/instancePolicy/oneOf/0/title":localizedMessage(namespace,"form.activeActive","多活"), "/properties/units/items/properties/instancePolicy/oneOf/1/title":localizedMessage(namespace,"form.leader","单主"), "/properties/units/items/properties/instancePolicy/oneOf/2/title":localizedMessage(namespace,"form.partitioned","分区"), "/properties/units/items/properties/stateModel/title":localizedMessage(namespace,"form.stateModel","状态模型"), "/properties/units/items/properties/stateModel/oneOf/0/title":localizedMessage(namespace,"form.externalShared","外部共享状态"), "/properties/units/items/properties/stateModel/oneOf/1/title":localizedMessage(namespace,"form.leaderOwned","主节点状态"), "/properties/units/items/properties/stateModel/oneOf/2/title":localizedMessage(namespace,"form.partitionOwned","分区状态"), "/properties/units/items/properties/stateModel/oneOf/3/title":localizedMessage(namespace,"form.localEphemeral","本地临时状态"), "/properties/units/items/properties/routing/title":localizedMessage(namespace,"form.routing","路由"), "/properties/units/items/properties/routing/oneOf/0/title":localizedMessage(namespace,"form.queue","队列"), "/properties/units/items/properties/routing/oneOf/1/title":localizedMessage(namespace,"form.leaderRoute","主节点"), "/properties/units/items/properties/routing/oneOf/2/title":localizedMessage(namespace,"form.partitionRoute","分区"), "/properties/units/items/properties/routing/oneOf/3/title":localizedMessage(namespace,"form.direct","直连"), "/properties/units/items/properties/routingDomain/title":localizedMessage(namespace,"form.routingDomain","路由域"), "/properties/units/items/properties/partitionKeys/title":localizedMessage(namespace,"form.partitionKeys","分区键"), "/properties/units/items/properties/dependsOn/title":localizedMessage(namespace,"form.dependsOn","依赖服务 ID"), "/properties/units/items/properties/nodeSelector/title":localizedMessage(namespace,"form.nodeSelector","节点标签选择"), "/properties/units/items/properties/config/title":localizedMessage(namespace,"form.config","非敏感配置")
    },
    uiLocalization: { "/deployment/ui:help":localizedMessage(namespace,"help.deployment","目标及 Platform Profile 由平台运维预授权，不能在此修改。"), "/units/ui:help":localizedMessage(namespace,"help.units","这里只能组合应用插件；内核会注入基础插件并校验制品来源、依赖 DAG、实例策略和节点调度。"), "/units/items/config/ui:help":localizedMessage(namespace,"help.config","密码、令牌等敏感值必须使用凭证引用，不能写入配置。") },
  };
}

export function buildBackendComposition(value: EditorValue, revision = 1): BackendApplicationComposition {
  const deployment = typeof value.deployment === "string" && value.deployment !== "" ? value.deployment : "deployment";
  return {
    version: 1, revision, id: deployment, target: { kernel: "backend" }, metadata: { name: deployment },
    units: Array.isArray(value.units) ? value.units.flatMap((item) => buildUnit(item)) : [],
  };
}

function buildUnit(value: unknown): BackendApplicationComposition["units"] {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return [];
  const item = value as Record<string, unknown>;
  if (typeof item.id !== "string" || typeof item.serviceClass !== "string") return [];
  const spec: BackendServiceUnit = {
    id: item.id, kind: "service", plugins: pluginRefs(item.plugins), enabled: true, service_role: "backend",
    replicas: typeof item.replicas === "number" ? item.replicas : 1,
    ...(text(item.logicalService) === undefined ? {} : { logical_service: text(item.logicalService) }),
    ...(text(item.instancePolicy) === undefined ? {} : { instance_policy: text(item.instancePolicy) }),
    ...(text(item.stateModel) === undefined ? {} : { state_model: text(item.stateModel) }),
    visibility: "cluster",
    ...(text(item.routing) === undefined ? {} : { routing: text(item.routing) }),
    ...(text(item.routingDomain) === undefined ? {} : { routing_domain: text(item.routingDomain) }),
    ...(strings(item.partitionKeys).length === 0 ? {} : { partition_keys: strings(item.partitionKeys) }),
    ...(strings(item.dependsOn).length === 0 ? {} : { depends_on: strings(item.dependsOn) }),
    ...(record(item.nodeSelector) === undefined ? {} : { placement: { nodeSelector: record(item.nodeSelector) } }),
    ...(record(item.config) === undefined ? {} : { config: record(item.config) }),
  };
  return [{ serviceClass: item.serviceClass, spec }];
}

function editorValue(revision: ServiceRevision): EditorValue {
  return { deployment: revision.deployment, units: revision.composition.units.map((unit) => ({
    serviceClass: unit.serviceClass, id: unit.spec.id, logicalService: unit.spec.logical_service ?? "", plugins: unit.spec.plugins,
    replicas: unit.spec.replicas, instancePolicy: unit.spec.instance_policy ?? "active-active", stateModel: unit.spec.state_model ?? "external-shared",
    routing: unit.spec.routing ?? "queue", routingDomain: unit.spec.routing_domain ?? "", partitionKeys: unit.spec.partition_keys ?? [],
    dependsOn: unit.spec.depends_on ?? [], nodeSelector: (unit.spec.placement?.nodeSelector as Record<string, unknown> | undefined) ?? {}, config: unit.spec.config ?? {},
  })) };
}

type DeploymentRow = ServiceRevision & Record<string, unknown>;

export function createDeploymentPage(client: PlatformAdminClient, serviceID: string, path: string, title: ReturnType<typeof localizedMessage>): CollectionPageDefinition<DeploymentRow> {
  const form = (id: "create" | "edit"): WorkbenchFormDefinition<DeploymentRow> => ({
    id,
    schema: serviceCompositionSchema([]),
    presentation: { layout: "vertical", navigation: "sections", sections: [{ id: "composition", title: localizedMessage(namespace,"panel.composition","应用服务组合"), columns: 1, fields: ["/deployment", "/units"] }], fields: [{ pointer: "/deployment" }, { pointer: "/units" }] },
    workflow: { surface: "drawer", size: "lg", title: localizedMessage(namespace, id === "create" ? "panel.new" : "panel.edit", id === "create" ? "新建服务草稿" : "编辑服务草稿"), submitLabel: localizedMessage(namespace, id === "create" ? "action.create" : "action.save", id === "create" ? "创建草稿" : "保存草稿"), success: { notify: localizedMessage(namespace, id === "create" ? "notice.created" : "notice.saved", id === "create" ? "草稿已创建" : "草稿已保存"), refreshCollection: true, close: true } },
    async prepare(_selected, signal) {
      const targets = await client.listDeploymentTargets();
      if (signal.aborted) return {};
      return { schema: serviceCompositionSchema(targets), ...(id === "create" ? { initialValue: { deployment: targets[0]?.deploymentName, units: [] } } : {}) };
    },
    ...(id === "edit" ? { async load(selected: readonly DeploymentRow[]) { return selected[0] === undefined ? { units: [] } : editorValue(selected[0]); } } : {}),
    async submit({ value, selected }) {
      const composition = buildBackendComposition(value, selected[0]?.composition.revision ?? 1);
      if (id === "create") await client.createServiceDraft(composition);
      else if (selected[0] !== undefined) await client.updateServiceDraft(selected[0].id, composition);
    },
  });
  const statusLabels = { Draft: localizedMessage(namespace,"status.draft","草稿"), PendingApproval: localizedMessage(namespace,"status.pendingApproval","待审批"), Approved: localizedMessage(namespace,"status.approved","已批准"), Publishing: localizedMessage(namespace,"status.publishing","发布中"), Published: localizedMessage(namespace,"status.published","已发布") };
  return defineCollectionPage<DeploymentRow>({
    id: `platform.deployment.${serviceID}`, path, title, description: localizedMessage(namespace,"page.description","在线组合应用服务、实例与调度并发布到 Node Agent 集群"), navigation: { id: `platform.deployment.${serviceID}`, label: title, zone: "settings", order: 60 },
    collection: { id: `platform.deployment.${serviceID}`, title, view: "table", query: { mode: "page", defaultPageSize: 20, pageSizeOptions: [20, 50, 100] },
      filters: [{ id: "deployment", label: localizedMessage(namespace,"column.deployment","部署"), kind: "text" }, { id: "status", label: localizedMessage(namespace,"column.status","状态"), kind: "select", options: Object.entries(statusLabels).map(([value, label]) => ({ value, label })) }],
      columns: [{ key: "id", label: "Revision", format: "number", defaultVisible: true, minWidth: 90 }, { key: "deployment", label: localizedMessage(namespace,"column.deployment","部署"), defaultVisible: true, minWidth: 180 }, { key: "status", label: localizedMessage(namespace,"column.status","状态"), format: "status", valueLabels: statusLabels, statusTones: { Draft: "neutral", PendingApproval: "warning", Approved: "info", Publishing: "warning", Published: "success" }, defaultVisible: true, minWidth: 110 }, { key: "active", label: localizedMessage(namespace,"column.active","活动"), format: "boolean", defaultVisible: true, minWidth: 80 }, { key: "updatedAt", label: localizedMessage(namespace,"column.updated","更新时间"), format: "datetime", defaultVisible: true, minWidth: 180 }], selection: "single",
      actions: [
        { id: "create", label: localizedMessage(namespace,"action.new","新建服务草稿"), placement: "page.primary", tone: "primary", form: "create" },
        { id: "edit", label: localizedMessage(namespace,"action.edit","编辑"), placement: "page.secondary", requiresSelection: true, form: "edit", visibleWhen: { pointer: "/status", equals: "Draft" } },
        { id: "submit", label: localizedMessage(namespace,"action.submit","提交审批"), placement: "page.secondary", requiresSelection: true, confirm: localizedMessage(namespace,"confirm.submit","提交后将重新执行可信解析，且不能继续编辑。"), visibleWhen: { pointer: "/status", equals: "Draft" } },
        { id: "approve", label: localizedMessage(namespace,"action.approve","批准"), placement: "page.secondary", requiresSelection: true, tone: "primary", confirm: localizedMessage(namespace,"confirm.approve","审批人与提交人必须不同。"), visibleWhen: { pointer: "/status", equals: "PendingApproval" } },
        { id: "publish", label: localizedMessage(namespace,"action.publish","发布"), placement: "page.secondary", requiresSelection: true, tone: "primary", confirm: localizedMessage(namespace,"confirm.publish","发布后 Controller 会把副本调度到符合条件的 Node Agent。"), visibleWhen: { pointer: "/status", in: ["Approved", "Publishing"] } },
        { id: "rollback", label: localizedMessage(namespace,"action.rollback","回滚到此版本"), placement: "page.secondary", requiresSelection: true, tone: "danger", confirm: localizedMessage(namespace,"confirm.rollback","回滚会用历史应用组合创建并发布一个新的单调 revision。"), visibleWhen: { all: [{ pointer: "/status", equals: "Published" }, { pointer: "/active", equals: false }] } },
        { id: "preview", label: localizedMessage(namespace,"action.preview","最终部署预览"), placement: "page.secondary", requiresSelection: true, overlay: "preview" },
        { id: "audit", label: localizedMessage(namespace,"action.audit","审计记录"), placement: "page.secondary", requiresSelection: true, overlay: "audit" },
      ] },
    forms: [form("create"), form("edit")],
    overlays: [
      { id: "preview", surface: "dialog", size: "lg", title: localizedMessage(namespace,"dialog.preview","内核解析后的 Deployment v2"), async load(selected) { return { kind: "json", documents: [{ value: (selected[0]?.preview ?? {}) as JSONValue }] }; } },
      { id: "audit", surface: "drawer", size: "lg", title: localizedMessage(namespace,"dialog.audit","服务组合审计"), async load(selected) { const rows = selected[0] === undefined ? [] : await client.listServiceRevisionAudit(selected[0].id); return { kind: "table", rowKey: "id", rows: rows as Array<ServiceAuditEvent & Record<string, unknown>>, columns: [{ key: "at", label: localizedMessage(namespace,"column.time","时间"), format: "datetime" }, { key: "action", label: localizedMessage(namespace,"column.action","动作") }, { key: "actorId", label: localizedMessage(namespace,"column.actor","操作者") }] }; } },
    ],
    async load(query: CollectionQuery, signal) { const deployment = typeof query.filters.deployment === "string" ? query.filters.deployment.trim().toLowerCase() : ""; const status = typeof query.filters.status === "string" ? query.filters.status : ""; const rows = (await client.listServiceRevisions()).filter((item) => (deployment === "" || item.deployment.toLowerCase().includes(deployment)) && (status === "" || item.status === status)) as DeploymentRow[]; if (signal.aborted) return { items: [], total: 0 }; const start = Math.max(0, (query.page - 1) * query.pageSize); return { items: rows.slice(start, start + query.pageSize), total: rows.length }; },
    async runAction({ action, selected }) { const item = selected[0]; if (item === undefined) return; if (action.id === "submit") await client.submitServiceDraft(item.id); else if (action.id === "approve") await client.approveServiceRevision(item.id); else if (action.id === "publish") await client.publishServiceRevision(item.id); else if (action.id === "rollback") await client.rollbackServiceRevision(item.id); return { notify: { title: action.label, kind: "success" } }; },
  });
}

function pluginRefs(value: unknown): BackendPluginRef[] { return Array.isArray(value) ? value.flatMap((item) => typeof item === "object" && item !== null && typeof (item as Record<string, unknown>).id === "string" && typeof (item as Record<string, unknown>).version === "string" ? [{ id: String((item as Record<string, unknown>).id), version: String((item as Record<string, unknown>).version), ...(text((item as Record<string, unknown>).channel) === undefined ? {} : { channel: text((item as Record<string, unknown>).channel) }) }] : []) : []; }
function text(value: unknown): string | undefined { return typeof value === "string" && value !== "" ? value : undefined; }
function strings(value: unknown): string[] { return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string" && item !== "") : []; }
function record(value: unknown): Record<string, unknown> | undefined { return typeof value === "object" && value !== null && !Array.isArray(value) && Object.keys(value).length > 0 ? JSON.parse(JSON.stringify(value)) as Record<string, unknown> : undefined; }
const deploymentManagerPlugin = {
  register(context: WorkbenchFrontendPluginContext) {
    const services = managementServicesFor(context.portal, "platform.deployment");
    if (services.length === 0) throw new Error("Portal 未绑定 platform.deployment 服务");
    for (const service of services) {
      const client = createBrowserPlatformAdminClient(context.portal.id, service.id);
      const suffix = services.length === 1 ? "" : `/${service.id}`;
      const label = context.i18n.message(services.length === 1 ? "page.title" : "page.titleService", services.length === 1 ? "服务与节点部署" : "服务与节点部署 · {service}", { service: service.label ?? service.id });
      context.addCollectionPage(createDeploymentPage(client, service.id, `/settings/deployment${suffix}`, label));
    }
  },
  localization:{defaultLocale:"zh-CN",messages:{
    "zh-CN":{"form.deployment":"部署目标","form.units":"应用服务","form.serviceClass":"服务分类","form.serviceId":"服务 ID","form.logicalService":"逻辑服务名","form.plugins":"应用插件","form.pluginId":"插件 ID","form.version":"精确版本","form.channel":"通道","form.stable":"稳定版","form.preview":"预发布","form.replicas":"实例数","form.instancePolicy":"实例策略","form.activeActive":"多活","form.leader":"单主","form.partitioned":"分区","form.stateModel":"状态模型","form.externalShared":"外部共享状态","form.leaderOwned":"主节点状态","form.partitionOwned":"分区状态","form.localEphemeral":"本地临时状态","form.routing":"路由","form.queue":"队列","form.leaderRoute":"主节点","form.partitionRoute":"分区","form.direct":"直连","form.routingDomain":"路由域","form.partitionKeys":"分区键","form.dependsOn":"依赖服务 ID","form.nodeSelector":"节点标签选择","form.config":"非敏感配置","help.deployment":"目标及 Platform Profile 由平台运维预授权，不能在此修改。","help.units":"这里只能组合应用插件；内核会注入基础插件并校验制品来源、依赖 DAG、实例策略和节点调度。","help.config":"密码、令牌等敏感值必须使用凭证引用，不能写入配置。","action.refresh":"刷新","action.new":"新建服务草稿","empty.targets":"没有平台预授权的部署目标","empty.targetsDescription":"请先由平台运维发布 Backend Platform Catalog 绑定。","panel.revisions":"服务组合 Revisions","empty.revisions":"尚无服务组合","column.deployment":"部署","column.status":"状态","status.activeSuffix":" · 活动","column.updated":"更新时间","panel.new":"新建服务草稿","action.create":"创建草稿","action.save":"保存草稿","field.kv":"控制面 KV revision","field.profile":"平台基线","field.submitted":"提交人","field.approved":"审批人","notice.created":"草稿已创建","notice.saved":"草稿已保存","notice.submitted":"已提交审批","confirm.submit":"提交后将重新执行可信解析，且不能继续编辑。","action.submit":"提交审批","notice.approved":"已批准","confirm.approve":"审批人与提交人必须不同。","action.approve":"批准","notice.published":"已发布","confirm.publish":"发布后 Controller 会把副本调度到符合条件的 Node Agent。","action.publish":"发布","notice.rolledBack":"已回滚","confirm.rollback":"回滚会用历史应用组合创建并发布一个新的单调 revision。","action.rollback":"回滚到此版本","notice.failed":"{title}失败","error.request":"服务编排请求失败","action.preview":"最终部署预览","action.audit":"审计记录","dialog.preview":"内核解析后的 Deployment v2","dialog.audit":"服务组合审计","empty.audit":"尚无审计记录","column.time":"时间","column.action":"动作","column.actor":"操作者","page.title":"服务与节点部署","page.titleService":"服务与节点部署 · {service}","page.description":"在线组合应用服务、实例与调度并发布到 Node Agent 集群"},
    "en-US":{"form.deployment":"Deployment target","form.units":"Application services","form.serviceClass":"Service class","form.serviceId":"Service ID","form.logicalService":"Logical service","form.plugins":"Application plugins","form.pluginId":"Plugin ID","form.version":"Exact version","form.channel":"Channel","form.stable":"Stable","form.preview":"Preview","form.replicas":"Replicas","form.instancePolicy":"Instance policy","form.activeActive":"Active-active","form.leader":"Leader","form.partitioned":"Partitioned","form.stateModel":"State model","form.externalShared":"External shared state","form.leaderOwned":"Leader-owned state","form.partitionOwned":"Partition-owned state","form.localEphemeral":"Local ephemeral state","form.routing":"Routing","form.queue":"Queue","form.leaderRoute":"Leader","form.partitionRoute":"Partition","form.direct":"Direct","form.routingDomain":"Routing domain","form.partitionKeys":"Partition keys","form.dependsOn":"Service dependencies","form.nodeSelector":"Node selector","form.config":"Non-sensitive config","help.deployment":"The target and Platform Profile are pre-authorized by platform operations and cannot be changed here.","help.units":"Only application plugins are composed here; the kernel injects foundation plugins and validates provenance, dependency DAG, instance policy, and scheduling.","help.config":"Sensitive values must use credential references and cannot be written to configuration.","action.refresh":"Refresh","action.new":"New service draft","empty.targets":"No pre-authorized deployment targets","empty.targetsDescription":"Platform operations must publish a Backend Platform Catalog binding first.","panel.revisions":"Service composition revisions","empty.revisions":"No service compositions","column.deployment":"Deployment","column.status":"Status","status.activeSuffix":" · Active","column.updated":"Updated","panel.new":"New service draft","action.create":"Create draft","action.save":"Save draft","field.kv":"Control-plane KV revision","field.profile":"Platform baseline","field.submitted":"Submitted by","field.approved":"Approved by","notice.created":"Draft created","notice.saved":"Draft saved","notice.submitted":"Submitted for approval","confirm.submit":"Trusted resolution runs again after submission and the draft can no longer be edited.","action.submit":"Submit","notice.approved":"Approved","confirm.approve":"The approver must be different from the submitter.","action.approve":"Approve","notice.published":"Published","confirm.publish":"After publishing, the Controller schedules replicas onto eligible Node Agents.","action.publish":"Publish","notice.rolledBack":"Rolled back","confirm.rollback":"Rollback creates and publishes a new monotonic revision from the historical application composition.","action.rollback":"Rollback to this version","notice.failed":"{title} failed","error.request":"Service composition request failed","action.preview":"Final deployment preview","action.audit":"Audit log","dialog.preview":"Kernel-resolved Deployment v2","dialog.audit":"Service composition audit","empty.audit":"No audit records","column.time":"Time","column.action":"Action","column.actor":"Actor","page.title":"Services and node deployment","page.titleService":"Services and node deployment · {service}","page.description":"Compose application services, replicas, and scheduling, then publish to the Node Agent cluster"}
  }},
};

Object.assign(deploymentManagerPlugin.localization.messages["zh-CN"], {
  "panel.composition": "应用服务组合", "panel.edit": "编辑服务草稿", "action.edit": "编辑", "column.active": "活动",
  "status.draft": "草稿", "status.pendingApproval": "待审批", "status.approved": "已批准", "status.publishing": "发布中", "status.published": "已发布",
});
Object.assign(deploymentManagerPlugin.localization.messages["en-US"], {
  "panel.composition": "Application service composition", "panel.edit": "Edit service draft", "action.edit": "Edit", "column.active": "Active",
  "status.draft": "Draft", "status.pendingApproval": "Pending approval", "status.approved": "Approved", "status.publishing": "Publishing", "status.published": "Published",
});

export default deploymentManagerPlugin;
