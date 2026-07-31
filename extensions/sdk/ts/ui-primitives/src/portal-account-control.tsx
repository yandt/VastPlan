import { useEffect, useMemo, useState, type CSSProperties } from "react";
import { message } from "@vastplan/ui-contract";
import {
  appearanceContrastIssue,
  builtinAppearanceTemplates,
  resolveAppearanceColors,
  type AppearanceColorOverrides,
  type AppearanceScheme,
  type PortalAppearanceSettings,
} from "./appearance.js";
import { usePortalI18n } from "./i18n.js";
import { usePortalUI } from "./portal-ui-context.js";
import type { UIShellProps } from "./shell.js";

const namespace = "cn.vastplan.foundation.frontend.structure.shell";
const colorFields = ["canvas", "surface", "text", "mutedText", "border", "primary", "danger", "warning", "success"] as const;

/** A single account entry point; appearance is edited here and persisted by the trusted host. */
export function PortalAccountControl(props: UIShellProps & { placement?: "top-start" | "bottom-end" }) {
  const { account } = props;
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const [menuOpen, setMenuOpen] = useState(false);
  const [dialog, setDialog] = useState<"profile" | "settings">();
  const label = i18n.text(message(namespace, "account.open", "打开用户菜单"));
  const initials = account.displayName.trim().slice(0, 1).toUpperCase() || "U";
  return <>
    <ui.Popover open={menuOpen} placement={props.placement ?? "top-start"} ariaLabel={label} initialFocus="first" onOpenChange={setMenuOpen} trigger={(trigger) => <button
      ref={(node) => trigger.ref(node)} type="button" className="vp-account-avatar" aria-label={label} title={account.displayName}
      aria-expanded={trigger["aria-expanded"]} aria-controls={trigger["aria-controls"]} onClick={trigger.onClick} onKeyDown={trigger.onKeyDown}
      style={{ width: 32, height: 32, borderRadius: "50%", border: 0, display: "grid", placeItems: "center", color: "#fff", background: ui.theme.tokens.color.primary, fontWeight: 700, cursor: "pointer" }}
    >{initials}</button>}>
      <ui.Menu variant="action" size="sm" items={[
        { id: "profile", label: i18n.text(message(namespace, "account.profile", "用户信息")) },
        { id: "settings", label: i18n.text(message(namespace, "account.settings", "用户设置")) },
      ]} onSelect={(id) => { setMenuOpen(false); setDialog(id as "profile" | "settings"); }} />
    </ui.Popover>
    <ui.Dialog open={dialog === "profile"} title={i18n.text(message(namespace, "account.profile", "用户信息"))} width="sm" onClose={() => setDialog(undefined)}>
      <dl style={{ display: "grid", gridTemplateColumns: "max-content 1fr", gap: "12px 20px", margin: 0 }}>
        <dt>{i18n.text(message(namespace, "account.name", "名称"))}</dt><dd style={{ margin: 0 }}>{account.displayName}</dd>
        <dt>{i18n.text(message(namespace, "account.id", "用户 ID"))}</dt><dd style={{ margin: 0, overflowWrap: "anywhere" }}>{account.subjectID}</dd>
        <dt>{i18n.text(message(namespace, "account.tenant", "租户"))}</dt><dd style={{ margin: 0, overflowWrap: "anywhere" }}>{account.tenantID}</dd>
      </dl>
    </ui.Dialog>
    <AppearanceDialog {...props} open={dialog === "settings"} onClose={() => setDialog(undefined)} />
  </>;
}

function AppearanceDialog(props: UIShellProps & { open: boolean; onClose(): void }) {
  const { appearance, open, onClose } = props;
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const [draft, setDraft] = useState(appearance);
  const [scheme, setScheme] = useState<AppearanceScheme>(appearance.mode === "dark" ? "dark" : "light");
  useEffect(() => { if (open) setDraft(appearance); }, [appearance, open]);
  useEffect(() => { if (open) setScheme(appearance.mode === "dark" ? "dark" : "light"); }, [appearance.mode, open]);
  const issue = appearanceContrastIssue(draft.light.templateID, draft.light.colors) ?? appearanceContrastIssue(draft.dark.templateID, draft.dark.colors);
  return <ui.Dialog open={open} title={i18n.text(message(namespace, "account.appearance", "用户设置 · 外观"))} width="lg" contentOverflow="scroll" onClose={onClose} footer={<ui.Stack direction="row" justify="end" gap="sm">
    <ui.Button kind="secondary" onClick={onClose}>{i18n.text(message(namespace, "common.cancel", "取消"))}</ui.Button>
    <ui.Button kind="primary" disabled={issue !== undefined} onClick={() => { props.onAppearanceChange?.(draft); onClose(); }}>{i18n.text(message(namespace, "common.apply", "应用"))}</ui.Button>
  </ui.Stack>}>
    <div style={{ display: "grid", gap: 18 }}>
      <ChoiceFields {...props} draft={draft} onDraft={setDraft} />
      <ui.Tabs activeID={scheme} onChange={(next) => setScheme(next as AppearanceScheme)} items={(["light", "dark"] as const).map((itemScheme) => ({
        id: itemScheme,
        label: i18n.text(message(namespace, `appearance.${itemScheme}`, itemScheme === "light" ? "浅色" : "深色")),
        content: <SchemeEditor scheme={itemScheme} value={draft} onChange={setDraft} />,
      }))} />
      {issue === undefined ? null : <ui.Status tone="error">{issue}</ui.Status>}
      <small style={{ color: ui.theme.tokens.color.mutedText }}>{i18n.text(message(namespace, "appearance.localOnly", "外观只保存在当前浏览器，不会上传到服务器。"))}</small>
    </div>
  </ui.Dialog>;
}

function ChoiceFields({ availableTemplates, template, onTemplateChange, renderers = [], renderer, onRendererChange, iconThemes = [], iconThemeID, onIconThemeChange, draft, onDraft }: UIShellProps & { draft: PortalAppearanceSettings; onDraft(value: PortalAppearanceSettings): void }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const fieldStyle = { display: "grid", gap: 6 } as CSSProperties;
  return <ui.Grid columns={{ xs: 1, md: 2 }} gap="md">
    <label style={fieldStyle}><span>{i18n.text(message(namespace, "appearance.mode", "主题模式"))}</span><ui.Select ariaLabel="主题模式" value={draft.mode} options={[
      { value: "system", label: i18n.text(message(namespace, "appearance.system", "跟随系统")) }, { value: "light", label: "浅色" }, { value: "dark", label: "深色" },
    ]} onChange={(mode) => { if (mode) onDraft({ ...draft, mode: mode as PortalAppearanceSettings["mode"] }); }} /></label>
    {renderers.length <= 1 ? null : <label style={fieldStyle}><span>UI 框架</span><ui.Select ariaLabel="UI 框架" value={renderer?.id} options={renderers.map((item) => ({ value: item.id, label: i18n.text(item.label) }))} onChange={(value) => { if (value) onRendererChange?.(value); }} /></label>}
    {availableTemplates.length <= 1 ? null : <label style={fieldStyle}><span>页面布局</span><ui.Select ariaLabel="页面布局" value={template.id} options={availableTemplates.map((item) => ({ value: item.id, label: i18n.text(item.label) }))} onChange={(value) => { if (value) onTemplateChange?.(value); }} /></label>}
    {iconThemes.length <= 1 ? null : <label style={fieldStyle}><span>图标风格</span><ui.Select ariaLabel="图标风格" value={iconThemeID} options={iconThemes.map((item) => ({ value: item.id, label: i18n.text(item.label) }))} onChange={(value) => { if (value) onIconThemeChange?.(value); }} /></label>}
  </ui.Grid>;
}

function SchemeEditor({ scheme, value, onChange }: { scheme: AppearanceScheme; value: PortalAppearanceSettings; onChange(value: PortalAppearanceSettings): void }) {
  const i18n = usePortalI18n();
  const current = value[scheme];
  const colors = resolveAppearanceColors(current.templateID, current.colors);
  const templates = builtinAppearanceTemplates.filter((item) => item.scheme === scheme);
  const updateColors = (key: typeof colorFields[number], color: string) => onChange({ ...value, [scheme]: { ...current, colors: { ...current.colors, [key]: color } } });
  return <div style={{ display: "grid", gap: 16, paddingTop: 12 }}>
    <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit,minmax(120px,1fr))", gap: 8 }}>{templates.map((item) => <button key={item.id} type="button" onClick={() => onChange({ ...value, [scheme]: { ...current, templateID: item.id, colors: undefined } })} style={{ padding: 8, border: item.id === current.templateID ? `2px solid ${item.preview.accent}` : "1px solid #d9d9d9", borderRadius: 8, background: item.preview.background, color: resolveAppearanceColors(item.id).text, cursor: "pointer" }}>{i18n.text(item.label)}</button>)}</div>
    <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit,minmax(160px,1fr))", gap: 10 }}>{colorFields.map((key) => <label key={key} style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8 }}><span>{colorLabel(key)}</span><input type="color" value={colors[key]} onChange={(event) => updateColors(key, event.currentTarget.value)} /></label>)}</div>
  </div>;
}

function colorLabel(key: typeof colorFields[number]): string {
  return ({ canvas: "页面背景", surface: "组件背景", text: "正文", mutedText: "次要文字", border: "边框", primary: "强调色", danger: "危险", warning: "警告", success: "成功" })[key];
}
