import type { CSSProperties, ReactNode } from "react";
import { resolveAppearanceColors, type AppearanceThemeTemplate } from "./appearance.js";
import { usePortalUI } from "./portal-ui-context.js";

export interface AppearanceTemplateSelectProps {
  readonly ariaLabel: string;
  readonly value: string;
  readonly templates: readonly AppearanceThemeTemplate[];
  readonly labelFor: (template: AppearanceThemeTemplate) => ReactNode;
  onChange(templateID: string): void;
}

/** A semantic theme-template selector. Render adapters keep ownership of the native Select control. */
export function AppearanceTemplateSelect({ ariaLabel, value, templates, labelFor, onChange }: AppearanceTemplateSelectProps) {
  const ui = usePortalUI();
  return <ui.Select ariaLabel={ariaLabel} value={value} options={templates.map((template) => ({
    value: template.id,
    label: <span style={templateOptionStyle}>
      <AppearanceTemplatePreview template={template} borderColor={ui.theme.tokens.color.border} />
      <span style={templateLabelStyle}>{labelFor(template)}</span>
    </span>,
  }))} onChange={(next) => {
    if (next !== undefined && templates.some((template) => template.id === next)) onChange(next);
  }} />;
}

function AppearanceTemplatePreview({ template, borderColor }: { template: AppearanceThemeTemplate; borderColor: string }) {
  const colors = resolveAppearanceColors(template.id);
  return <span aria-hidden style={{ display: "grid", alignSelf: "center", flex: "0 0 auto", gridTemplateRows: "4px minmax(0,1fr)", width: 42, height: 24, overflow: "hidden", border: `1px solid ${borderColor}`, borderRadius: 3, background: colors.canvas, boxSizing: "border-box" }}>
    <span style={{ background: colors.primary }} />
    <span style={{ display: "grid", gridTemplateColumns: "11px minmax(0,1fr)", gap: 2, padding: 3 }}>
      <span style={{ borderRadius: 1, background: colors.surface }} />
      <span style={{ display: "grid", gap: 2 }}>
        <span style={{ width: "76%", height: 3, borderRadius: 1, background: colors.text }} />
        <span style={{ width: "52%", height: 3, borderRadius: 1, background: colors.mutedText }} />
      </span>
    </span>
  </span>;
}

/** Normalizes the selected-value and dropdown-option vertical baselines. */
const templateOptionStyle: CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: 8,
  minHeight: 24,
  height: "100%",
  lineHeight: 1,
  verticalAlign: "middle",
};

const templateLabelStyle: CSSProperties = { display: "block", lineHeight: "20px", minWidth: 0 };
