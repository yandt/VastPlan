import { useCallback, useEffect, useMemo, useState } from "react";
import type { FormSchema, FrontendPluginContext, JSONValue, PortalApplicationComposition, PortalAuditEvent, PortalFetch, PortalRevision, StatusTone } from "@vastplan/ui-primitives";
import { jsonSchemaDialect, message, PortalControlClient, PortalControlError, usePortalI18n, usePortalMessages, usePortalUI } from "@vastplan/ui-primitives";
import { governanceMessages, GovernanceWorkspaces } from "./governance-workspaces";

const namespace = "cn.vastplan.platform.configuration.portal-composer";

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
  localization: {
    "/properties/name/title":message(namespace,"form.name","名称"), "/properties/route/title":message(namespace,"form.route","访问路径"), "/properties/domains/title":message(namespace,"form.domains","绑定域名"), "/properties/audience/title":message(namespace,"form.audience","目标受众"), "/properties/branding/title":message(namespace,"form.branding","品牌配置"), "/properties/plugins/title":message(namespace,"form.plugins","应用功能插件"), "/properties/plugins/items/properties/id/title":message(namespace,"form.pluginId","插件 ID"), "/properties/plugins/items/properties/version/title":message(namespace,"form.version","精确版本"), "/properties/plugins/items/properties/channel/title":message(namespace,"form.channel","发布通道"), "/properties/plugins/items/properties/channel/oneOf/0/title":message(namespace,"form.stable","稳定版"), "/properties/plugins/items/properties/channel/oneOf/1/title":message(namespace,"form.preview","预发布"), "/properties/config/title":message(namespace,"form.config","非敏感插件配置")
  },
  uiLocalization: { "/route/ui:help":message(namespace,"help.route","必须以 / 开始；同一租户内不能与其他已发布 Portal 冲突"), "/domains/ui:help":message(namespace,"help.domains","留空表示不限制域名"), "/audience/ui:help":message(namespace,"help.audience","只声明可见受众，不替代服务端授权"), "/branding/ui:help":message(namespace,"help.branding","只能保存非敏感 JSON 品牌配置"), "/plugins/ui:help":message(namespace,"help.plugins","这里只能选择应用插件；设计系统和平台插件由 Platform Profile 管理"), "/config/ui:help":message(namespace,"help.config","禁止写入密码、令牌或凭证明文") },
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
  const client = useMemo(() => suppliedClient ?? createDefaultClient(), [suppliedClient]);
  return <GovernanceWorkspaces client={client} applications={<ApplicationWorkspace client={client} />} />;
}

function ApplicationWorkspace({ client: suppliedClient }: { client: PortalControlClient }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const t = usePortalMessages(namespace);
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
  const publishedBaseline = selected === undefined ? undefined : revisions.find((revision) => revision.portalId === selected.portalId && revision.status === "Published" && revision.id !== selected.id);

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
      setError(controlErrorMessage(cause, t));
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
      const failure = controlErrorMessage(cause, t);
      setError(failure);
      ui.notify({ title: t("notice.failed","{title}失败",{title}), content: failure, kind: "error" });
    } finally {
      setBusy(false);
    }
  };

  const save = () => mutate(creating ? t("notice.created","草稿已创建") : t("notice.saved","草稿已保存"), () => {
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
      setError(controlErrorMessage(cause, t));
    } finally {
      setBusy(false);
    }
  };

  const rows = revisions.map((revision) => ({ ...revision, key: String(revision.id) }));
  const editorReadOnly = !creating && selected?.status !== "Draft";

  return <ui.Stack gap="md"><ui.Stack direction="row" gap="sm" justify="end">
    <ui.Button kind="secondary" onClick={() => void load()} loading={loading}>{t("action.refresh","刷新")}</ui.Button>
    <ui.Button kind="primary" onClick={startNew}>{t("action.new","新建草稿")}</ui.Button>
  </ui.Stack>
    {error === undefined ? null : <ui.ErrorState title={error} retry={() => void load()} />}
    <ui.Grid columns={{ xs: 1, lg: 2 }} gap="lg">
      <ui.GridItem><ui.Panel title={t("panel.revisions","Revisions")}>
        <ui.Table
          loading={loading}
          rowKey="key"
          rows={rows}
          empty={<ui.EmptyState title={t("empty.revisions","尚无 Portal revision")} />}
          columns={[
            { key: "id", title: "Revision", width: 100, render: (_cell, row) => <ui.Button kind="text" onClick={() => selectRevision(row as unknown as PortalRevision)}>#{String(row.id)}</ui.Button> },
            { key: "portalId", title: "Portal" },
            { key: "status", title: t("column.status","状态"), render: (cell) => <ui.Status tone={statusTone[String(cell) as PortalRevision["status"]]}>{String(cell)}</ui.Status> },
            { key: "updatedAt", title: t("column.updated","更新时间"), render: (cell) => formatTime(cell,i18n.formatDate) },
          ]}
        />
      </ui.Panel></ui.GridItem>
      <ui.GridItem><ui.Panel title={creating ? t("panel.new","新建草稿") : `Revision #${selected?.id ?? "-"}`}>
        {!creating && selected !== undefined ? <>
          <ui.Descriptions columns={2} items={[
            { id: "status", label: t("column.status","状态"), value: <ui.Status tone={statusTone[selected.status]}>{selected.status}</ui.Status> },
            { id: "portal", label: "Portal", value: selected.portalId },
            { id: "submitted", label: t("field.submitted","提交人"), value: selected.submittedBy ?? "-" },
            { id: "approved", label: t("field.approved","审批人"), value: selected.approvedBy ?? "-" },
            { id: "published", label: t("field.published","发布人"), value: selected.publishedBy ?? "-" },
            { id: "updated", label: t("column.updated","更新时间"), value: formatTime(selected.updatedAt,i18n.formatDate) },
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
          {creating || selected?.status === "Draft" ? <ui.Button kind="primary" onClick={() => void save()} loading={busy} disabled={!valid}>{creating ? t("action.create","创建草稿") : t("action.save","保存草稿")}</ui.Button> : null}
          {selected?.status === "Draft" ? <ui.Button onClick={() => void mutate(t("notice.submitted","已提交审批"), () => client.submit(selected.id), t("confirm.submit","提交后不能继续编辑，需要由另一位审批人处理。"))}>{t("action.submit","提交审批")}</ui.Button> : null}
          {selected?.status === "PendingApproval" ? <ui.Button kind="primary" onClick={() => void mutate(t("notice.approved","已批准"), () => client.approve(selected.id), t("confirm.approve","确认该 revision 的插件、路由和配置可以发布？"))}>{t("action.approve","批准")}</ui.Button> : null}
          {selected?.status === "Approved" ? <ui.Button kind="primary" onClick={() => void mutate(t("notice.inputPublished","已发布为可选输入"), () => client.publish(selected.id), t("confirm.inputPublish","发布仅使该 Application 可被 Activation 引用，不会直接改变线上 Portal。"))}>{t("action.publishInput","发布为可选输入")}</ui.Button> : null}
          {selected === undefined ? null : <ui.Button kind="secondary" onClick={() => setDiffOpen(true)}>{t("action.diff","查看差异")}</ui.Button>}
          {selected === undefined ? null : <ui.Button kind="secondary" onClick={() => void openAudit()}>{t("action.audit","审计记录")}</ui.Button>}
        </ui.Stack>
      </ui.Panel></ui.GridItem>
    </ui.Grid>
    <ui.Dialog open={diffOpen} title={t("dialog.diff","Revision #{id} 差异",{id:selected?.id ?? "-"})} width="lg" onClose={() => setDiffOpen(false)}>
      <ui.Grid columns={2} gap="md">
        <ui.GridItem><DiffDocument title={publishedBaseline === undefined ? t("diff.active","无其他已发布输入") : t("diff.activeRevision","已发布输入 #{id}",{id:publishedBaseline.id})} empty={t("diff.empty","无")} value={publishedBaseline?.composition} /></ui.GridItem>
        <ui.GridItem><DiffDocument title={selected === undefined ? t("diff.selected","所选版本") : t("diff.selectedRevision","所选版本 #{id}",{id:selected.id})} empty={t("diff.empty","无")} value={selected?.composition} /></ui.GridItem>
      </ui.Grid>
    </ui.Dialog>
    <ui.Drawer open={auditOpen} title={t("dialog.audit","Revision #{id} 审计",{id:selected?.id ?? "-"})} width="lg" onClose={() => setAuditOpen(false)}>
      <ui.Table rowKey="id" rows={audit as unknown as Array<Record<string, unknown>>} empty={<ui.EmptyState title={t("empty.audit","尚无审计记录")} />} columns={[
        { key: "at", title: t("column.time","时间"), render: (cell) => formatTime(cell,i18n.formatDate) },
        { key: "action", title: t("column.action","动作") },
        { key: "actorId", title: t("column.actor","操作者") },
        { key: "priority", title: t("column.priority","级别"), render: (cell) => <ui.Status tone={cell === "high" ? "error" : "neutral"}>{String(cell)}</ui.Status> },
        { key: "reason", title: t("column.reason","原因"), render: (cell) => typeof cell === "string" && cell !== "" ? cell : "-" },
      ]} />
    </ui.Drawer>
  </ui.Stack>;
}

function DiffDocument({ title, empty, value }: { title: string; empty: string; value: unknown }) {
  return <section><h3>{title}</h3><pre style={{ overflow: "auto", maxHeight: 480, padding: 12, background: "var(--color-fill-1, #f7f8fa)" }}>{value === undefined ? empty : JSON.stringify(value, null, 2)}</pre></section>;
}

function controlErrorMessage(error: unknown, t: (key:string,fallback:string,values?:Record<string,string|number>)=>string): string {
  if (error instanceof PortalControlError) return t(`error.${error.code}`, errorMessages[error.code] ?? "请求被拒绝（{code}）",{code:error.code});
  return error instanceof Error ? error.message : t("error.unknown","未知错误");
}

function formatTime(value: unknown, format: (value:string)=>string): string {
  if (typeof value !== "string" || value === "") return "-";
  const parsed = new Date(value);
  return Number.isNaN(parsed.valueOf()) ? value : format(value);
}

function PortalGovernanceBadge() {
  const ui = usePortalUI();
  const t = usePortalMessages(namespace);
  return <ui.Status tone="info">{t("badge.governance","平台治理")}</ui.Status>;
}

const portalComposerPlugin = {
  register(context: FrontendPluginContext) {
    context.addPage({ id: "platform.portal-composer", path: "/settings/portals", title: context.i18n.message("page.title.v2","Portal 管理中心"), description: context.i18n.message("page.description.v2","治理 Platform Profiles、Portals 与不可变 Activation"), navigation: { id: "platform.portal-composer", label: context.i18n.message("page.navigation.v2","Portal 管理"), zone: "settings", order: 10 }, slots: [{ id: "governance", slot: "page.header.end", component: PortalGovernanceBadge, order: 10 }, { id: "body", slot: "page.body.main", component: PortalComposerView }] });
  },
  localization:{defaultLocale:"zh-CN",messages:{
    "zh-CN":{"form.name":"名称","form.route":"访问路径","form.domains":"绑定域名","form.audience":"目标受众","form.branding":"品牌配置","form.plugins":"应用功能插件","form.pluginId":"插件 ID","form.version":"精确版本","form.channel":"发布通道","form.stable":"稳定版","form.preview":"预发布","form.config":"非敏感插件配置","help.route":"必须以 / 开始；同一租户内不能与其他已发布 Portal 冲突","help.domains":"留空表示不限制域名","help.audience":"只声明可见受众，不替代服务端授权","help.branding":"只能保存非敏感 JSON 品牌配置","help.plugins":"这里只能选择应用插件；设计系统和平台插件由 Platform Profile 管理","help.config":"禁止写入密码、令牌或凭证明文","notice.failed":"{title}失败","notice.created":"草稿已创建","notice.saved":"草稿已保存","action.refresh":"刷新","action.new":"新建草稿","panel.revisions":"Revisions","empty.revisions":"尚无 Portal revision","column.status":"状态","status.activeSuffix":" · 活动","column.updated":"更新时间","panel.new":"新建草稿","field.submitted":"提交人","field.approved":"审批人","field.published":"发布人","action.create":"创建草稿","action.save":"保存草稿","notice.submitted":"已提交审批","confirm.submit":"提交后不能继续编辑，需要由另一位审批人处理。","action.submit":"提交审批","notice.approved":"已批准","confirm.approve":"确认该 revision 的插件、路由和配置可以发布？","action.approve":"批准","notice.published":"已发布","confirm.publish":"发布会把该 revision 设为当前活动版本。","action.publish":"发布","notice.rolledBack":"已回滚","confirm.rollback":"系统将基于当前平台基线重新解析该历史版本并创建新的活动 revision。","action.rollback":"回滚到此版本","action.diff":"查看差异","action.audit":"审计记录","dialog.diff":"Revision #{id} 差异","diff.active":"当前活动版本","diff.activeRevision":"活动版本 #{id}","diff.selected":"所选版本","diff.selectedRevision":"所选版本 #{id}","diff.empty":"无","dialog.audit":"Revision #{id} 审计","empty.audit":"尚无审计记录","column.time":"时间","column.action":"动作","column.actor":"操作者","column.priority":"级别","column.reason":"原因","error.forbidden":"当前账号没有执行此操作的权限，或提交人与审批人相同。","error.transition_rejected":"当前 revision 状态不允许此操作，或制品/路由校验失败。","error.csrf_rejected":"安全令牌已失效，请重试。","error.session_required":"登录会话已失效。","error.network_unavailable":"无法连接 Portal Edge。","error.rejected":"请求被拒绝（{code}）","error.unknown":"未知错误","badge.governance":"平台治理","page.title":"门户与插件组合","page.description":"治理 Portal 草稿、审批、发布与回滚","page.navigation":"门户组合"},
    "en-US":{"form.name":"Name","form.route":"Route","form.domains":"Domains","form.audience":"Audience","form.branding":"Branding","form.plugins":"Application plugins","form.pluginId":"Plugin ID","form.version":"Exact version","form.channel":"Release channel","form.stable":"Stable","form.preview":"Preview","form.config":"Non-sensitive plugin config","help.route":"Must start with /. It must not conflict with another published Portal in the tenant.","help.domains":"Leave empty to allow any domain","help.audience":"Declares visibility only; it does not replace server authorization","help.branding":"Only non-sensitive JSON branding configuration is allowed","help.plugins":"Only application plugins can be selected here; Platform Profile manages design-system and platform plugins","help.config":"Passwords, tokens, and credential plaintext are forbidden","notice.failed":"{title} failed","notice.created":"Draft created","notice.saved":"Draft saved","action.refresh":"Refresh","action.new":"New draft","panel.revisions":"Revisions","empty.revisions":"No Portal revisions","column.status":"Status","status.activeSuffix":" · Active","column.updated":"Updated","panel.new":"New draft","field.submitted":"Submitted by","field.approved":"Approved by","field.published":"Published by","action.create":"Create draft","action.save":"Save draft","notice.submitted":"Submitted for approval","confirm.submit":"Editing is disabled after submission; another approver must process it.","action.submit":"Submit","notice.approved":"Approved","confirm.approve":"Confirm that the revision plugins, routes, and configuration can be published?","action.approve":"Approve","notice.published":"Published","confirm.publish":"Publishing makes this revision active.","action.publish":"Publish","notice.rolledBack":"Rolled back","confirm.rollback":"The historical version will be resolved against the current platform baseline and published as a new revision.","action.rollback":"Rollback to this version","action.diff":"View diff","action.audit":"Audit log","dialog.diff":"Revision #{id} diff","diff.active":"Current active revision","diff.activeRevision":"Active revision #{id}","diff.selected":"Selected revision","diff.selectedRevision":"Selected revision #{id}","diff.empty":"None","dialog.audit":"Revision #{id} audit","empty.audit":"No audit records","column.time":"Time","column.action":"Action","column.actor":"Actor","column.priority":"Priority","column.reason":"Reason","error.forbidden":"This account is not authorized, or the submitter and approver are the same person.","error.transition_rejected":"The revision state does not allow this operation, or artifact/route validation failed.","error.csrf_rejected":"The security token expired. Please retry.","error.session_required":"The login session expired.","error.network_unavailable":"Portal Edge is unavailable.","error.rejected":"Request rejected ({code})","error.unknown":"Unknown error","badge.governance":"Platform governance","page.title":"Portal and plugin composition","page.description":"Govern Portal drafts, approval, publishing, and rollback","page.navigation":"Portal composition"}
  }},
};

portalComposerPlugin.localization.messages["zh-CN"] = { ...portalComposerPlugin.localization.messages["zh-CN"], ...governanceMessages["zh-CN"] };
portalComposerPlugin.localization.messages["en-US"] = { ...portalComposerPlugin.localization.messages["en-US"], ...governanceMessages["en-US"] };

export default portalComposerPlugin;
