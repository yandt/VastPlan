import type { ReactNode } from "react";
import type { AppearanceThemeTemplate } from "./appearance.js";
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
    label: <span style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
      <span aria-hidden style={{ width: 10, height: 10, borderRadius: "50%", background: template.preview.accent, boxShadow: `0 0 0 1px ${ui.theme.tokens.color.border}` }} />
      <span>{labelFor(template)}</span>
    </span>,
  }))} onChange={(next) => {
    if (next !== undefined && templates.some((template) => template.id === next)) onChange(next);
  }} />;
}
