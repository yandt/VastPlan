import { useCallback, useEffect, useMemo, useState } from "react";
import type { FormSchema, FrontendPluginContext, JSONValue, PortalApplicationComposition, PortalAuditEvent, PortalFetch, PortalRevision, StatusTone } from "@vastplan/portal-ui";
import { jsonSchemaDialect, PortalControlClient, PortalControlError, usePortalUI } from "@vastplan/portal-ui";

type EditorValue = Record<string, unknown>;

export type ApplicationComposition = PortalApplicationComposition;

export const portalCompositionSchema: FormSchema = {
  id: "portal-composition.v1",
  schema: {
    $schema: jsonSchemaDialect,
    title: "Portal Application Composition",
    type: "object",
    additionalProperties: false,
    required: ["name", "route", "plugins"],
    properties: {
      name: { type: "string", title: "名称", minLength: 1 },
      route: { type: "string", title: "访问路径", pattern: "^/" },
      domains: { type: "array", title: "绑定域名", uniqueItems: true, items: { type: "string", minLength: 1 } },
      audience: { type: "array", title: "目标受众", uniqueItems: true, items: { type: "string", minLength: 1 } },
      branding: { type: "object", title: "品牌配置", additionalProperties: true, default: {} },
      plugins: {
        type: "array",
        title: "应用功能插件",
        minItems: 1,
        items: {
          type: "object",
          additionalProperties: false,
          required: ["id", "version"],
          properties: {
            id: { type: "string", title: "插件 ID", pattern: "^[a-z0-9]+(?:[.-][a-z0-9]+)+$" },
            version: { type: "string", title: "精确版本", pattern: "^\\d+\\.\\d+\\.\\d+(?:[-+][0-9A-Za-z.-]+)?$" },
            channel: {
              type: "string",
              title: "发布通道",
              default: "stable",
              oneOf: [
                { const: "stable", title: "稳定版" },
                { const: "preview", title: "预发布" },
              ],
            },
          },
        },
      },
      config: { type: "object", title: "非敏感插件配置", additionalProperties: true, default: {} },
    },
  },
  uiSchema: {
    route: { "ui:help": "必须以 / 开始；同一租户内不能与其他已发布 Portal 冲突" },
    domains: { "ui:help": "留空表示不限制域名" },
    audience: { "ui:help": "只声明可见受众，不替代服务端授权" },
    branding: { "ui:help": "只能保存非敏感 JSON 品牌配置" },
    plugins: {
      "ui:help": "这里只能选择应用插件；设计系统和平台插件由 Platform Profile 管理",
      items: { channel: { "ui:widget": "select" } },
    },
    config: { "ui:help": "禁止写入密码、令牌或凭证明文" },
  },
};

export function buildApplicationComposition(value: EditorValue, revision = 1): ApplicationComposition {
  return {
    version: 1,
    revision,
    id: typeof value.name === "string" && value.name !== "" ? value.name : "portal",
    target: { kernel: "frontend" },
    route: typeof value.route === "string" ? value.route : "/",
    ...optionalStrings("domains", value.domains),
    ...optionalStrings("audience", value.audience),
    ...optionalRecord("branding", value.branding),
    plugins: normalizePluginRefs(value.plugins),
    config: jsonRecord(value.config),
  };
}

export function compositionToEditorValue(composition: ApplicationComposition): EditorValue {
  return {
    name: composition.id,
    route: composition.route,
    domains: composition.domains ?? [],
    audience: composition.audience ?? [],
    branding: composition.branding ?? {},
    plugins: composition.plugins.map((ref) => ({ ...ref })),
    config: composition.config,
  };
}

function optionalStrings<K extends "domains" | "audience">(key: K, value: unknown): Partial<Pick<ApplicationComposition, K>> {
  const strings = Array.isArray(value) ? value.filter((item): item is string => typeof item === "string" && item !== "") : [];
  return strings.length === 0 ? {} : { [key]: strings } as Partial<Pick<ApplicationComposition, K>>;
}

function optionalRecord<K extends "branding">(key: K, value: unknown): Partial<Pick<ApplicationComposition, K>> {
  const record = jsonRecord(value);
  return Object.keys(record).length === 0 ? {} : { [key]: record } as Partial<Pick<ApplicationComposition, K>>;
}

function jsonRecord(value: unknown): Record<string, JSONValue> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return {};
  return JSON.parse(JSON.stringify(value)) as Record<string, JSONValue>;
}

function normalizePluginRefs(value: unknown): ApplicationComposition["plugins"] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((candidate) => {
    if (typeof candidate !== "object" || candidate === null) return [];
    const { id, version, channel } = candidate as Record<string, unknown>;
    if (typeof id !== "string" || typeof version !== "string") return [];
    return [{ id, version, ...(typeof channel === "string" ? { channel } : {}) }];
  });
}

function createDefaultClient(): PortalControlClient {
  const fetcher: PortalFetch = (input, init) => globalThis.fetch(input, init as RequestInit);
  return new PortalControlClient({ fetch: fetcher });
}

const statusTone: Record<PortalRevision["status"], StatusTone> = {
  Draft: "neutral",
  PendingApproval: "warning",
  Approved: "info",
  Published: "success",
};

const errorMessages: Record<string, string> = {
  forbidden: "当前账号没有执行此操作的权限，或提交人与审批人相同。",
  transition_rejected: "当前 revision 状态不允许此操作，或制品/路由校验失败。",
  csrf_rejected: "安全令牌已失效，请重试。",
  session_required: "登录会话已失效。",
  network_unavailable: "无法连接 Portal Edge。",
};

export function PortalComposerView({ client: suppliedClient }: { client?: PortalControlClient } = {}) {
  const ui = usePortalUI();
  const client = useMemo(() => suppliedClient ?? createDefaultClient(), [suppliedClient]);
  const [revisions, setRevisions] = useState<PortalRevision[]>([]);
  const [selectedID, setSelectedID] = useState<number>();
  const [value, setValue] = useState<EditorValue>({ route: "/", domains: [], audience: [], branding: {}, plugins: [], config: {} });
  const [valid, setValid] = useState(false);
  const [creating, setCreating] = useState(true);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [audit, setAudit] = useState<PortalAuditEvent[]>([]);
  const [auditOpen, setAuditOpen] = useState(false);
  const [diffOpen, setDiffOpen] = useState(false);

  const selected = revisions.find((revision) => revision.id === selectedID);
  const active = selected === undefined ? undefined : revisions.find((revision) => revision.portalId === selected.portalId && revision.active);

  const selectRevision = useCallback((revision: PortalRevision) => {
    setSelectedID(revision.id);
    setCreating(false);
    setValue(compositionToEditorValue(revision.composition));
    setError(undefined);
  }, []);

  const load = useCallback(async (preferID?: number) => {
    setLoading(true);
    try {
      const next = await client.list();
      setRevisions(next);
      const preferred = next.find((revision) => revision.id === (preferID ?? selectedID)) ?? next[0];
      if (preferred !== undefined && !creating) selectRevision(preferred);
      setError(undefined);
    } catch (cause) {
      setError(controlErrorMessage(cause));
    } finally {
      setLoading(false);
    }
  }, [client, creating, selectRevision, selectedID]);

  useEffect(() => { void load(); }, [client]);

  const startNew = () => {
    setCreating(true);
    setSelectedID(undefined);
    setValue({ route: "/", domains: [], audience: [], branding: {}, plugins: [], config: {} });
    setError(undefined);
  };

  const mutate = async (title: string, action: () => Promise<PortalRevision>, confirmContent?: string) => {
    if (confirmContent !== undefined && !await ui.confirm({ title, content: confirmContent })) return;
    setBusy(true);
    try {
      const changed = await action();
      setCreating(false);
      setSelectedID(changed.id);
      await load(changed.id);
      ui.notify({ title, kind: "success" });
    } catch (cause) {
      const message = controlErrorMessage(cause);
      setError(message);
      ui.notify({ title: `${title}失败`, content: message, kind: "error" });
    } finally {
      setBusy(false);
    }
  };

  const save = () => mutate(creating ? "草稿已创建" : "草稿已保存", () => {
    const composition = buildApplicationComposition(value, selected?.composition.revision ?? 1);
    return creating || selected === undefined ? client.create(composition) : client.update(selected.id, composition);
  });

  const openAudit = async () => {
    if (selected === undefined) return;
    setBusy(true);
    try {
      setAudit(await client.audit(selected.id));
      setAuditOpen(true);
    } catch (cause) {
      setError(controlErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  };

  const rows = revisions.map((revision) => ({ ...revision, key: String(revision.id) }));
  const editorReadOnly = !creating && selected?.status !== "Draft";

  return <ui.Stack gap="md"><ui.Stack direction="row" gap="sm" justify="end">
    <ui.Button kind="secondary" onClick={() => void load()} loading={loading}>刷新</ui.Button>
    <ui.Button kind="primary" onClick={startNew}>新建草稿</ui.Button>
  </ui.Stack>
    {error === undefined ? null : <ui.ErrorState title={error} retry={() => void load()} />}
    <ui.Grid columns={{ xs: 1, lg: 2 }} gap="lg">
      <ui.GridItem><ui.Panel title="Revisions">
        <ui.Table
          loading={loading}
          rowKey="key"
          rows={rows}
          empty={<ui.EmptyState title="尚无 Portal revision" />}
          columns={[
            { key: "id", title: "Revision", width: 100, render: (_cell, row) => <ui.Button kind="text" onClick={() => selectRevision(row as unknown as PortalRevision)}>#{String(row.id)}</ui.Button> },
            { key: "portalId", title: "Portal" },
            { key: "status", title: "状态", render: (cell, row) => <ui.Status tone={statusTone[String(cell) as PortalRevision["status"]]}>{String(cell)}{row.active === true ? " · Active" : ""}</ui.Status> },
            { key: "updatedAt", title: "更新时间", render: (cell) => formatTime(cell) },
          ]}
        />
      </ui.Panel></ui.GridItem>
      <ui.GridItem><ui.Panel title={creating ? "新建草稿" : `Revision #${selected?.id ?? "-"}`}>
        {!creating && selected !== undefined ? <>
          <ui.Descriptions columns={2} items={[
            { id: "status", label: "状态", value: <ui.Status tone={statusTone[selected.status]}>{selected.status}{selected.active ? " · Active" : ""}</ui.Status> },
            { id: "portal", label: "Portal", value: selected.portalId },
            { id: "submitted", label: "提交人", value: selected.submittedBy ?? "-" },
            { id: "approved", label: "审批人", value: selected.approvedBy ?? "-" },
            { id: "published", label: "发布人", value: selected.publishedBy ?? "-" },
            { id: "updated", label: "更新时间", value: formatTime(selected.updatedAt) },
          ]} />
          <ui.Divider />
        </> : null}
        <ui.FormRenderer
          schema={portalCompositionSchema}
          value={value}
          onChange={setValue}
          readOnly={editorReadOnly}
          submitting={busy}
          onValidationChange={(result) => setValid(result.valid)}
        />
        <ui.Stack direction="row" gap="sm" wrap>
          {creating || selected?.status === "Draft" ? <ui.Button kind="primary" onClick={() => void save()} loading={busy} disabled={!valid}>{creating ? "创建草稿" : "保存草稿"}</ui.Button> : null}
          {selected?.status === "Draft" ? <ui.Button onClick={() => void mutate("已提交审批", () => client.submit(selected.id), "提交后不能继续编辑，需要由另一位审批人处理。")}>提交审批</ui.Button> : null}
          {selected?.status === "PendingApproval" ? <ui.Button kind="primary" onClick={() => void mutate("已批准", () => client.approve(selected.id), "确认该 revision 的插件、路由和配置可以发布？")}>批准</ui.Button> : null}
          {selected?.status === "Approved" ? <ui.Button kind="primary" onClick={() => void mutate("已发布", () => client.publish(selected.id), "发布会把该 revision 设为当前活动版本。")}>发布</ui.Button> : null}
          {selected?.status === "Published" && !selected.active ? <ui.Button kind="danger" onClick={() => void mutate("已回滚", () => client.rollback(selected.id), "系统将基于当前平台基线重新解析该历史版本并创建新的活动 revision。")}>回滚到此版本</ui.Button> : null}
          {selected === undefined ? null : <ui.Button kind="secondary" onClick={() => setDiffOpen(true)}>查看差异</ui.Button>}
          {selected === undefined ? null : <ui.Button kind="secondary" onClick={() => void openAudit()}>审计记录</ui.Button>}
        </ui.Stack>
      </ui.Panel></ui.GridItem>
    </ui.Grid>
    <ui.Dialog open={diffOpen} title={`Revision #${selected?.id ?? "-"} 差异`} width="lg" onClose={() => setDiffOpen(false)}>
      <ui.Grid columns={2} gap="md">
        <ui.GridItem><DiffDocument title={active === undefined || active.id === selected?.id ? "当前活动版本" : `Active #${active.id}`} value={active?.composition} /></ui.GridItem>
        <ui.GridItem><DiffDocument title={selected === undefined ? "所选版本" : `Selected #${selected.id}`} value={selected?.composition} /></ui.GridItem>
      </ui.Grid>
    </ui.Dialog>
    <ui.Drawer open={auditOpen} title={`Revision #${selected?.id ?? "-"} 审计`} width="lg" onClose={() => setAuditOpen(false)}>
      <ui.Table rowKey="id" rows={audit as unknown as Array<Record<string, unknown>>} empty={<ui.EmptyState title="尚无审计记录" />} columns={[
        { key: "at", title: "时间", render: formatTime },
        { key: "action", title: "动作" },
        { key: "actorId", title: "操作者" },
        { key: "priority", title: "级别", render: (cell) => <ui.Status tone={cell === "high" ? "error" : "neutral"}>{String(cell)}</ui.Status> },
        { key: "reason", title: "原因", render: (cell) => typeof cell === "string" && cell !== "" ? cell : "-" },
      ]} />
    </ui.Drawer>
  </ui.Stack>;
}

function DiffDocument({ title, value }: { title: string; value: unknown }) {
  return <section><h3>{title}</h3><pre style={{ overflow: "auto", maxHeight: 480, padding: 12, background: "var(--color-fill-1, #f7f8fa)" }}>{value === undefined ? "无" : JSON.stringify(value, null, 2)}</pre></section>;
}

function controlErrorMessage(error: unknown): string {
  if (error instanceof PortalControlError) return errorMessages[error.code] ?? `请求被拒绝（${error.code}）`;
  return error instanceof Error ? error.message : "未知错误";
}

function formatTime(value: unknown): string {
  if (typeof value !== "string" || value === "") return "-";
  const parsed = new Date(value);
  return Number.isNaN(parsed.valueOf()) ? value : parsed.toLocaleString();
}

function PortalGovernanceBadge() {
  const ui = usePortalUI();
  return <ui.Status tone="info">平台治理</ui.Status>;
}

export default {
  register(context: FrontendPluginContext) {
    context.addPage({ id: "platform.portal-composer", path: "/settings/portals", title: "门户与插件组合", description: "治理 Portal 草稿、审批、发布与回滚", navigation: { id: "platform.portal-composer", label: "门户组合", zone: "settings", order: 10 }, slots: [{ id: "governance", slot: "page.header.end", component: PortalGovernanceBadge, order: 10 }, { id: "body", slot: "page.body.main", component: PortalComposerView }] });
  },
};
