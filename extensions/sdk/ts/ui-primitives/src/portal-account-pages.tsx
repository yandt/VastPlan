import { useEffect, useState, type CSSProperties, type ReactNode } from "react";
import { message } from "@vastplan/ui-contract";
import { appearanceContrastIssue, builtinAppearanceTemplates, resolveAppearanceColors, type AppearanceScheme, type PortalAppearanceSettings } from "./appearance.js";
import { usePortalI18n } from "./i18n.js";
import { usePortalUI } from "./portal-ui-context.js";
import { usePortalPersonalization, type PortalPersonalization } from "./portal-personalization.js";

const namespace = "cn.vastplan.foundation.frontend.identity.account-center";
const colorFields = ["canvas", "surface", "text", "mutedText", "border", "primary", "danger", "warning", "success"] as const;

export function PortalAccountProfilePage() {
  const { account } = usePortalPersonalization();
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  return <ui.Panel title={i18n.text(message(namespace, "profile.summary", "基本信息"))}>
    <ui.Descriptions columns={1} items={[
      { id: "name", label: i18n.text(message(namespace, "profile.name", "名称")), value: account.displayName },
      { id: "subject", label: i18n.text(message(namespace, "profile.subject", "用户 ID")), value: account.subjectID },
      { id: "tenant", label: i18n.text(message(namespace, "profile.tenant", "租户")), value: account.tenantID },
    ]} />
  </ui.Panel>;
}

export function PortalAppearanceSettingsPage() {
  const props = usePortalPersonalization();
  const { appearance } = props;
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const [draft, setDraft] = useState(appearance);
  const [scheme, setScheme] = useState<AppearanceScheme>(appearance.mode === "dark" ? "dark" : "light");
  useEffect(() => setDraft(appearance), [appearance]);
  const issue = appearanceIssue(draft);
  const changeDraft = (next: PortalAppearanceSettings) => {
    setDraft(next);
    applyAppearanceChange(next, props.onAppearanceChange);
  };
  return <ui.Panel title={i18n.text(message(namespace, "appearance.summary", "外观设置"))}>
    <div style={{ display: "grid", gap: 24, maxWidth: 720 }}>
      <AppearanceSection title={i18n.text(message(namespace, "appearance.preferences", "偏好设置"))} description={i18n.text(message(namespace, "appearance.preferencesHint", "切换后立即生效"))}>
        <ChoiceFields {...props} draft={draft} onDraft={changeDraft} />
      </AppearanceSection>
      <AppearanceSection title={i18n.text(message(namespace, "appearance.theme", "主题与颜色"))} description={i18n.text(message(namespace, "appearance.themeHint", "分别配置浅色与深色模式"))}>
        <ui.Tabs activeID={scheme} onChange={(next) => setScheme(next as AppearanceScheme)} items={(["light", "dark"] as const).map((itemScheme) => ({
          id: itemScheme,
          label: i18n.text(message(namespace, `appearance.${itemScheme}`, itemScheme === "light" ? "浅色" : "深色")),
          content: <SchemeEditor scheme={itemScheme} value={draft} onChange={changeDraft} />,
        }))} />
      </AppearanceSection>
      {issue === undefined ? null : <ui.Status tone="error">{issue}</ui.Status>}
      <small style={{ color: ui.theme.tokens.color.mutedText }}>{i18n.text(message(namespace, "appearance.localOnly", "外观只保存在当前浏览器，修改后即时生效且不会上传到服务器。"))}</small>
    </div>
  </ui.Panel>;
}

/** Applies only readable themes; invalid color drafts remain visible for correction. */
export function applyAppearanceChange(next: PortalAppearanceSettings, onChange?: (appearance: PortalAppearanceSettings) => void): boolean {
  if (appearanceIssue(next) !== undefined) return false;
  onChange?.(next);
  return true;
}

function appearanceIssue(appearance: PortalAppearanceSettings): string | undefined {
  return appearanceContrastIssue(appearance.light.templateID, appearance.light.colors) ?? appearanceContrastIssue(appearance.dark.templateID, appearance.dark.colors);
}

function AppearanceSection({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  const ui = usePortalUI();
  return <section style={{ display: "grid", gap: 16, paddingBottom: 24, borderBottom: `1px solid ${ui.theme.tokens.color.border}` }}>
    <div style={{ display: "grid", gap: 4 }}>
      <strong style={{ color: ui.theme.tokens.color.text, fontSize: 16 }}>{title}</strong>
      <small style={{ color: ui.theme.tokens.color.mutedText }}>{description}</small>
    </div>
    {children}
  </section>;
}

function ChoiceFields({ availableTemplates, template, onTemplateChange, renderers = [], renderer, onRendererChange, iconThemes = [], iconThemeID, onIconThemeChange, draft, onDraft }: PortalPersonalization & { draft: PortalAppearanceSettings; onDraft(value: PortalAppearanceSettings): void }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const fieldStyle = { display: "grid", gap: 8, width: "100%" } as CSSProperties;
  return <div style={{ display: "grid", gap: 16, width: "100%" }}>
    <label style={fieldStyle}><span>{i18n.text(message(namespace, "appearance.mode", "主题模式"))}</span><ui.Select ariaLabel={i18n.text(message(namespace, "appearance.mode", "主题模式"))} value={draft.mode} options={[
      { value: "system", label: i18n.text(message(namespace, "appearance.system", "跟随系统")) },
      { value: "light", label: i18n.text(message(namespace, "appearance.light", "浅色")) },
      { value: "dark", label: i18n.text(message(namespace, "appearance.dark", "深色")) },
    ]} onChange={(mode) => { if (mode) onDraft({ ...draft, mode: mode as PortalAppearanceSettings["mode"] }); }} /></label>
    {renderers.length <= 1 ? null : <label style={fieldStyle}><span>{i18n.text(message(namespace, "appearance.framework", "UI 框架"))}</span><ui.Select ariaLabel={i18n.text(message(namespace, "appearance.framework", "UI 框架"))} value={renderer?.id} options={renderers.map((item) => ({ value: item.id, label: i18n.text(item.label) }))} onChange={(value) => { if (value) onRendererChange?.(value); }} /></label>}
    {availableTemplates.length <= 1 ? null : <label style={fieldStyle}><span>{i18n.text(message(namespace, "appearance.layout", "页面布局"))}</span><ui.Select ariaLabel={i18n.text(message(namespace, "appearance.layout", "页面布局"))} value={template.id} options={availableTemplates.map((item) => ({ value: item.id, label: i18n.text(item.label) }))} onChange={(value) => { if (value) onTemplateChange?.(value); }} /></label>}
    {iconThemes.length <= 1 ? null : <label style={fieldStyle}><span>{i18n.text(message(namespace, "appearance.icons", "图标风格"))}</span><ui.Select ariaLabel={i18n.text(message(namespace, "appearance.icons", "图标风格"))} value={iconThemeID} options={iconThemes.map((item) => ({ value: item.id, label: i18n.text(item.label) }))} onChange={(value) => { if (value) onIconThemeChange?.(value); }} /></label>}
  </div>;
}

function SchemeEditor({ scheme, value, onChange }: { scheme: AppearanceScheme; value: PortalAppearanceSettings; onChange(value: PortalAppearanceSettings): void }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const current = value[scheme];
  const colors = resolveAppearanceColors(current.templateID, current.colors);
  const templates = builtinAppearanceTemplates.filter((item) => item.scheme === scheme);
  const updateColors = (key: typeof colorFields[number], color: string) => onChange({ ...value, [scheme]: { ...current, colors: { ...current.colors, [key]: color } } });
  return <div style={{ display: "grid", gap: 24, paddingTop: 8 }}>
    <div style={{ display: "grid", gap: 8 }}>{templates.map((item) => <button key={item.id} type="button" aria-pressed={item.id === current.templateID} onClick={() => onChange({ ...value, [scheme]: { ...current, templateID: item.id, colors: undefined } })} style={{ display: "grid", gridTemplateColumns: "12px minmax(0,1fr)", alignItems: "center", columnGap: 10, minHeight: 40, padding: "8px 12px", border: item.id === current.templateID ? `${ui.theme.tokens.focus.width}px solid ${item.preview.accent}` : `1px solid ${ui.theme.tokens.color.border}`, borderRadius: ui.theme.tokens.radius.sm, background: item.preview.background, color: resolveAppearanceColors(item.id).text, textAlign: "left", cursor: "pointer" }}><span aria-hidden style={{ width: 10, height: 10, borderRadius: "50%", background: item.preview.accent }} />{i18n.text(message(namespace, `theme.${item.id}`, themeLabel(item.id)))}</button>)}</div>
    <div style={{ display: "grid", gap: 12 }}>{colorFields.map((key) => <label key={key} style={{ display: "flex", alignItems: "center", justifyContent: "space-between", minHeight: 32, gap: 16, paddingBottom: 8, borderBottom: `1px solid ${ui.theme.tokens.color.border}` }}><span>{i18n.text(message(namespace, `color.${key}`, colorLabel(key)))}</span><input aria-label={i18n.text(message(namespace, `color.${key}`, colorLabel(key)))} type="color" value={colors[key]} onChange={(event) => updateColors(key, event.currentTarget.value)} /></label>)}</div>
  </div>;
}

function colorLabel(key: typeof colorFields[number]): string {
  return ({ canvas: "页面背景", surface: "组件背景", text: "正文", mutedText: "次要文字", border: "边框", primary: "强调色", danger: "危险", warning: "警告", success: "成功" })[key];
}

function themeLabel(id: string): string {
  return ({ light: "浅色经典", "light-soft": "浅色柔和", "light-warm": "浅色暖调", dark: "深色石墨", "dark-midnight": "深色午夜", "dark-slate": "深色蓝灰" } as Record<string, string>)[id] ?? id;
}
