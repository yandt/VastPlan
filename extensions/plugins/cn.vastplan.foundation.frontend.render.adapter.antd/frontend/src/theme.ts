import { builtinAppearanceTemplates, message, type SemanticThemeTokens } from "@vastplan/ui-primitives";

export const namespace = "cn.vastplan.foundation.frontend.render.adapter";
export const gaps = { xs: 4, sm: 8, md: 16, lg: 24 } as const;
export const dialogWidths = { sm: 480, md: 720, lg: 960 } as const;

export const antdThemeTemplates = builtinAppearanceTemplates;

export const antdIconThemes = Object.freeze([
  { id: "canonical", label: message(namespace, "iconTheme.canonical", "VastPlan 图标"), source: "canonical" as const },
  { id: "renderer-native", label: message(namespace, "iconTheme.native", "Ant Design 原生图标"), source: "renderer-native" as const },
]);

export function antdThemeTemplate(id: string | undefined) {
  return antdThemeTemplates.find((template) => template.id === id) ?? antdThemeTemplates[0];
}

export function antdIconTheme(id: string | undefined) {
  return antdIconThemes.find((theme) => theme.id === id) ?? antdIconThemes[0];
}

export const semanticTokens: SemanticThemeTokens = {
  color: {
    canvas: "var(--ant-color-bg-layout)", surface: "var(--ant-color-bg-container)", overlaySurface: "var(--ant-color-bg-elevated)",
    text: "var(--ant-color-text)", mutedText: "var(--ant-color-text-secondary)", border: "var(--ant-color-border-secondary)",
    primary: "var(--ant-color-primary)", danger: "var(--ant-color-error)", warning: "var(--ant-color-warning)", success: "var(--ant-color-success)",
    hover: "var(--ant-color-fill-tertiary)", selected: "var(--ant-color-primary-bg)", focusRing: "var(--ant-color-primary)",
  },
  radius: { sm: 4, md: 6, lg: 8 },
  spacing: gaps,
  shell: { barHeight: 64, railWidth: 64, navigationWidth: 240, navigationCompactWidth: 220 },
  overlay: { navigationMinWidth: 480, navigationMaxWidth: 840 },
  elevation: { overlay: "0 8px 24px rgba(0,0,0,.12)" },
  motion: { fast: 120, normal: 180 },
  focus: { width: 2 },
  touch: { minimum: 44 },
};
