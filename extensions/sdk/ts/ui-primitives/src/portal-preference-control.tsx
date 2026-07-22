import { useState, type CSSProperties } from "react";
import { message } from "@vastplan/ui-contract";
import { usePortalI18n } from "./i18n.js";
import { usePortalUI } from "./portal-ui-context.js";
import type { UIShellProps } from "./index.js";

const namespace = "cn.vastplan.foundation.frontend.structure.shell";

export function PortalPreferenceControl({
  availableTemplates, template, onTemplateChange,
  renderers = [], renderer, onRendererChange,
  themeTemplates = [], themeTemplateID, onThemeTemplateChange,
  iconThemes = [], iconThemeID, onIconThemeChange,
}: Pick<UIShellProps, "availableTemplates" | "template" | "onTemplateChange" | "renderers" | "renderer" | "onRendererChange" | "themeTemplates" | "themeTemplateID" | "onThemeTemplateChange" | "iconThemes" | "iconThemeID" | "onIconThemeChange">) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const [open, setOpen] = useState(false);
  const hasChoices = availableTemplates.length > 1 || renderers.length > 1 || themeTemplates.length > 1 || iconThemes.length > 1;
  if (!hasChoices) return null;
  const label = i18n.text(message(namespace, "preference.appearance", "外观设置"));
  const panelStyle = {
    display: "grid", gap: 12, minWidth: 260, maxWidth: 360, padding: 16,
    color: ui.theme.tokens.color.text, background: ui.theme.tokens.color.overlaySurface,
  } as CSSProperties;
  const fieldStyle = { display: "grid", gap: 6 } as CSSProperties;
  return <ui.Popover open={open} placement="bottom-end" ariaLabel={label} initialFocus="first" onOpenChange={setOpen} trigger={(trigger) => <button
    ref={(node) => trigger.ref(node)} type="button" aria-label={label} title={label}
    aria-expanded={trigger["aria-expanded"]} aria-controls={trigger["aria-controls"]}
    onClick={trigger.onClick} onKeyDown={trigger.onKeyDown}
    style={{ width: ui.theme.tokens.touch.minimum, height: ui.theme.tokens.touch.minimum, display: "grid", placeItems: "center", border: 0, borderRadius: ui.theme.tokens.radius.md, background: "transparent", color: "inherit", cursor: "pointer" }}
  ><ui.Icon name="settings" /></button>}>
    <section style={panelStyle}>
      <strong>{label}</strong>
      {renderers.length <= 1 ? null : <label style={fieldStyle}><span>{i18n.text(message(namespace, "preference.renderer", "UI 框架"))}</span><ui.Select ariaLabel={i18n.text(message(namespace, "preference.renderer", "UI 框架"))} value={renderer?.id} options={renderers.map((item) => ({ value: item.id, label: i18n.text(item.label) }))} onChange={(value) => { if (value !== undefined) onRendererChange?.(value); }} /></label>}
      {availableTemplates.length <= 1 ? null : <label style={fieldStyle}><span>{i18n.text(message(namespace, "preference.layout", "页面布局"))}</span><ui.Select ariaLabel={i18n.text(message(namespace, "preference.layout", "页面布局"))} value={template.id} options={availableTemplates.map((item) => ({ value: item.id, label: i18n.text(item.label) }))} onChange={(value) => { if (value !== undefined) onTemplateChange?.(value); }} /></label>}
      {themeTemplates.length <= 1 ? null : <label style={fieldStyle}><span>{i18n.text(message(namespace, "preference.theme", "主题"))}</span><ui.Select ariaLabel={i18n.text(message(namespace, "preference.theme", "主题"))} value={themeTemplateID} options={themeTemplates.map((item) => ({ value: item.id, label: i18n.text(item.label) }))} onChange={(value) => { if (value !== undefined) onThemeTemplateChange?.(value); }} /></label>}
      {iconThemes.length <= 1 ? null : <label style={fieldStyle}><span>{i18n.text(message(namespace, "preference.icons", "图标风格"))}</span><ui.Select ariaLabel={i18n.text(message(namespace, "preference.icons", "图标风格"))} value={iconThemeID} options={iconThemes.map((item) => ({ value: item.id, label: i18n.text(item.label) }))} onChange={(value) => { if (value !== undefined) onIconThemeChange?.(value); }} /></label>}
    </section>
  </ui.Popover>;
}
