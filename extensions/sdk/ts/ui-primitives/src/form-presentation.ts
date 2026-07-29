import type { ComponentSize, FormLabelPlacement, FormPresentation, FormSectionPresentation } from "@vastplan/ui-contract";
import type { CSSProperties } from "react";

const defaultColumns = 1;

export function resolveFormPresentation(source: FormPresentation | undefined): FormPresentation {
  const preset = source?.preset ?? "standard";
  const labelPlacement = source?.labelPlacement ?? legacyOrPresetLabel(source, preset);
  const layout = source?.layout ?? (preset === "compact" ? "compact" : preset === "comfortable" || preset === "guided" ? "vertical" : "horizontal");
  const navigation = source?.navigation ?? (preset === "guided" ? "steps" : undefined);
  return Object.freeze({ ...source, preset, layout, labelPlacement, ...(navigation === undefined ? {} : { navigation }) });
}

export function formControlSize(presentation: FormPresentation | undefined): ComponentSize {
  switch (presentation?.preset) {
    case "compact": return "sm";
    case "comfortable": return "lg";
    default: return "md";
  }
}

export function formLabelPlacement(presentation: FormPresentation | undefined): FormLabelPlacement {
  return resolveFormPresentation(presentation).labelPlacement ?? "inline";
}

export function formGridColumns(presentation: FormPresentation | undefined, section?: FormSectionPresentation): number {
  return section?.columns ?? presentation?.columns ?? defaultColumns;
}

export function formGridTemplate(presentation: FormPresentation | undefined, section?: FormSectionPresentation): string {
  const columns = formGridColumns(presentation, section);
  const widths = section?.columnWidths ?? (section?.columns === undefined ? presentation?.columnWidths : undefined);
  const total = widths?.reduce((sum, value) => sum + value, 0);
  if (widths?.length === columns && widths.every((value) => Number.isFinite(value) && value > 0) && total !== undefined && Math.abs(total - 100) <= 0.01) {
    return widths.map((value) => `minmax(0, ${value}fr)`).join(" ");
  }
  return `repeat(${columns}, minmax(0, 1fr))`;
}

export const formGridClassName = "vp-form-grid";
export const formGridCSS = ".vp-form-grid{box-sizing:border-box;display:grid;grid-template-columns:var(--vp-form-grid-columns);gap:16px;width:100%;min-width:0}.vp-form-grid>*{min-width:0}@media(max-width:767px){.vp-form-grid{grid-template-columns:minmax(0,1fr)!important}.vp-form-grid>*{grid-column:span 1!important}}";

export function formGridStyle(presentation: FormPresentation | undefined, section?: FormSectionPresentation): CSSProperties {
  return { "--vp-form-grid-columns": formGridTemplate(presentation, section) } as CSSProperties;
}

function legacyOrPresetLabel(source: FormPresentation | undefined, preset: NonNullable<FormPresentation["preset"]>): FormLabelPlacement {
  if (source?.layout === "vertical") return "stacked";
  if (source?.layout === "horizontal") return "inline";
  return preset === "comfortable" || preset === "guided" ? "stacked" : "inline";
}
