import { defineCollectionPage, jsonSchemaDialect, message, type CollectionPageDefinition, type CollectionQuery, type WorkbenchFormDefinition, type WorkbenchFormFieldErrors, type WorkbenchFormSubmitResult } from "@vastplan/workbench-sdk";
import type { PublishedWorkflowDefinition, WorkflowBinding, WorkflowCatalog, WorkflowDefinition, WorkflowInstance, WorkflowManagementClient, WorkflowTask } from "./management-client.js";

const namespace = "cn.vastplan.platform.workflow.orchestrator";
type Title = ReturnType<typeof message>;

interface CatalogRow extends Record<string, unknown> { id: string; kind: string; contract: string; title: string; owner: string; }
interface DefinitionRow extends Record<string, unknown> { id: string; revision: number; featureId: string; entryNodeId: string; nodeCount: number; digest: string; publishedBy: string; publishedAt: string; definition: WorkflowDefinition; }
interface BindingRow extends Record<string, unknown> { serviceId: string; featureId: string; definitionId: string; definitionRevision: number; digest: string; revision: number; updatedAt: string; updatedBy: string; binding: WorkflowBinding; }
interface InstanceRow extends Record<string, unknown> { id: string; featureId: string; resource: string; mode: string; status: string; currentNodeId: string; revision: number; updatedAt: string; instance: WorkflowInstance; }
interface TaskRow extends Record<string, unknown> { id: string; instanceId: string; title: string; outcomes: string; revision: number; createdAt: string; task: WorkflowTask; }

export function createWorkflowPages(client: WorkflowManagementClient, serviceID: string, suffix: string, label: string): readonly CollectionPageDefinition[] {
  return [catalogPage(client, serviceID, suffix, label), definitionsPage(client, serviceID, suffix, label), bindingsPage(client, serviceID, suffix, label), instancesPage(client, serviceID, suffix, label), tasksPage(client, serviceID, suffix, label)];
}

function catalogPage(client: WorkflowManagementClient, serviceID: string, suffix: string, label: string): CollectionPageDefinition<CatalogRow> {
  return defineCollectionPage({
    id: `platform.workflow.catalog.${serviceID}`, path: `/settings/workflows${suffix}/catalog`, title: message(namespace, "catalog.title", `流程目录 · ${label}`),
    description: message(namespace, "catalog.description", "查看签名功能点、节点模板和运行时 Provider"), requiredPermissions: ["platform.workflow.read"],
    navigation: navigation(serviceID, "catalog", message(namespace, "catalog.navigation", "流程目录"), 10),
    collection: { id: `platform.workflow.catalog.${serviceID}`, title: message(namespace, "catalog.title", "流程目录"), view: "table", query: pageQuery(), columns: [
      { key: "kind", label: message(namespace, "column.kind", "类型"), defaultVisible: true, minWidth: 120 },
      { key: "id", label: message(namespace, "column.id", "ID"), defaultVisible: true, minWidth: 260 },
      { key: "contract", label: message(namespace, "column.contract", "契约"), defaultVisible: true, minWidth: 110 },
      { key: "title", label: message(namespace, "column.title", "名称"), defaultVisible: true, minWidth: 180 },
      { key: "owner", label: message(namespace, "column.owner", "签名所有者"), defaultVisible: true, minWidth: 280 },
    ] },
    async load(query, signal) { return paged(catalogRows(await client.catalog()), query, signal); },
  });
}

function definitionsPage(client: WorkflowManagementClient, serviceID: string, suffix: string, label: string): CollectionPageDefinition<DefinitionRow> {
  const form: WorkbenchFormDefinition<DefinitionRow> = {
    id: "publish", schema: textSchema("workflow-definition.v1", "definitionJSON", "流程定义 JSON"),
    presentation: { layout: "vertical", fields: [{ pointer: "/definitionJSON", widget: "textarea" }] },
    workflow: { title: message(namespace, "definition.publish", "发布流程定义"), description: message(namespace, "definition.help", "定义发布后不可修改；后续变更必须使用连续的新修订。"), dialogWidth: "lg", submitLabel: message(namespace, "action.publish", "发布"), success: { notify: message(namespace, "definition.published", "流程定义已发布"), refreshCollection: true, close: true } },
    initialValue: { definitionJSON: JSON.stringify(exampleDefinition(), null, 2) },
    async validate({ value }) { return jsonFieldErrors(value.definitionJSON, "definitionJSON"); },
    async submit({ value }): Promise<WorkbenchFormSubmitResult | void> {
      const definition = parseJSON(value.definitionJSON) as WorkflowDefinition | undefined;
      if (definition === undefined) return { fieldErrors: { definitionJSON: message(namespace, "error.json", "请输入有效 JSON") } };
      await client.publishDefinition(definition);
    },
  };
  return defineCollectionPage({
    id: `platform.workflow.definitions.${serviceID}`, path: `/settings/workflows${suffix}/definitions`, title: message(namespace, "definitions.title", `流程定义 · ${label}`),
    description: message(namespace, "definitions.description", "管理不可变流程修订和锁定后的 Core 执行图"), requiredPermissions: ["platform.workflow.read"],
    navigation: navigation(serviceID, "definitions", message(namespace, "definitions.navigation", "流程定义"), 20),
    pageActions: [{ id: "publish", label: message(namespace, "definition.publish", "发布流程定义"), icon: "publish", tone: "primary", form: "publish", requiredPermissions: ["platform.workflow.manage"] }],
    collection: { id: `platform.workflow.definitions.${serviceID}`, title: message(namespace, "definitions.title", "流程定义"), view: "table", query: pageQuery(), columns: [
      { key: "id", label: message(namespace, "column.id", "ID"), defaultVisible: true, minWidth: 240 },
      { key: "revision", label: message(namespace, "column.revision", "修订"), format: "number", defaultVisible: true, minWidth: 80 },
      { key: "featureId", label: message(namespace, "column.feature", "功能点"), defaultVisible: true, minWidth: 240 },
      { key: "nodeCount", label: message(namespace, "column.nodes", "节点数"), format: "number", defaultVisible: true, minWidth: 90 },
      { key: "publishedAt", label: message(namespace, "column.updatedAt", "发布时间"), format: "datetime", defaultVisible: true, minWidth: 180 },
    ] }, forms: [form],
    async load(query, signal) { return paged((await client.listDefinitions()).map(definitionRow), query, signal); },
  });
}

function bindingsPage(client: WorkflowManagementClient, serviceID: string, suffix: string, label: string): CollectionPageDefinition<BindingRow> {
  const form: WorkbenchFormDefinition<BindingRow> = {
    id: "bind", schema: bindingSchema(), context: { editing: false },
    presentation: { layout: "vertical", navigation: "sections", sections: [{ id: "binding", title: message(namespace, "binding.section", "精确定义绑定"), columns: 2, fields: ["/featureId", "/definitionId", "/definitionRevision", "/digest", "/expectedRevision"] }], fields: [{ pointer: "/digest", span: 2 }, { pointer: "/expectedRevision", widget: "number" }] },
    workflow: { title: message(namespace, "binding.bind", "绑定流程定义"), description: message(namespace, "binding.help", "绑定使用精确定义修订和摘要；更新时必须提供当前绑定修订。"), dialogWidth: "md", submitLabel: message(namespace, "action.save", "保存"), success: { notify: message(namespace, "binding.saved", "流程绑定已保存"), refreshCollection: true, close: true } },
    initialValue: { expectedRevision: 0 },
    async load(selected) { const row = selected[0]; return row === undefined ? { expectedRevision: 0 } : { featureId: row.featureId, definitionId: row.definitionId, definitionRevision: row.definitionRevision, digest: row.digest, expectedRevision: row.revision }; },
    async submit({ value }): Promise<WorkbenchFormSubmitResult | void> {
      if (typeof value.featureId !== "string" || typeof value.definitionId !== "string" || typeof value.digest !== "string" || !Number.isSafeInteger(value.definitionRevision) || !Number.isSafeInteger(value.expectedRevision)) return { fieldErrors: { featureId: message(namespace, "error.required", "请完整填写绑定信息") } };
      await client.bindDefinition(value.featureId, { id: value.definitionId, revision: Number(value.definitionRevision), digest: value.digest }, Number(value.expectedRevision));
    },
  };
  return defineCollectionPage({
    id: `platform.workflow.bindings.${serviceID}`, path: `/settings/workflows${suffix}/bindings`, title: message(namespace, "bindings.title", `服务绑定 · ${label}`),
    description: message(namespace, "bindings.description", "为当前服务的功能点选择精确流程修订"), requiredPermissions: ["platform.workflow.read"], navigation: navigation(serviceID, "bindings", message(namespace, "bindings.navigation", "服务绑定"), 30),
    pageActions: [{ id: "bind", label: message(namespace, "binding.bind", "绑定流程定义"), icon: "add", tone: "primary", form: "bind", requiredPermissions: ["platform.workflow.manage"] }],
    collection: { id: `platform.workflow.bindings.${serviceID}`, title: message(namespace, "bindings.title", "服务绑定"), view: "table", query: pageQuery(), columns: [
      { key: "featureId", label: message(namespace, "column.feature", "功能点"), defaultVisible: true, minWidth: 250 },
      { key: "definitionId", label: message(namespace, "column.definition", "流程定义"), defaultVisible: true, minWidth: 230 },
      { key: "definitionRevision", label: message(namespace, "column.revision", "修订"), format: "number", defaultVisible: true, minWidth: 80 },
      { key: "updatedBy", label: message(namespace, "column.updatedBy", "更新人"), defaultVisible: true, minWidth: 140 },
      { key: "updatedAt", label: message(namespace, "column.updatedAt", "更新时间"), format: "datetime", defaultVisible: true, minWidth: 180 },
    ], actions: [{ id: "edit-binding", label: message(namespace, "action.edit", "编辑"), icon: "edit", placement: "record.row", form: "bind", requiredPermissions: ["platform.workflow.manage"] }] }, forms: [form],
    async load(query, signal) { return paged((await client.listBindings()).map(bindingRow), query, signal); },
  });
}

function instancesPage(client: WorkflowManagementClient, serviceID: string, suffix: string, label: string): CollectionPageDefinition<InstanceRow> {
  const form: WorkbenchFormDefinition<InstanceRow> = {
    id: "cancel", schema: textSchema("workflow-cancel.v1", "reason", "取消原因"), presentation: { layout: "vertical", fields: [{ pointer: "/reason", widget: "textarea" }] },
    workflow: { title: message(namespace, "instance.cancel", "取消流程实例"), dialogWidth: "sm", submitLabel: message(namespace, "action.cancel", "取消实例"), success: { notify: message(namespace, "instance.cancelled", "流程实例已取消"), refreshCollection: true, close: true } },
    async submit({ value, selected }) { const row = selected[0]; if (row === undefined || typeof value.reason !== "string" || value.reason.trim() === "") return { fieldErrors: { reason: message(namespace, "error.required", "请填写取消原因") } }; await client.cancelInstance(row.id, row.revision, value.reason); },
  };
  return defineCollectionPage({
    id: `platform.workflow.instances.${serviceID}`, path: `/settings/workflows${suffix}/instances`, title: message(namespace, "instances.title", `流程实例 · ${label}`),
    description: message(namespace, "instances.description", "查看流程、直通、挂起和终态实例"), requiredPermissions: ["platform.workflow.read"], navigation: navigation(serviceID, "instances", message(namespace, "instances.navigation", "流程实例"), 40),
    collection: { id: `platform.workflow.instances.${serviceID}`, title: message(namespace, "instances.title", "流程实例"), view: "table", query: pageQuery(), columns: [
      { key: "id", label: message(namespace, "column.id", "ID"), defaultVisible: true, minWidth: 260 }, { key: "featureId", label: message(namespace, "column.feature", "功能点"), defaultVisible: true, minWidth: 240 },
      { key: "resource", label: message(namespace, "column.resource", "资源"), defaultVisible: true, minWidth: 220 }, { key: "mode", label: message(namespace, "column.mode", "模式"), defaultVisible: true, minWidth: 100 },
      { key: "status", label: message(namespace, "column.status", "状态"), defaultVisible: true, minWidth: 110 }, { key: "updatedAt", label: message(namespace, "column.updatedAt", "更新时间"), format: "datetime", defaultVisible: true, minWidth: 180 },
    ], actions: [{ id: "cancel-instance", label: message(namespace, "instance.cancel", "取消"), icon: "close", placement: "record.row", tone: "danger", form: "cancel", requiredPermissions: ["platform.workflow.cancel"] }] }, forms: [form],
    async load(query, signal) { return paged((await client.listInstances()).map(instanceRow), query, signal); },
  });
}

function tasksPage(client: WorkflowManagementClient, serviceID: string, suffix: string, label: string): CollectionPageDefinition<TaskRow> {
  const form: WorkbenchFormDefinition<TaskRow> = {
    id: "complete", schema: taskSchema(), presentation: { layout: "vertical", fields: [{ pointer: "/outcome" }, { pointer: "/comment", widget: "textarea" }] },
    workflow: { title: message(namespace, "task.complete", "处理人工任务"), dialogWidth: "sm", submitLabel: message(namespace, "action.submit", "提交结果"), success: { notify: message(namespace, "task.completed", "任务已处理"), refreshCollection: true, close: true } },
    async load(selected) { return { outcome: selected[0]?.task.allowedOutcomes[0] ?? "" }; },
    async submit({ value, selected }) { const row = selected[0]; if (row === undefined || typeof value.outcome !== "string" || !row.task.allowedOutcomes.includes(value.outcome)) return { fieldErrors: { outcome: message(namespace, "error.outcome", "结果必须来自任务允许的结果列表") } }; await client.completeTask(row.id, row.revision, value.outcome, typeof value.comment === "string" ? value.comment : undefined); },
  };
  return defineCollectionPage({
    id: `platform.workflow.tasks.${serviceID}`, path: `/settings/workflows${suffix}/tasks`, title: message(namespace, "tasks.title", `待办任务 · ${label}`),
    description: message(namespace, "tasks.description", "只显示当前主体角色可处理的待办"), requiredPermissions: ["platform.workflow.read"], navigation: navigation(serviceID, "tasks", message(namespace, "tasks.navigation", "待办任务"), 50),
    collection: { id: `platform.workflow.tasks.${serviceID}`, title: message(namespace, "tasks.title", "待办任务"), view: "table", query: pageQuery(), columns: [
      { key: "title", label: message(namespace, "column.title", "任务"), defaultVisible: true, minWidth: 220 }, { key: "instanceId", label: message(namespace, "column.instance", "流程实例"), defaultVisible: true, minWidth: 260 },
      { key: "outcomes", label: message(namespace, "column.outcomes", "允许结果"), defaultVisible: true, minWidth: 180 }, { key: "createdAt", label: message(namespace, "column.createdAt", "创建时间"), format: "datetime", defaultVisible: true, minWidth: 180 },
    ], actions: [{ id: "complete-task", label: message(namespace, "task.complete", "处理"), icon: "success", placement: "record.row", form: "complete", requiredPermissions: ["platform.workflow.task.complete"] }] }, forms: [form],
    async load(query, signal) { return paged((await client.listTasks()).map(taskRow), query, signal); },
  });
}

function navigation(serviceID: string, id: string, label: Title, order: number) { return { id: `platform.workflow.${id}.${serviceID}`, label, parentMenuRef: { pluginID: namespace, nodeID: "workflow-management" }, managementServiceID: serviceID, order }; }
function pageQuery() { return { mode: "page" as const, defaultPageSize: 20, pageSizeOptions: [20, 50, 100] }; }
function paged<Row>(items: readonly Row[], query: CollectionQuery, signal: AbortSignal) { if (signal.aborted) return { items: [] as Row[], total: 0 }; const start = Math.max(0, (query.page - 1) * query.pageSize); return { items: items.slice(start, start + query.pageSize), total: items.length }; }
function catalogRows(catalog: WorkflowCatalog): CatalogRow[] { return [
  ...catalog.features.map(({ descriptor, owner }) => ({ id: descriptor.id, kind: "feature", contract: descriptor.contract, title: descriptor.resourceKind, owner: `${owner.ref.pluginId}@${owner.ref.version}` })),
  ...catalog.templates.map(({ descriptor, owner }) => ({ id: descriptor.id, kind: "template", contract: descriptor.contract, title: descriptor.title, owner: `${owner.ref.pluginId}@${owner.ref.version}` })),
  ...catalog.providers.map(({ descriptor, owner }) => ({ id: descriptor.id, kind: "provider", contract: descriptor.contract, title: descriptor.title, owner: `${owner.ref.pluginId}@${owner.ref.version}` })),
]; }
function definitionRow(value: PublishedWorkflowDefinition): DefinitionRow { return { id: value.definition.id, revision: value.definition.revision, featureId: value.definition.featureId, entryNodeId: value.definition.entryNodeId, nodeCount: value.definition.nodes.length, digest: value.ref.digest, publishedBy: value.publishedBy, publishedAt: value.publishedAt, definition: value.definition }; }
function bindingRow(value: WorkflowBinding): BindingRow { return { serviceId: value.serviceId, featureId: value.featureId, definitionId: value.definition.id, definitionRevision: value.definition.revision, digest: value.definition.digest, revision: value.revision, updatedAt: value.updatedAt, updatedBy: value.updatedBy, binding: value }; }
function instanceRow(value: WorkflowInstance): InstanceRow { return { id: value.id, featureId: value.featureId, resource: `${value.resource.kind}/${value.resource.id}`, mode: value.mode, status: value.status, currentNodeId: value.currentNodeId ?? "", revision: value.revision, updatedAt: value.updatedAt, instance: value }; }
function taskRow(value: WorkflowTask): TaskRow { return { id: value.id, instanceId: value.instanceId, title: value.title, outcomes: value.allowedOutcomes.join(", "), revision: value.revision, createdAt: value.createdAt, task: value }; }
function parseJSON(value: unknown): unknown | undefined { if (typeof value !== "string") return undefined; try { return JSON.parse(value) as unknown; } catch { return undefined; } }
function jsonFieldErrors(value: unknown, field: string): WorkbenchFormFieldErrors { return parseJSON(value) === undefined ? { [field]: message(namespace, "error.json", "请输入有效 JSON") } : {}; }
function textSchema(id: string, field: string, title: string) { return { id, schema: { $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: [field], properties: { [field]: { type: "string", title, minLength: 1 } } }, localization: { [`/properties/${field}/title`]: message(namespace, `${id}.${field}`, title) } }; }
function bindingSchema() { return { id: "workflow-binding.v1", schema: { $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["featureId", "definitionId", "definitionRevision", "digest", "expectedRevision"], properties: { featureId: { type: "string", title: "功能点 ID" }, definitionId: { type: "string", title: "流程定义 ID" }, definitionRevision: { type: "integer", title: "定义修订", minimum: 1 }, digest: { type: "string", title: "定义摘要", pattern: "^[a-f0-9]{64}$" }, expectedRevision: { type: "integer", title: "当前绑定修订", minimum: 0 } } } }; }
function taskSchema() { return { id: "workflow-task-completion.v1", schema: { $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["outcome"], properties: { outcome: { type: "string", title: "处理结果" }, comment: { type: "string", title: "备注", maxLength: 2000 } } } }; }
function exampleDefinition(): WorkflowDefinition { return { id: "platform.example.approval", revision: 1, featureId: "platform.example.feature", entryNodeId: "review", nodes: [
  { id: "review", type: { id: "workflow.core.manual-task", contract: "1.0.0" }, title: "Review", roles: ["platform.approver"], outcomes: { approved: "execute", rejected: "rejected" } },
  { id: "execute", type: { id: "workflow.core.action", contract: "1.0.0" }, actionId: "platform.example.execute", next: "done" },
  { id: "done", type: { id: "workflow.core.end", contract: "1.0.0" }, result: "succeeded" }, { id: "rejected", type: { id: "workflow.core.end", contract: "1.0.0" }, result: "rejected" },
] }; }
