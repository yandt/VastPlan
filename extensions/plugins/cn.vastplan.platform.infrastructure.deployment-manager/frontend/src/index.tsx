import { useCallback, useEffect, useMemo, useState } from "react";
import {
  createBrowserPlatformAdminClient,
  type BackendApplicationComposition,
  type BackendPluginRef,
  type BackendServiceUnit,
  type DeploymentTarget,
  type PlatformAdminClient,
  type ServiceAuditEvent,
  type ServiceRevision,
  type ServiceRevisionStatus,
} from "@vastplan/platform-admin";
import { jsonSchemaDialect, managementServicesFor, message as localizedMessage, usePortalI18n, usePortalMessages, usePortalUI, type FormSchema, type FrontendPluginContext, type StatusTone } from "@vastplan/ui-primitives";

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

const tones: Record<ServiceRevisionStatus, StatusTone> = { Draft: "neutral", PendingApproval: "warning", Approved: "info", Publishing: "warning", Published: "success" };

export function DeploymentManagerView({ client }: { client: PlatformAdminClient }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const t = usePortalMessages(namespace);
  const [targets, setTargets] = useState<DeploymentTarget[]>([]);
  const [revisions, setRevisions] = useState<ServiceRevision[]>([]);
  const [selectedID, setSelectedID] = useState<number>();
  const [value, setValue] = useState<EditorValue>({ units: [] });
  const [creating, setCreating] = useState(true);
  const [valid, setValid] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [audit, setAudit] = useState<ServiceAuditEvent[]>([]);
  const [auditOpen, setAuditOpen] = useState(false);
  const [previewOpen, setPreviewOpen] = useState(false);
  const selected = revisions.find((item) => item.id === selectedID);
  const schema = useMemo(() => serviceCompositionSchema(targets), [targets]);

  const select = useCallback((revision: ServiceRevision) => { setSelectedID(revision.id); setCreating(false); setValue(editorValue(revision)); }, []);
  const load = useCallback(async (preferID?: number) => {
    setLoading(true);
    try {
      const [nextTargets, nextRevisions] = await Promise.all([client.listDeploymentTargets(), client.listServiceRevisions()]);
      setTargets(nextTargets); setRevisions(nextRevisions);
      const preferred = nextRevisions.find((item) => item.id === (preferID ?? selectedID)) ?? nextRevisions[0];
      if (!creating && preferred !== undefined) select(preferred);
      if (creating && typeof value.deployment !== "string" && nextTargets[0] !== undefined) setValue((current) => ({ ...current, deployment: nextTargets[0]?.deploymentName }));
      setError(undefined);
    } catch (cause) { setError(errorMessage(cause,t("error.request","服务编排请求失败"))); } finally { setLoading(false); }
  }, [client, creating, select, selectedID, value.deployment]);
  useEffect(() => { void load(); }, [client]);

  const mutate = async (title: string, action: () => Promise<ServiceRevision>, confirmation?: string) => {
    if (confirmation !== undefined && !await ui.confirm({ title, content: confirmation })) return;
    setBusy(true);
    try { const changed = await action(); setCreating(false); setSelectedID(changed.id); await load(changed.id); ui.notify({ title, kind: "success" }); }
    catch (cause) { const text = errorMessage(cause,t("error.request","服务编排请求失败")); setError(text); ui.notify({ title: t("notice.failed","{title}失败",{title}), content: text, kind: "error" }); }
    finally { setBusy(false); }
  };
  const startNew = () => { setCreating(true); setSelectedID(undefined); setValue({ deployment: targets[0]?.deploymentName, units: [] }); };
  const save = () => mutate(creating ? t("notice.created","草稿已创建") : t("notice.saved","草稿已保存"), () => {
    const composition = buildBackendComposition(value, selected?.composition.revision ?? 1);
    return creating || selected === undefined ? client.createServiceDraft(composition) : client.updateServiceDraft(selected.id, composition);
  });
  const openAudit = async () => { if (selected === undefined) return; setAudit(await client.listServiceRevisionAudit(selected.id)); setAuditOpen(true); };

  return <ui.Stack gap="md"><ui.Stack direction="row" gap="sm" justify="end">
    <ui.Button kind="secondary" onClick={() => void load()} loading={loading}>{t("action.refresh","刷新")}</ui.Button><ui.Button kind="primary" onClick={startNew}>{t("action.new","新建服务草稿")}</ui.Button>
  </ui.Stack>
    {error === undefined ? null : <ui.ErrorState title={error} retry={() => void load()} />}
    {targets.length === 0 && !loading ? <ui.EmptyState title={t("empty.targets","没有平台预授权的部署目标")} description={t("empty.targetsDescription","请先由平台运维发布 Backend Platform Catalog 绑定。")} /> : null}
    <ui.Grid columns={{ xs: 1, lg: 2 }} gap="lg">
      <ui.GridItem><ui.Panel title={t("panel.revisions","服务组合 Revisions")}><ui.Table loading={loading} rowKey="id" rows={revisions as unknown as Array<Record<string, unknown>>} empty={<ui.EmptyState title={t("empty.revisions","尚无服务组合")} />} columns={[
        { key: "id", title: "Revision", render: (_cell, row) => <ui.Button kind="text" onClick={() => select(row as unknown as ServiceRevision)}>#{String(row.id)}</ui.Button> },
        { key: "deployment", title: t("column.deployment","部署") },
        { key: "status", title: t("column.status","状态"), render: (cell, row) => <ui.Status tone={tones[String(cell) as ServiceRevisionStatus]}>{String(cell)}{row.active === true ? t("status.activeSuffix"," · 活动") : ""}</ui.Status> },
        { key: "updatedAt", title: t("column.updated","更新时间"), render: (cell) => formatTime(cell,i18n.formatDate) },
      ]} /></ui.Panel></ui.GridItem>
      <ui.GridItem><ui.Panel title={creating ? t("panel.new","新建服务草稿") : `Revision #${selected?.id ?? "-"}`}>
        {selected === undefined ? null : <><ui.Descriptions columns={2} items={[
          { id: "status", label: t("column.status","状态"), value: <ui.Status tone={tones[selected.status]}>{selected.status}{selected.active ? t("status.activeSuffix"," · 活动") : ""}</ui.Status> },
          { id: "deployment", label: t("column.deployment","部署"), value: selected.deployment }, { id: "kv", label: t("field.kv","控制面 KV revision"), value: selected.kvRevision ?? "-" },
          { id: "profile", label: t("field.profile","平台基线"), value: targets.find((target) => target.deploymentName === selected.deployment)?.platformProfile.id ?? "-" },
          { id: "submitted", label: t("field.submitted","提交人"), value: selected.submittedBy ?? "-" }, { id: "approved", label: t("field.approved","审批人"), value: selected.approvedBy ?? "-" },
        ]} /><ui.Divider /></>}
        <ui.FormRenderer schema={schema} value={value} onChange={setValue} readOnly={!creating && selected?.status !== "Draft"} submitting={busy} onValidationChange={(result) => setValid(result.valid)} />
        <ui.Stack direction="row" gap="sm" wrap>
          {creating || selected?.status === "Draft" ? <ui.Button kind="primary" disabled={!valid || targets.length === 0} loading={busy} onClick={() => void save()}>{creating ? t("action.create","创建草稿") : t("action.save","保存草稿")}</ui.Button> : null}
          {selected?.status === "Draft" ? <ui.Button onClick={() => void mutate(t("notice.submitted","已提交审批"), () => client.submitServiceDraft(selected.id), t("confirm.submit","提交后将重新执行可信解析，且不能继续编辑。"))}>{t("action.submit","提交审批")}</ui.Button> : null}
          {selected?.status === "PendingApproval" ? <ui.Button kind="primary" onClick={() => void mutate(t("notice.approved","已批准"), () => client.approveServiceRevision(selected.id), t("confirm.approve","审批人与提交人必须不同。"))}>{t("action.approve","批准")}</ui.Button> : null}
          {selected?.status === "Approved" || selected?.status === "Publishing" ? <ui.Button kind="primary" onClick={() => void mutate(t("notice.published","已发布"), () => client.publishServiceRevision(selected.id), t("confirm.publish","发布后 Controller 会把副本调度到符合条件的 Node Agent。"))}>{t("action.publish","发布")}</ui.Button> : null}
          {selected?.status === "Published" && !selected.active ? <ui.Button kind="danger" onClick={() => void mutate(t("notice.rolledBack","已回滚"), () => client.rollbackServiceRevision(selected.id), t("confirm.rollback","回滚会用历史应用组合创建并发布一个新的单调 revision。"))}>{t("action.rollback","回滚到此版本")}</ui.Button> : null}
          {selected === undefined ? null : <ui.Button kind="secondary" onClick={() => setPreviewOpen(true)}>{t("action.preview","最终部署预览")}</ui.Button>}
          {selected === undefined ? null : <ui.Button kind="secondary" onClick={() => void openAudit()}>{t("action.audit","审计记录")}</ui.Button>}
        </ui.Stack>
      </ui.Panel></ui.GridItem>
    </ui.Grid>
    <ui.Dialog open={previewOpen} title={t("dialog.preview","内核解析后的 Deployment v2")} width="lg" onClose={() => setPreviewOpen(false)}><pre style={{ overflow: "auto", maxHeight: 560 }}>{JSON.stringify(selected?.preview ?? {}, null, 2)}</pre></ui.Dialog>
    <ui.Drawer open={auditOpen} title={t("dialog.audit","服务组合审计")} width="lg" onClose={() => setAuditOpen(false)}><ui.Table rowKey="id" rows={audit as unknown as Array<Record<string, unknown>>} empty={<ui.EmptyState title={t("empty.audit","尚无审计记录")} />} columns={[
      { key: "at", title: t("column.time","时间"), render: (cell) => formatTime(cell,i18n.formatDate) }, { key: "action", title: t("column.action","动作") }, { key: "actorId", title: t("column.actor","操作者") },
    ]} /></ui.Drawer>
  </ui.Stack>;
}

function pluginRefs(value: unknown): BackendPluginRef[] { return Array.isArray(value) ? value.flatMap((item) => typeof item === "object" && item !== null && typeof (item as Record<string, unknown>).id === "string" && typeof (item as Record<string, unknown>).version === "string" ? [{ id: String((item as Record<string, unknown>).id), version: String((item as Record<string, unknown>).version), ...(text((item as Record<string, unknown>).channel) === undefined ? {} : { channel: text((item as Record<string, unknown>).channel) }) }] : []) : []; }
function text(value: unknown): string | undefined { return typeof value === "string" && value !== "" ? value : undefined; }
function strings(value: unknown): string[] { return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string" && item !== "") : []; }
function record(value: unknown): Record<string, unknown> | undefined { return typeof value === "object" && value !== null && !Array.isArray(value) && Object.keys(value).length > 0 ? JSON.parse(JSON.stringify(value)) as Record<string, unknown> : undefined; }
function formatTime(value: unknown, format: (value:string)=>string): string { if (typeof value !== "string") return "-"; const parsed = new Date(value); return Number.isNaN(parsed.valueOf()) ? value : format(value); }
function errorMessage(cause: unknown, fallback: string): string { return cause instanceof Error ? cause.message : fallback; }

export default {
  register(context: FrontendPluginContext) {
    const services = managementServicesFor(context.portal, "platform.deployment");
    if (services.length === 0) throw new Error("Portal 未绑定 platform.deployment 服务");
    for (const service of services) {
      const client = createBrowserPlatformAdminClient(context.portal.id, service.id);
      const suffix = services.length === 1 ? "" : `/${service.id}`;
      const label = context.i18n.message(services.length === 1 ? "page.title" : "page.titleService", services.length === 1 ? "服务与节点部署" : "服务与节点部署 · {service}", { service: service.label ?? service.id });
      context.addPage({ id: `platform.deployment.${service.id}`, path: `/settings/deployment${suffix}`, title: label, description: context.i18n.message("page.description","在线组合应用服务、实例与调度并发布到 Node Agent 集群"), navigation: { id: `platform.deployment.${service.id}`, label, zone: "settings", order: 60 }, slots: [{ id: "body", slot: "page.body.main", component: () => <DeploymentManagerView client={client} /> }] });
    }
  },
  localization:{defaultLocale:"zh-CN",messages:{
    "zh-CN":{"form.deployment":"部署目标","form.units":"应用服务","form.serviceClass":"服务分类","form.serviceId":"服务 ID","form.logicalService":"逻辑服务名","form.plugins":"应用插件","form.pluginId":"插件 ID","form.version":"精确版本","form.channel":"通道","form.stable":"稳定版","form.preview":"预发布","form.replicas":"实例数","form.instancePolicy":"实例策略","form.activeActive":"多活","form.leader":"单主","form.partitioned":"分区","form.stateModel":"状态模型","form.externalShared":"外部共享状态","form.leaderOwned":"主节点状态","form.partitionOwned":"分区状态","form.localEphemeral":"本地临时状态","form.routing":"路由","form.queue":"队列","form.leaderRoute":"主节点","form.partitionRoute":"分区","form.direct":"直连","form.routingDomain":"路由域","form.partitionKeys":"分区键","form.dependsOn":"依赖服务 ID","form.nodeSelector":"节点标签选择","form.config":"非敏感配置","help.deployment":"目标及 Platform Profile 由平台运维预授权，不能在此修改。","help.units":"这里只能组合应用插件；内核会注入基础插件并校验制品来源、依赖 DAG、实例策略和节点调度。","help.config":"密码、令牌等敏感值必须使用凭证引用，不能写入配置。","action.refresh":"刷新","action.new":"新建服务草稿","empty.targets":"没有平台预授权的部署目标","empty.targetsDescription":"请先由平台运维发布 Backend Platform Catalog 绑定。","panel.revisions":"服务组合 Revisions","empty.revisions":"尚无服务组合","column.deployment":"部署","column.status":"状态","status.activeSuffix":" · 活动","column.updated":"更新时间","panel.new":"新建服务草稿","action.create":"创建草稿","action.save":"保存草稿","field.kv":"控制面 KV revision","field.profile":"平台基线","field.submitted":"提交人","field.approved":"审批人","notice.created":"草稿已创建","notice.saved":"草稿已保存","notice.submitted":"已提交审批","confirm.submit":"提交后将重新执行可信解析，且不能继续编辑。","action.submit":"提交审批","notice.approved":"已批准","confirm.approve":"审批人与提交人必须不同。","action.approve":"批准","notice.published":"已发布","confirm.publish":"发布后 Controller 会把副本调度到符合条件的 Node Agent。","action.publish":"发布","notice.rolledBack":"已回滚","confirm.rollback":"回滚会用历史应用组合创建并发布一个新的单调 revision。","action.rollback":"回滚到此版本","notice.failed":"{title}失败","error.request":"服务编排请求失败","action.preview":"最终部署预览","action.audit":"审计记录","dialog.preview":"内核解析后的 Deployment v2","dialog.audit":"服务组合审计","empty.audit":"尚无审计记录","column.time":"时间","column.action":"动作","column.actor":"操作者","page.title":"服务与节点部署","page.titleService":"服务与节点部署 · {service}","page.description":"在线组合应用服务、实例与调度并发布到 Node Agent 集群"},
    "en-US":{"form.deployment":"Deployment target","form.units":"Application services","form.serviceClass":"Service class","form.serviceId":"Service ID","form.logicalService":"Logical service","form.plugins":"Application plugins","form.pluginId":"Plugin ID","form.version":"Exact version","form.channel":"Channel","form.stable":"Stable","form.preview":"Preview","form.replicas":"Replicas","form.instancePolicy":"Instance policy","form.activeActive":"Active-active","form.leader":"Leader","form.partitioned":"Partitioned","form.stateModel":"State model","form.externalShared":"External shared state","form.leaderOwned":"Leader-owned state","form.partitionOwned":"Partition-owned state","form.localEphemeral":"Local ephemeral state","form.routing":"Routing","form.queue":"Queue","form.leaderRoute":"Leader","form.partitionRoute":"Partition","form.direct":"Direct","form.routingDomain":"Routing domain","form.partitionKeys":"Partition keys","form.dependsOn":"Service dependencies","form.nodeSelector":"Node selector","form.config":"Non-sensitive config","help.deployment":"The target and Platform Profile are pre-authorized by platform operations and cannot be changed here.","help.units":"Only application plugins are composed here; the kernel injects foundation plugins and validates provenance, dependency DAG, instance policy, and scheduling.","help.config":"Sensitive values must use credential references and cannot be written to configuration.","action.refresh":"Refresh","action.new":"New service draft","empty.targets":"No pre-authorized deployment targets","empty.targetsDescription":"Platform operations must publish a Backend Platform Catalog binding first.","panel.revisions":"Service composition revisions","empty.revisions":"No service compositions","column.deployment":"Deployment","column.status":"Status","status.activeSuffix":" · Active","column.updated":"Updated","panel.new":"New service draft","action.create":"Create draft","action.save":"Save draft","field.kv":"Control-plane KV revision","field.profile":"Platform baseline","field.submitted":"Submitted by","field.approved":"Approved by","notice.created":"Draft created","notice.saved":"Draft saved","notice.submitted":"Submitted for approval","confirm.submit":"Trusted resolution runs again after submission and the draft can no longer be edited.","action.submit":"Submit","notice.approved":"Approved","confirm.approve":"The approver must be different from the submitter.","action.approve":"Approve","notice.published":"Published","confirm.publish":"After publishing, the Controller schedules replicas onto eligible Node Agents.","action.publish":"Publish","notice.rolledBack":"Rolled back","confirm.rollback":"Rollback creates and publishes a new monotonic revision from the historical application composition.","action.rollback":"Rollback to this version","notice.failed":"{title} failed","error.request":"Service composition request failed","action.preview":"Final deployment preview","action.audit":"Audit log","dialog.preview":"Kernel-resolved Deployment v2","dialog.audit":"Service composition audit","empty.audit":"No audit records","column.time":"Time","column.action":"Action","column.actor":"Actor","page.title":"Services and node deployment","page.titleService":"Services and node deployment · {service}","page.description":"Compose application services, replicas, and scheduling, then publish to the Node Agent cluster"}
  }},
};
