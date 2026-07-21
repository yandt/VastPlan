import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import {
  jsonSchemaDialect,
  message,
  PortalControlError,
  usePortalI18n,
  usePortalMessages,
  usePortalUI,
  type FormSchema,
  type JSONValue,
  type PortalActivation,
  type PortalBindingRevision,
  type PortalControlClient,
  type PortalGovernanceSnapshot,
  type PortalManagementBinding,
  type PortalPlatformProfile,
  type PortalProfileRevision,
  type PortalRevisionStatus,
  type StatusTone,
} from "@vastplan/ui-primitives";

const namespace = "cn.vastplan.platform.configuration.portal-composer";

export const governanceMessages = {
  "zh-CN": {
    "governance.workspace.profiles": "Platform Profiles", "governance.workspace.portals": "Portals",
    "governance.tab.activations": "Activations", "governance.tab.applications": "Applications", "governance.tab.bindings": "Bindings",
    "governance.action.newFromSelected": "基于所选 {resource} 新建版本", "governance.action.createDraft": "创建草稿", "governance.action.saveDraft": "保存草稿",
    "governance.action.submit": "提交审批", "governance.action.approve": "批准", "governance.action.publishInput": "发布为可选输入", "governance.action.activate": "校验并激活", "governance.action.rollback": "回滚到此版本",
    "governance.panel.profileRevisions": "Profile revisions", "governance.panel.bindingRevisions": "Binding revisions", "governance.panel.currentActivation": "当前线上 Activation", "governance.panel.newActivation": "创建 Activation", "governance.panel.activationHistory": "不可变 Activation 历史",
    "governance.empty.profiles": "尚无 Platform Profile", "governance.empty.profileBase": "请选择一个 Profile 作为版本基线", "governance.empty.bindings": "尚无 Portal Binding", "governance.empty.bindingBase": "请选择一个 Binding 作为版本基线", "governance.empty.activation": "尚无 Activation", "governance.empty.current": "尚未激活 Portal",
    "governance.column.revision": "Revision", "governance.column.profile": "Profile", "governance.column.portal": "Portal", "governance.column.status": "状态", "governance.column.activation": "Activation", "governance.column.time": "时间", "governance.column.action": "操作", "governance.column.inputs": "输入", "governance.column.references": "制品引用",
    "governance.status.referencePending": "引用同步中", "governance.status.referenceSynced": "已保护",
    "governance.notice.profilePublished": "Profile 已发布，尚未影响任何 Portal；必须创建 Activation 才会生效。", "governance.notice.bindingPublished": "Binding 已发布，只有被 Activation 引用后才影响线上 Portal。",
    "governance.notice.saved": "治理资源已保存", "governance.notice.transitioned": "治理状态已更新", "governance.notice.activated": "Portal 已激活", "governance.notice.rolledBack": "Portal 已回滚", "governance.notice.failed": "治理操作失败",
    "governance.error.load": "治理数据加载失败", "governance.error.activationFailed": "Activation 校验失败：{message}",
    "notice.inputPublished": "Application 已发布为可选输入", "confirm.inputPublish": "发布仅使该 Application 可被 Activation 引用，不会直接改变线上 Portal。", "action.publishInput": "发布为可选输入", "diff.active": "无其他已发布输入", "diff.activeRevision": "已发布输入 #{id}",
    "governance.form.profileId": "Profile ID", "governance.form.renderer": "默认 UI 框架", "governance.form.allowedRenderers": "允许的 UI 框架", "governance.form.userRenderer": "允许用户切换 UI 框架", "governance.form.layout": "布局插件", "governance.form.bodyWidth": "页面正文宽度", "governance.form.navigationGroups": "导航分组", "governance.form.portalId": "Portal ID", "governance.form.publishedProfile": "已发布 Profile", "governance.form.services": "管理服务绑定", "governance.form.publishedApplication": "已发布 Application", "governance.form.publishedBinding": "已发布 Binding", "governance.form.expectedActivation": "期望当前 Activation", "governance.form.reason": "变更说明",
    "governance.option.arco": "Arco Design", "governance.option.mui": "Material UI", "governance.option.standardLayout": "标准侧栏布局", "governance.option.topLayout": "顶部导航布局", "governance.option.fluid": "自适应", "governance.option.contained": "最大 1280px",
    "page.title.v2": "Portal 管理中心", "page.description.v2": "治理 Platform Profiles、Portals 与不可变 Activation", "page.navigation.v2": "Portal 管理",
  },
  "en-US": {
    "governance.workspace.profiles": "Platform Profiles", "governance.workspace.portals": "Portals",
    "governance.tab.activations": "Activations", "governance.tab.applications": "Applications", "governance.tab.bindings": "Bindings",
    "governance.action.newFromSelected": "Create a version from selected {resource}", "governance.action.createDraft": "Create draft", "governance.action.saveDraft": "Save draft",
    "governance.action.submit": "Submit", "governance.action.approve": "Approve", "governance.action.publishInput": "Publish as eligible input", "governance.action.activate": "Validate and activate", "governance.action.rollback": "Rollback to this version",
    "governance.panel.profileRevisions": "Profile revisions", "governance.panel.bindingRevisions": "Binding revisions", "governance.panel.currentActivation": "Current Activations", "governance.panel.newActivation": "Create Activation", "governance.panel.activationHistory": "Immutable Activation history",
    "governance.empty.profiles": "No Platform Profiles", "governance.empty.profileBase": "Select a Profile as the version baseline", "governance.empty.bindings": "No Portal Bindings", "governance.empty.bindingBase": "Select a Binding as the version baseline", "governance.empty.activation": "No Activations", "governance.empty.current": "No active Portal",
    "governance.column.revision": "Revision", "governance.column.profile": "Profile", "governance.column.portal": "Portal", "governance.column.status": "Status", "governance.column.activation": "Activation", "governance.column.time": "Time", "governance.column.action": "Action", "governance.column.inputs": "Inputs", "governance.column.references": "Artifact references",
    "governance.status.referencePending": "Syncing references", "governance.status.referenceSynced": "Protected",
    "governance.notice.profilePublished": "The Profile is eligible but does not affect a Portal until an Activation references it.", "governance.notice.bindingPublished": "The Binding is eligible but affects the live Portal only through an Activation.",
    "governance.notice.saved": "Governance resource saved", "governance.notice.transitioned": "Governance status updated", "governance.notice.activated": "Portal activated", "governance.notice.rolledBack": "Portal rolled back", "governance.notice.failed": "Governance operation failed",
    "governance.error.load": "Failed to load governance data", "governance.error.activationFailed": "Activation validation failed: {message}",
    "notice.inputPublished": "Application published as an eligible input", "confirm.inputPublish": "Publishing only makes this Application eligible for Activation and does not change the live Portal.", "action.publishInput": "Publish as eligible input", "diff.active": "No other published input", "diff.activeRevision": "Published input #{id}",
    "governance.form.profileId": "Profile ID", "governance.form.renderer": "Default UI framework", "governance.form.allowedRenderers": "Allowed UI frameworks", "governance.form.userRenderer": "Allow users to switch UI framework", "governance.form.layout": "Layout plugin", "governance.form.bodyWidth": "Page body width", "governance.form.navigationGroups": "Navigation groups", "governance.form.portalId": "Portal ID", "governance.form.publishedProfile": "Published Profile", "governance.form.services": "Managed service bindings", "governance.form.publishedApplication": "Published Application", "governance.form.publishedBinding": "Published Binding", "governance.form.expectedActivation": "Expected current Activation", "governance.form.reason": "Change reason",
    "governance.option.arco": "Arco Design", "governance.option.mui": "Material UI", "governance.option.standardLayout": "Standard sidebar layout", "governance.option.topLayout": "Top navigation layout", "governance.option.fluid": "Fluid", "governance.option.contained": "Maximum 1280px",
    "page.title.v2": "Portal Management", "page.description.v2": "Govern Platform Profiles, Portals, and immutable Activations", "page.navigation.v2": "Portal Management",
  },
} as const;

type Translator = (key: string, fallback: string, values?: Record<string, string | number>) => string;

const tones: Record<PortalRevisionStatus, StatusTone> = { Draft: "neutral", PendingApproval: "warning", Approved: "info", Published: "success" };
const activationTones: Record<PortalActivation["status"], StatusTone> = { Preparing: "info", Activating: "warning", Current: "success", Superseded: "neutral", Failed: "error" };

export function GovernanceWorkspaces({ client, applications }: { client: PortalControlClient; applications: ReactNode }) {
  const ui = usePortalUI();
  const t = usePortalMessages(namespace);
  const [workspace, setWorkspace] = useState("portals");
  const [snapshot, setSnapshot] = useState<PortalGovernanceSnapshot>({ profiles: [], applications: [], bindings: [], activations: [] });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string>();
  const refresh = useCallback(async () => {
    setLoading(true);
    try { setSnapshot(await client.governance()); setError(undefined); }
    catch (cause) { setError(controlError(cause, t, t("governance.error.load", "治理数据加载失败"))); }
    finally { setLoading(false); }
  }, [client]);
  useEffect(() => { void refresh(); }, [refresh]);
  return <ui.Stack gap="md">
    {error === undefined ? null : <ui.ErrorState title={error} retry={() => void refresh()} />}
    <ui.Tabs activeID={workspace} onChange={setWorkspace} items={[
      { id: "profiles", label: t("governance.workspace.profiles", "Platform Profiles"), content: <ProfileWorkspace client={client} snapshot={snapshot} refresh={refresh} loading={loading} /> },
      { id: "portals", label: t("governance.workspace.portals", "Portals"), content: <PortalWorkspace client={client} snapshot={snapshot} refresh={refresh} loading={loading} applications={applications} /> },
    ]} />
  </ui.Stack>;
}

type ProfileValue = { name: string; defaultRenderer: "arco" | "mui"; allowedRenderers: Array<"arco" | "mui">; userSelectableRenderer: boolean; defaultTemplate: "standard" | "top-navigation"; pageBodyWidth: "fluid" | "contained"; navigationGroups: unknown[] };

function ProfileWorkspace({ client, snapshot, refresh, loading }: WorkspaceProps) {
  const ui = usePortalUI();
  const t = usePortalMessages(namespace);
  const [selectedID, setSelectedID] = useState<number>();
  const [draftBase, setDraftBase] = useState<PortalProfileRevision>();
  const [value, setValue] = useState<Record<string, unknown>>({});
  const [valid, setValid] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const selected = snapshot.profiles.find((item) => item.id === selectedID) ?? snapshot.profiles[0];
  useEffect(() => {
    if (selected === undefined || draftBase?.id === selected.id) return;
    setSelectedID(selected.id); setDraftBase(selected); setValue(profileValue(selected.profile));
  }, [draftBase?.id, selected]);
  const mutate = async (title: string, action: () => Promise<unknown>) => {
    setBusy(true);
    try { await action(); await refresh(); setError(undefined); ui.notify({ title, kind: "success" }); }
    catch (cause) { const detail = controlError(cause, t); setError(detail); ui.notify({ title: t("governance.notice.failed", "治理操作失败"), content: detail, kind: "error" }); }
    finally { setBusy(false); }
  };
  const startVersion = () => { if (selected === undefined) return; setDraftBase(selected); setSelectedID(undefined); setValue(profileValue(selected.profile)); };
  const save = () => {
    if (draftBase === undefined) return;
    const profile = buildProfile(draftBase.profile, value, selectedID === undefined);
    void mutate(t("governance.notice.saved", "治理资源已保存"), () => selectedID === undefined ? client.createProfile(profile) : client.updateProfile(selectedID, profile));
  };
  const transition = (action: "submit" | "approve" | "publish") => selected === undefined ? undefined : void mutate(t("governance.notice.transitioned", "治理状态已更新"), () => client.transitionProfile(selected.id, action));
  return <ui.Grid columns={{ xs: 1, lg: 2 }} gap="lg">
    <ui.GridItem><ui.Panel title={t("governance.panel.profileRevisions", "Profile revisions")}><ui.Stack gap="sm">
      <ui.Button kind="primary" onClick={startVersion} disabled={selected === undefined}>{t("governance.action.newFromSelected", "基于所选 {resource} 新建版本", { resource: "Profile" })}</ui.Button>
      <ui.Table loading={loading} rowKey="id" rows={snapshot.profiles as unknown as Array<Record<string, unknown>>} empty={<ui.EmptyState title={t("governance.empty.profiles", "尚无 Platform Profile")} />} columns={[
        { key: "id", title: t("governance.column.revision", "Revision"), render: (_value, row) => <ui.Button kind="text" onClick={() => { const item = row as unknown as PortalProfileRevision; setSelectedID(item.id); setDraftBase(item); setValue(profileValue(item.profile)); }}>#{String(row.id)}</ui.Button> },
        { key: "profile", title: t("governance.column.profile", "Profile"), render: (entry) => (entry as PortalPlatformProfile).id },
        { key: "status", title: t("governance.column.status", "状态"), render: (entry) => <ui.Status tone={tones[entry as PortalRevisionStatus]}>{String(entry)}</ui.Status> },
      ]} />
    </ui.Stack></ui.Panel></ui.GridItem>
    <ui.GridItem><ui.Panel title={selectedID === undefined ? "新 Profile revision" : `Profile revision #${selected?.id ?? "-"}`}>
      {error === undefined ? null : <ui.ErrorState title={error} />}
      {draftBase === undefined ? <ui.EmptyState title={t("governance.empty.profileBase", "请选择一个 Profile 作为版本基线")} /> : <ui.Stack gap="md">
        <ui.FormRenderer schema={profileSchema} value={value} onChange={setValue} readOnly={selectedID !== undefined && selected?.status !== "Draft"} submitting={busy} onValidationChange={(result) => setValid(result.valid)} />
        <ui.Stack direction="row" gap="sm" wrap>
          {selectedID === undefined || selected?.status === "Draft" ? <ui.Button kind="primary" disabled={!valid} loading={busy} onClick={save}>{selectedID === undefined ? t("governance.action.createDraft", "创建草稿") : t("governance.action.saveDraft", "保存草稿")}</ui.Button> : null}
          {selected?.status === "Draft" ? <ui.Button onClick={() => transition("submit")}>{t("governance.action.submit", "提交审批")}</ui.Button> : null}
          {selected?.status === "PendingApproval" ? <ui.Button kind="primary" onClick={() => transition("approve")}>{t("governance.action.approve", "批准")}</ui.Button> : null}
          {selected?.status === "Approved" ? <ui.Button kind="primary" onClick={() => transition("publish")}>{t("governance.action.publishInput", "发布为可选输入")}</ui.Button> : null}
        </ui.Stack>
        {selected?.status === "Published" ? <ui.Status tone="info">{t("governance.notice.profilePublished", "已发布，尚未影响任何 Portal；必须创建 Activation 才会生效。")}</ui.Status> : null}
      </ui.Stack>}
    </ui.Panel></ui.GridItem>
  </ui.Grid>;
}

function PortalWorkspace({ client, snapshot, refresh, loading, applications }: WorkspaceProps & { applications: ReactNode }) {
  const ui = usePortalUI();
  const t = usePortalMessages(namespace);
  const [tab, setTab] = useState("activations");
  return <ui.Tabs activeID={tab} onChange={setTab} items={[
    { id: "activations", label: t("governance.tab.activations", "Activations"), content: <ActivationWorkspace client={client} snapshot={snapshot} refresh={refresh} loading={loading} /> },
    { id: "applications", label: t("governance.tab.applications", "Applications"), content: applications },
    { id: "bindings", label: t("governance.tab.bindings", "Bindings"), content: <BindingWorkspace client={client} snapshot={snapshot} refresh={refresh} loading={loading} /> },
  ]} />;
}

function BindingWorkspace({ client, snapshot, refresh, loading }: WorkspaceProps) {
  const ui = usePortalUI();
  const t = usePortalMessages(namespace);
  const [selectedID, setSelectedID] = useState<number>();
  const [base, setBase] = useState<PortalBindingRevision>();
  const [value, setValue] = useState<Record<string, unknown>>({});
  const [valid, setValid] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const selected = snapshot.bindings.find((item) => item.id === selectedID) ?? snapshot.bindings[0];
  const schema = useMemo(() => bindingSchema(snapshot.profiles), [snapshot.profiles]);
  useEffect(() => { if (selected === undefined || base?.id === selected.id) return; setSelectedID(selected.id); setBase(selected); setValue(bindingValue(selected)); }, [base?.id, selected]);
  const mutate = async (title: string, action: () => Promise<unknown>) => {
    setBusy(true);
    try { await action(); await refresh(); setError(undefined); ui.notify({ title, kind: "success" }); }
    catch (cause) { const detail = controlError(cause, t); setError(detail); ui.notify({ title: t("governance.notice.failed", "治理操作失败"), content: detail, kind: "error" }); }
    finally { setBusy(false); }
  };
  const save = () => {
    if (base === undefined) return;
    const profileRevisionId = Number(value.profileRevisionId);
    const binding = buildBinding(base.binding, value);
    void mutate(t("governance.notice.saved", "治理资源已保存"), () => selectedID === undefined ? client.createBinding(profileRevisionId, binding) : client.updateBinding(selectedID, profileRevisionId, binding));
  };
  const transition = (action: "submit" | "approve" | "publish") => selected === undefined ? undefined : void mutate(t("governance.notice.transitioned", "治理状态已更新"), () => client.transitionBinding(selected.id, action));
  return <ui.Grid columns={{ xs: 1, lg: 2 }} gap="lg">
    <ui.GridItem><ui.Panel title={t("governance.panel.bindingRevisions", "Binding revisions")}><ui.Stack gap="sm"><ui.Button kind="primary" disabled={selected === undefined} onClick={() => { if (selected === undefined) return; setBase(selected); setSelectedID(undefined); setValue(bindingValue(selected)); }}>{t("governance.action.newFromSelected", "基于所选 {resource} 新建版本", { resource: "Binding" })}</ui.Button><ui.Table loading={loading} rowKey="id" rows={snapshot.bindings as unknown as Array<Record<string, unknown>>} empty={<ui.EmptyState title={t("governance.empty.bindings", "尚无 Portal Binding")} />} columns={[
      { key: "id", title: t("governance.column.revision", "Revision"), render: (_entry, row) => <ui.Button kind="text" onClick={() => { const item = row as unknown as PortalBindingRevision; setSelectedID(item.id); setBase(item); setValue(bindingValue(item)); }}>#{String(row.id)}</ui.Button> },
      { key: "portalId", title: t("governance.column.portal", "Portal") }, { key: "status", title: t("governance.column.status", "状态"), render: (entry) => <ui.Status tone={tones[entry as PortalRevisionStatus]}>{String(entry)}</ui.Status> },
    ]} /></ui.Stack></ui.Panel></ui.GridItem>
    <ui.GridItem><ui.Panel title={selectedID === undefined ? "新 Binding revision" : `Binding revision #${selected?.id ?? "-"}`}>
      {error === undefined ? null : <ui.ErrorState title={error} />}
      {base === undefined ? <ui.EmptyState title={t("governance.empty.bindingBase", "请选择一个 Binding 作为版本基线")} /> : <ui.Stack gap="md"><ui.FormRenderer schema={schema} value={value} onChange={setValue} readOnly={selectedID !== undefined && selected?.status !== "Draft"} submitting={busy} onValidationChange={(result) => setValid(result.valid)} /><ui.Stack direction="row" gap="sm" wrap>
        {selectedID === undefined || selected?.status === "Draft" ? <ui.Button kind="primary" disabled={!valid} onClick={save}>{t("governance.action.saveDraft", "保存草稿")}</ui.Button> : null}
        {selected?.status === "Draft" ? <ui.Button onClick={() => transition("submit")}>{t("governance.action.submit", "提交审批")}</ui.Button> : null}{selected?.status === "PendingApproval" ? <ui.Button kind="primary" onClick={() => transition("approve")}>{t("governance.action.approve", "批准")}</ui.Button> : null}{selected?.status === "Approved" ? <ui.Button kind="primary" onClick={() => transition("publish")}>{t("governance.action.publishInput", "发布为可选输入")}</ui.Button> : null}
      </ui.Stack>{selected?.status === "Published" ? <ui.Status tone="info">{t("governance.notice.bindingPublished", "已发布，只有被 Activation 引用后才影响线上 Portal。")}</ui.Status> : null}</ui.Stack>}
    </ui.Panel></ui.GridItem>
  </ui.Grid>;
}

function ActivationWorkspace({ client, snapshot, refresh, loading }: WorkspaceProps) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const t = usePortalMessages(namespace);
  const [value, setValue] = useState<Record<string, unknown>>({});
  const [valid, setValid] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const schema = useMemo(() => activationSchema(snapshot), [snapshot]);
  const current = snapshot.activations.filter((item) => item.status === "Current");
  useEffect(() => {
    if (value.portalId !== undefined || snapshot.bindings.length === 0) return;
    setValue(activationDefaults(snapshot));
  }, [snapshot, value.portalId]);
  const activate = async () => {
    setBusy(true);
    try {
      const result = await client.activate({ portalId: String(value.portalId ?? ""), applicationRevisionId: Number(value.applicationRevisionId), profileRevisionId: Number(value.profileRevisionId), bindingRevisionId: Number(value.bindingRevisionId), expectedCurrentId: Number(value.expectedCurrentId ?? 0), reason: typeof value.reason === "string" ? value.reason : undefined });
      await refresh();
      if (result.status === "Failed") {
        const message = result.phases.find((phase) => phase.status === "Failed")?.message ?? "unknown";
        throw new Error(t("governance.error.activationFailed", "Activation 校验失败：{message}", { message }));
      }
      setError(undefined);
      ui.notify({ title: t("governance.notice.activated", "Portal 已激活"), kind: "success" });
    } catch (cause) {
      const detail = controlError(cause, t);
      setError(detail);
      ui.notify({ title: t("governance.notice.failed", "治理操作失败"), content: detail, kind: "error" });
    } finally { setBusy(false); }
  };
  const rollback = async (source: PortalActivation) => {
    const live = current.find((item) => item.portalId === source.portalId);
    if (live === undefined) return;
    setBusy(true);
    try {
      const result = await client.rollbackActivation(source.id, live.id, "管理员从 Portal 工作区发起回滚");
      await refresh();
      if (result.status === "Failed") throw new Error(result.phases.find((phase) => phase.status === "Failed")?.message ?? "Activation rollback failed");
      setError(undefined);
      ui.notify({ title: t("governance.notice.rolledBack", "Portal 已回滚"), kind: "success" });
    } catch (cause) {
      const detail = controlError(cause, t);
      setError(detail);
      ui.notify({ title: t("governance.notice.failed", "治理操作失败"), content: detail, kind: "error" });
    } finally { setBusy(false); }
  };
  return <ui.Stack gap="lg">
    {error === undefined ? null : <ui.ErrorState title={error} retry={() => void refresh()} />}
    <ui.Panel title={t("governance.panel.currentActivation", "当前线上 Activation")}><ui.Descriptions columns={{ xs: 1, lg: 3 }} items={current.length === 0 ? [{ id: "empty", label: t("governance.column.status", "状态"), value: t("governance.empty.current", "尚未激活 Portal") }] : current.flatMap((item) => [
      { id: `${item.id}:portal`, label: t("governance.column.portal", "Portal"), value: item.portalId }, { id: `${item.id}:activation`, label: t("governance.column.activation", "Activation"), value: `#${item.id} · ${item.status}${item.referencePending === true ? ` · ${t("governance.status.referencePending", "引用同步中")}` : ""}` }, { id: `${item.id}:inputs`, label: t("governance.column.inputs", "输入"), value: `Profile #${item.profileRevisionId} / Application #${item.applicationRevisionId} / Binding #${item.bindingRevisionId}` },
    ])} /></ui.Panel>
    <ui.Grid columns={{ xs: 1, lg: 2 }} gap="lg"><ui.GridItem><ui.Panel title={t("governance.panel.newActivation", "创建 Activation")}><ui.Stack gap="md"><ui.FormRenderer schema={schema} value={value} onChange={(next) => setValue(normalizeActivationValue(snapshot, next))} submitting={busy} onValidationChange={(result) => setValid(result.valid)} /><ui.Button kind="primary" disabled={!valid} loading={busy} onClick={() => void activate()}>{t("governance.action.activate", "校验并激活")}</ui.Button></ui.Stack></ui.Panel></ui.GridItem>
    <ui.GridItem><ui.Panel title={t("governance.panel.activationHistory", "不可变 Activation 历史")}><ui.Table loading={loading} rowKey="id" rows={snapshot.activations as unknown as Array<Record<string, unknown>>} empty={<ui.EmptyState title={t("governance.empty.activation", "尚无 Activation")} />} columns={[
      { key: "id", title: t("governance.column.activation", "Activation"), render: (entry) => `#${String(entry)}` }, { key: "portalId", title: t("governance.column.portal", "Portal") }, { key: "status", title: t("governance.column.status", "状态"), render: (entry) => <ui.Status tone={activationTones[entry as PortalActivation["status"]]}>{String(entry)}</ui.Status> }, { key: "referencePending", title: t("governance.column.references", "制品引用"), render: (entry) => entry === true ? <ui.Status tone="warning">{t("governance.status.referencePending", "同步中")}</ui.Status> : <ui.Status tone="success">{t("governance.status.referenceSynced", "已保护")}</ui.Status> }, { key: "createdAt", title: t("governance.column.time", "时间"), render: (entry) => typeof entry === "string" ? i18n.formatDate(entry) : "-" },
      { key: "rollback", title: t("governance.column.action", "操作"), render: (_entry, row) => row.status === "Superseded" ? <ui.Button kind="text" disabled={busy} onClick={() => void rollback(row as unknown as PortalActivation)}>{t("governance.action.rollback", "回滚到此版本")}</ui.Button> : null },
    ]} /></ui.Panel></ui.GridItem></ui.Grid>
  </ui.Stack>;
}

interface WorkspaceProps { client: PortalControlClient; snapshot: PortalGovernanceSnapshot; refresh(): Promise<void>; loading: boolean; }

const profileSchema: FormSchema = { id: "portal-profile-editor.v1", schema: { $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["name", "defaultRenderer", "allowedRenderers", "userSelectableRenderer", "defaultTemplate", "pageBodyWidth", "navigationGroups"], properties: {
  name: { type: "string", title: "Profile ID", minLength: 1 },
  defaultRenderer: { type: "string", title: "默认 UI 框架", oneOf: [{ const: "arco", title: "Arco Design" }, { const: "mui", title: "Material UI" }] },
  allowedRenderers: { type: "array", title: "允许的 UI 框架", minItems: 1, uniqueItems: true, items: { type: "string", oneOf: [{ const: "arco", title: "Arco Design" }, { const: "mui", title: "Material UI" }] } },
  userSelectableRenderer: { type: "boolean", title: "允许用户切换 UI 框架" },
  defaultTemplate: { type: "string", title: "默认布局", oneOf: [{ const: "standard", title: "标准侧栏布局" }, { const: "top-navigation", title: "顶部导航布局" }] },
  pageBodyWidth: { type: "string", title: "页面正文宽度", oneOf: [{ const: "fluid", title: "自适应" }, { const: "contained", title: "最大 1280px" }] },
  navigationGroups: { type: "array", title: "导航分组", items: { type: "object", additionalProperties: false, required: ["id", "label", "zone", "icon"], properties: { id: { type: "string" }, parentID: { type: "string" }, label: { type: "string" }, zone: { enum: ["primary", "secondary", "settings"] }, icon: { enum: ["menu", "settings", "info"] }, order: { type: "integer" } } } },
} }, uiSchema: { defaultRenderer: { "ui:widget": "select" }, defaultTemplate: { "ui:widget": "select" }, pageBodyWidth: { "ui:widget": "select" } }, localization: {
  "/properties/name/title": message(namespace, "governance.form.profileId", "Profile ID"),
  "/properties/defaultRenderer/title": message(namespace, "governance.form.renderer", "默认 UI 框架"),
  "/properties/defaultRenderer/oneOf/0/title": message(namespace, "governance.option.arco", "Arco Design"),
  "/properties/defaultRenderer/oneOf/1/title": message(namespace, "governance.option.mui", "Material UI"),
  "/properties/allowedRenderers/title": message(namespace, "governance.form.allowedRenderers", "允许的 UI 框架"),
  "/properties/userSelectableRenderer/title": message(namespace, "governance.form.userRenderer", "允许用户切换 UI 框架"),
  "/properties/defaultTemplate/title": message(namespace, "governance.form.layout", "默认布局"),
  "/properties/defaultTemplate/oneOf/0/title": message(namespace, "governance.option.standardLayout", "标准侧栏布局"),
  "/properties/defaultTemplate/oneOf/1/title": message(namespace, "governance.option.topLayout", "顶部导航布局"),
  "/properties/pageBodyWidth/title": message(namespace, "governance.form.bodyWidth", "页面正文宽度"),
  "/properties/pageBodyWidth/oneOf/0/title": message(namespace, "governance.option.fluid", "自适应"),
  "/properties/pageBodyWidth/oneOf/1/title": message(namespace, "governance.option.contained", "最大 1280px"),
  "/properties/navigationGroups/title": message(namespace, "governance.form.navigationGroups", "导航分组"),
} };

function profileValue(profile: PortalPlatformProfile): ProfileValue { return { name: profile.id, defaultRenderer: profile.renderAdapter.config.defaultRenderer === "mui" ? "mui" : "arco", allowedRenderers: profile.renderAdapter.config.allowedRenderers.filter((value): value is "arco" | "mui" => value === "arco" || value === "mui"), userSelectableRenderer: profile.renderAdapter.config.userSelectable, defaultTemplate: profile.shell.config.defaultTemplate === "top-navigation" ? "top-navigation" : "standard", pageBodyWidth: profile.shell.config.templateOptions?.[profile.shell.config.defaultTemplate]?.pageBodyWidth === "contained" ? "contained" : "fluid", navigationGroups: Array.isArray(profile.shell.config.navigationGroups) ? profile.shell.config.navigationGroups : [] }; }
function buildProfile(base: PortalPlatformProfile, value: Record<string, unknown>, newRevision: boolean): PortalPlatformProfile {
  const defaultRenderer = value.defaultRenderer === "mui" ? "mui" : "arco";
  const allowedRenderers = Array.isArray(value.allowedRenderers) ? value.allowedRenderers.filter((item): item is "arco" | "mui" => item === "arco" || item === "mui") : [defaultRenderer];
  if (!allowedRenderers.includes(defaultRenderer)) allowedRenderers.unshift(defaultRenderer);
  const defaultTemplate = value.defaultTemplate === "top-navigation" ? "top-navigation" : "standard";
  const templateOptions = { ...(base.shell.config.templateOptions ?? {}), [defaultTemplate]: { ...(base.shell.config.templateOptions?.[defaultTemplate] ?? {}), pageBodyWidth: value.pageBodyWidth as JSONValue } };
  return { ...base, id: String(value.name), revision: newRevision ? base.revision + 1 : base.revision, renderAdapter: { ...base.renderAdapter, config: { ...base.renderAdapter.config, defaultRenderer, allowedRenderers, userSelectable: value.userSelectableRenderer === true } }, shell: { ...base.shell, config: { ...base.shell.config, defaultTemplate, navigationGroups: value.navigationGroups as JSONValue, templateOptions } } };
}

function bindingSchema(profiles: PortalProfileRevision[]): FormSchema { const published = profiles.filter((item) => item.status === "Published"); return { id: "portal-binding-editor.v1", schema: { $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["portalId", "profileRevisionId", "services"], properties: {
  portalId: { type: "string", title: "Portal ID", minLength: 1 }, profileRevisionId: { type: "integer", title: "Published Profile", oneOf: published.map((item) => ({ const: item.id, title: `${item.profile.id} #${item.id}` })) },
  services: { type: "array", title: "管理服务绑定", items: { type: "object", additionalProperties: false, required: ["id", "logicalService", "routingDomain", "capabilities"], properties: { id: { type: "string" }, label: { type: "string" }, logicalService: { type: "string" }, routingDomain: { type: "string" }, capabilities: { type: "array", items: { type: "object", additionalProperties: false, required: ["capability"], properties: { capability: { type: "string" }, read: { type: "array", items: { type: "string" } }, write: { type: "array", items: { type: "string" } } } } } } } },
} }, uiSchema: { profileRevisionId: { "ui:widget": "select" } }, localization: {
  "/properties/portalId/title": message(namespace, "governance.form.portalId", "Portal ID"),
  "/properties/profileRevisionId/title": message(namespace, "governance.form.publishedProfile", "已发布 Profile"),
  "/properties/services/title": message(namespace, "governance.form.services", "管理服务绑定"),
} }; }
function bindingValue(revision: PortalBindingRevision) { return { portalId: revision.portalId, profileRevisionId: revision.profileRevisionId, services: revision.binding.services }; }
function buildBinding(base: PortalManagementBinding, value: Record<string, unknown>): PortalManagementBinding { return { ...base, portalId: String(value.portalId), services: Array.isArray(value.services) ? value.services as PortalManagementBinding["services"] : [] }; }

function activationSchema(snapshot: PortalGovernanceSnapshot): FormSchema {
  const apps = snapshot.applications.filter((item) => item.status === "Published"); const profiles = snapshot.profiles.filter((item) => item.status === "Published"); const bindings = snapshot.bindings.filter((item) => item.status === "Published");
  return { id: "portal-activation.v1", schema: { $schema: jsonSchemaDialect, type: "object", additionalProperties: false, required: ["portalId", "applicationRevisionId", "profileRevisionId", "bindingRevisionId", "expectedCurrentId"], properties: {
    portalId: { type: "string", title: "Portal ID", oneOf: [...new Set(bindings.map((item) => item.portalId))].map((id) => ({ const: id, title: id })) },
    applicationRevisionId: { type: "integer", title: "Published Application", oneOf: apps.map((item) => ({ const: item.id, title: `${item.portalId} #${item.id}` })) }, profileRevisionId: { type: "integer", title: "Published Profile", oneOf: profiles.map((item) => ({ const: item.id, title: `${item.profile.id} #${item.id}` })) }, bindingRevisionId: { type: "integer", title: "Published Binding", oneOf: bindings.map((item) => ({ const: item.id, title: `${item.portalId} #${item.id}` })) }, expectedCurrentId: { type: "integer", title: "期望当前 Activation", minimum: 0, default: 0 }, reason: { type: "string", title: "变更说明" },
  } }, uiSchema: { portalId: { "ui:widget": "select" }, applicationRevisionId: { "ui:widget": "select" }, profileRevisionId: { "ui:widget": "select" }, bindingRevisionId: { "ui:widget": "select" } }, localization: {
    "/properties/portalId/title": message(namespace, "governance.form.portalId", "Portal ID"),
    "/properties/applicationRevisionId/title": message(namespace, "governance.form.publishedApplication", "已发布 Application"),
    "/properties/profileRevisionId/title": message(namespace, "governance.form.publishedProfile", "已发布 Profile"),
    "/properties/bindingRevisionId/title": message(namespace, "governance.form.publishedBinding", "已发布 Binding"),
    "/properties/expectedCurrentId/title": message(namespace, "governance.form.expectedActivation", "期望当前 Activation"),
    "/properties/reason/title": message(namespace, "governance.form.reason", "变更说明"),
  } };
}

function activationDefaults(snapshot: PortalGovernanceSnapshot, requestedPortalID?: string): Record<string, unknown> {
  const bindings = snapshot.bindings.filter((item) => item.status === "Published");
  const binding = bindings.find((item) => item.portalId === requestedPortalID) ?? bindings[0];
  if (binding === undefined) return {};
  const application = snapshot.applications.find((item) => item.status === "Published" && item.portalId === binding.portalId);
  const current = snapshot.activations.find((item) => item.status === "Current" && item.portalId === binding.portalId);
  return {
    portalId: binding.portalId,
    applicationRevisionId: application?.id,
    profileRevisionId: binding.profileRevisionId,
    bindingRevisionId: binding.id,
    expectedCurrentId: current?.id ?? 0,
    reason: "",
  };
}

function normalizeActivationValue(snapshot: PortalGovernanceSnapshot, next: Record<string, unknown>): Record<string, unknown> {
  const portalID = typeof next.portalId === "string" ? next.portalId : undefined;
  const binding = snapshot.bindings.find((item) => item.status === "Published" && item.id === Number(next.bindingRevisionId));
  if (portalID === undefined || binding?.portalId === portalID) return next;
  return { ...next, ...activationDefaults(snapshot, portalID), portalId: portalID };
}

function controlError(cause: unknown, t: Translator, fallback = "请求被拒绝"): string {
  if (cause instanceof PortalControlError) {
    return t(`error.${cause.code}`, fallback);
  }
  return cause instanceof Error ? cause.message : fallback;
}
