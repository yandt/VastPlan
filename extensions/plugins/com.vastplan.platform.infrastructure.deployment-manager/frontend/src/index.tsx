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
import { jsonSchemaDialect, managementServicesFor, usePortalUI, type FormSchema, type FrontendPluginContext, type StatusTone } from "@vastplan/portal-ui";

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
    } catch (cause) { setError(message(cause)); } finally { setLoading(false); }
  }, [client, creating, select, selectedID, value.deployment]);
  useEffect(() => { void load(); }, [client]);

  const mutate = async (title: string, action: () => Promise<ServiceRevision>, confirmation?: string) => {
    if (confirmation !== undefined && !await ui.confirm({ title, content: confirmation })) return;
    setBusy(true);
    try { const changed = await action(); setCreating(false); setSelectedID(changed.id); await load(changed.id); ui.notify({ title, kind: "success" }); }
    catch (cause) { const text = message(cause); setError(text); ui.notify({ title: `${title}失败`, content: text, kind: "error" }); }
    finally { setBusy(false); }
  };
  const startNew = () => { setCreating(true); setSelectedID(undefined); setValue({ deployment: targets[0]?.deploymentName, units: [] }); };
  const save = () => mutate(creating ? "草稿已创建" : "草稿已保存", () => {
    const composition = buildBackendComposition(value, selected?.composition.revision ?? 1);
    return creating || selected === undefined ? client.createServiceDraft(composition) : client.updateServiceDraft(selected.id, composition);
  });
  const openAudit = async () => { if (selected === undefined) return; setAudit(await client.listServiceRevisionAudit(selected.id)); setAuditOpen(true); };

  return <ui.Stack gap="md"><ui.Stack direction="row" gap="sm" justify="end">
    <ui.Button kind="secondary" onClick={() => void load()} loading={loading}>刷新</ui.Button><ui.Button kind="primary" onClick={startNew}>新建服务草稿</ui.Button>
  </ui.Stack>
    {error === undefined ? null : <ui.ErrorState title={error} retry={() => void load()} />}
    {targets.length === 0 && !loading ? <ui.EmptyState title="没有平台预授权的部署目标" description="请先由平台运维发布 Backend Platform Catalog 绑定。" /> : null}
    <ui.Grid columns={{ xs: 1, lg: 2 }} gap="lg">
      <ui.GridItem><ui.Panel title="服务组合 Revisions"><ui.Table loading={loading} rowKey="id" rows={revisions as unknown as Array<Record<string, unknown>>} empty={<ui.EmptyState title="尚无服务组合" />} columns={[
        { key: "id", title: "Revision", render: (_cell, row) => <ui.Button kind="text" onClick={() => select(row as unknown as ServiceRevision)}>#{String(row.id)}</ui.Button> },
        { key: "deployment", title: "部署" },
        { key: "status", title: "状态", render: (cell, row) => <ui.Status tone={tones[String(cell) as ServiceRevisionStatus]}>{String(cell)}{row.active === true ? " · Active" : ""}</ui.Status> },
        { key: "updatedAt", title: "更新时间", render: formatTime },
      ]} /></ui.Panel></ui.GridItem>
      <ui.GridItem><ui.Panel title={creating ? "新建服务草稿" : `Revision #${selected?.id ?? "-"}`}>
        {selected === undefined ? null : <><ui.Descriptions columns={2} items={[
          { id: "status", label: "状态", value: <ui.Status tone={tones[selected.status]}>{selected.status}{selected.active ? " · Active" : ""}</ui.Status> },
          { id: "deployment", label: "部署", value: selected.deployment }, { id: "kv", label: "控制面 KV revision", value: selected.kvRevision ?? "-" },
          { id: "profile", label: "平台基线", value: targets.find((target) => target.deploymentName === selected.deployment)?.platformProfile.id ?? "-" },
          { id: "submitted", label: "提交人", value: selected.submittedBy ?? "-" }, { id: "approved", label: "审批人", value: selected.approvedBy ?? "-" },
        ]} /><ui.Divider /></>}
        <ui.FormRenderer schema={schema} value={value} onChange={setValue} readOnly={!creating && selected?.status !== "Draft"} submitting={busy} onValidationChange={(result) => setValid(result.valid)} />
        <ui.Stack direction="row" gap="sm" wrap>
          {creating || selected?.status === "Draft" ? <ui.Button kind="primary" disabled={!valid || targets.length === 0} loading={busy} onClick={() => void save()}>{creating ? "创建草稿" : "保存草稿"}</ui.Button> : null}
          {selected?.status === "Draft" ? <ui.Button onClick={() => void mutate("已提交审批", () => client.submitServiceDraft(selected.id), "提交后将重新执行可信解析，且不能继续编辑。")}>提交审批</ui.Button> : null}
          {selected?.status === "PendingApproval" ? <ui.Button kind="primary" onClick={() => void mutate("已批准", () => client.approveServiceRevision(selected.id), "审批人与提交人必须不同。")}>批准</ui.Button> : null}
          {selected?.status === "Approved" || selected?.status === "Publishing" ? <ui.Button kind="primary" onClick={() => void mutate("已发布", () => client.publishServiceRevision(selected.id), "发布后 Controller 会把副本调度到符合条件的 Node Agent。")}>发布</ui.Button> : null}
          {selected?.status === "Published" && !selected.active ? <ui.Button kind="danger" onClick={() => void mutate("已回滚", () => client.rollbackServiceRevision(selected.id), "回滚会用历史应用组合创建并发布一个新的单调 revision。")}>回滚到此版本</ui.Button> : null}
          {selected === undefined ? null : <ui.Button kind="secondary" onClick={() => setPreviewOpen(true)}>最终部署预览</ui.Button>}
          {selected === undefined ? null : <ui.Button kind="secondary" onClick={() => void openAudit()}>审计记录</ui.Button>}
        </ui.Stack>
      </ui.Panel></ui.GridItem>
    </ui.Grid>
    <ui.Dialog open={previewOpen} title="内核解析后的 Deployment v2" width="lg" onClose={() => setPreviewOpen(false)}><pre style={{ overflow: "auto", maxHeight: 560 }}>{JSON.stringify(selected?.preview ?? {}, null, 2)}</pre></ui.Dialog>
    <ui.Drawer open={auditOpen} title="服务组合审计" width="lg" onClose={() => setAuditOpen(false)}><ui.Table rowKey="id" rows={audit as unknown as Array<Record<string, unknown>>} empty={<ui.EmptyState title="尚无审计记录" />} columns={[
      { key: "at", title: "时间", render: formatTime }, { key: "action", title: "动作" }, { key: "actorId", title: "操作者" },
    ]} /></ui.Drawer>
  </ui.Stack>;
}

function pluginRefs(value: unknown): BackendPluginRef[] { return Array.isArray(value) ? value.flatMap((item) => typeof item === "object" && item !== null && typeof (item as Record<string, unknown>).id === "string" && typeof (item as Record<string, unknown>).version === "string" ? [{ id: String((item as Record<string, unknown>).id), version: String((item as Record<string, unknown>).version), ...(text((item as Record<string, unknown>).channel) === undefined ? {} : { channel: text((item as Record<string, unknown>).channel) }) }] : []) : []; }
function text(value: unknown): string | undefined { return typeof value === "string" && value !== "" ? value : undefined; }
function strings(value: unknown): string[] { return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string" && item !== "") : []; }
function record(value: unknown): Record<string, unknown> | undefined { return typeof value === "object" && value !== null && !Array.isArray(value) && Object.keys(value).length > 0 ? JSON.parse(JSON.stringify(value)) as Record<string, unknown> : undefined; }
function formatTime(value: unknown): string { if (typeof value !== "string") return "-"; const parsed = new Date(value); return Number.isNaN(parsed.valueOf()) ? value : parsed.toLocaleString(); }
function message(cause: unknown): string { return cause instanceof Error ? cause.message : "服务编排请求失败"; }

export default {
  register(context: FrontendPluginContext) {
    const services = managementServicesFor(context.portal, "platform.deployment");
    if (services.length === 0) throw new Error("Portal 未绑定 platform.deployment 服务");
    for (const service of services) {
      const client = createBrowserPlatformAdminClient(context.portal.id, service.id);
      const suffix = services.length === 1 ? "" : `/${service.id}`;
      const label = services.length === 1 ? "服务与节点部署" : `服务与节点部署 · ${service.label ?? service.id}`;
      context.addPage({ id: `platform.deployment.${service.id}`, path: `/settings/deployment${suffix}`, title: label, description: "在线组合应用服务、实例与调度并发布到 Node Agent 集群", navigation: { id: `platform.deployment.${service.id}`, label, zone: "settings", order: 60 }, slots: [{ id: "body", slot: "page.body.main", component: () => <DeploymentManagerView client={client} /> }] });
    }
  },
};
