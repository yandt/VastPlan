import { useEffect, useState, type ReactNode } from "react";
import { message } from "@vastplan/ui-contract";
import { appearanceContrastIssue, builtinAppearanceTemplates, resolveAppearanceColors, type AppearanceScheme, type PortalAppearanceSettings } from "./appearance.js";
import { AppearanceModeSelector } from "./appearance-mode-selector.js";
import { AppearanceTemplateSelect } from "./appearance-template-select.js";
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
  return <div style={{ display: "grid", gap: 24, maxWidth: 720 }}>
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
    </div>;
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

function AppearanceInlineField({ label, children }: { label: string; children: ReactNode }) {
  return <div style={{ display: "grid", gridTemplateColumns: "minmax(112px, 180px) minmax(0, 1fr)", alignItems: "center", columnGap: 16, minHeight: 32, width: "100%" }}>
    <span title={label} style={{ overflow: "hidden", color: "inherit", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{label}</span>
    <div style={{ display: "flex", minWidth: 0 }}>{children}</div>
  </div>;
}

function ChoiceFields({ availableTemplates, template, onTemplateChange, renderers = [], renderer, onRendererChange, iconThemes = [], iconThemeID, onIconThemeChange, draft, onDraft }: PortalPersonalization & { draft: PortalAppearanceSettings; onDraft(value: PortalAppearanceSettings): void }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  return <div style={{ display: "grid", gap: 16, width: "100%" }}>
    <AppearanceInlineField label={i18n.text(message(namespace, "appearance.mode", "主题模式"))}><AppearanceModeSelector ariaLabel={i18n.text(message(namespace, "appearance.mode", "主题模式"))} value={draft.mode} appearance={draft} labels={{ system: i18n.text(message(namespace, "appearance.system", "跟随系统")), light: i18n.text(message(namespace, "appearance.light", "浅色")), dark: i18n.text(message(namespace, "appearance.dark", "深色")) }} onChange={(mode) => onDraft({ ...draft, mode })} /></AppearanceInlineField>
    {renderers.length <= 1 ? null : <AppearanceInlineField label={i18n.text(message(namespace, "appearance.framework", "UI 框架"))}><ui.Select ariaLabel={i18n.text(message(namespace, "appearance.framework", "UI 框架"))} value={renderer?.id} options={renderers.map((item) => ({ value: item.id, label: i18n.text(item.label) }))} onChange={(value) => { if (value) onRendererChange?.(value); }} /></AppearanceInlineField>}
    {availableTemplates.length <= 1 ? null : <AppearanceInlineField label={i18n.text(message(namespace, "appearance.layout", "页面布局"))}><ui.Select ariaLabel={i18n.text(message(namespace, "appearance.layout", "页面布局"))} value={template.id} options={availableTemplates.map((item) => ({ value: item.id, label: i18n.text(item.label) }))} onChange={(value) => { if (value) onTemplateChange?.(value); }} /></AppearanceInlineField>}
    {iconThemes.length <= 1 ? null : <AppearanceInlineField label={i18n.text(message(namespace, "appearance.icons", "图标风格"))}><ui.Select ariaLabel={i18n.text(message(namespace, "appearance.icons", "图标风格"))} value={iconThemeID} options={iconThemes.map((item) => ({ value: item.id, label: i18n.text(item.label) }))} onChange={(value) => { if (value) onIconThemeChange?.(value); }} /></AppearanceInlineField>}
  </div>;
}

function SchemeEditor({ scheme, value, onChange }: { scheme: AppearanceScheme; value: PortalAppearanceSettings; onChange(value: PortalAppearanceSettings): void }) {
  const i18n = usePortalI18n();
  const current = value[scheme];
  const colors = resolveAppearanceColors(current.templateID, current.colors);
  const templates = builtinAppearanceTemplates.filter((item) => item.scheme === scheme);
  const updateColors = (key: typeof colorFields[number], color: string) => onChange({ ...value, [scheme]: { ...current, colors: { ...current.colors, [key]: color } } });
  return <div style={{ display: "grid", gap: 24, paddingTop: 8 }}>
    <AppearanceInlineField label={i18n.text(message(namespace, "appearance.template", "主题模板"))}><AppearanceTemplateSelect ariaLabel={i18n.text(message(namespace, "appearance.template", "主题模板"))} value={current.templateID} templates={templates} labelFor={(item) => i18n.text(message(namespace, `theme.${item.id}`, themeLabel(item.id)))} onChange={(templateID) => onChange({ ...value, [scheme]: { ...current, templateID, colors: undefined } })} /></AppearanceInlineField>
    <div style={{ display: "grid", gap: 12 }}>{colorFields.map((key) => <AppearanceInlineField key={key} label={i18n.text(message(namespace, `color.${key}`, colorLabel(key)))}><input aria-label={i18n.text(message(namespace, `color.${key}`, colorLabel(key)))} type="color" value={colors[key]} onChange={(event) => updateColors(key, event.currentTarget.value)} /></AppearanceInlineField>)}</div>
  </div>;
}


function colorLabel(key: typeof colorFields[number]): string {
  return ({ canvas: "页面背景", surface: "组件背景", text: "正文", mutedText: "次要文字", border: "边框", primary: "强调色", danger: "危险", warning: "警告", success: "成功" })[key];
}

function themeLabel(id: string): string {
  return ({ light: "浅色经典", "light-soft": "浅色柔和", "light-warm": "浅色暖调", dark: "深色石墨", "dark-midnight": "深色午夜", "dark-slate": "深色蓝灰" } as Record<string, string>)[id] ?? id;
}
