import type { ComponentType, ReactNode } from "react";
import type { LocalizedText, LocaleDirection, PluginLocalization, UICapability } from "@vastplan/ui-contract";

export type ThemeTemplateScheme = "light" | "dark" | "high-contrast";

export interface ThemeTemplate {
  id: string;
  label: LocalizedText;
  scheme: ThemeTemplateScheme;
}

/** Selects icon geometry without exposing a framework package to consumers. */
export interface IconThemeTemplate {
  id: string;
  label: LocalizedText;
  source: "canonical" | "renderer-native";
}

/** A concrete UI framework implementation owned by the selected render adapter. */
export interface UIRenderer {
  id: string;
  label: LocalizedText;
  framework: string;
  capabilities: readonly UICapability[];
  themeTemplates: readonly ThemeTemplate[];
  defaultThemeTemplate: string;
  iconThemes: readonly IconThemeTemplate[];
  defaultIconTheme: string;
  Provider: ComponentType<{ children: ReactNode; locale: string; direction: LocaleDirection; themeTemplate?: string; iconTheme?: string }>;
  localization?: PluginLocalization;
}

/** A discoverable, framework-neutral renderer choice. */
export interface UIRendererTemplate {
  id: string;
  label: LocalizedText;
  framework: string;
  /** Verified first-party module, fetched only after this Renderer is selected. */
  module: {
    id: string;
    version: string;
    channel?: string;
  };
}

/** Safe Renderer metadata exposed to Shell chrome; it never leaks module routing. */
export interface UIRendererChoice {
  id: string;
  label: LocalizedText;
  framework: string;
}

export interface UIRenderAdapter {
  id: "ui.render.adapter";
  uiContract: string;
  /** Renderer catalog is owned by the Adapter; functional plugins never name a framework. */
  renderers: readonly UIRendererTemplate[];
  defaultRenderer: string;
  localization?: PluginLocalization;
}

