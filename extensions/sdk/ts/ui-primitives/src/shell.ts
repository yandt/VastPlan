import type { ComponentType, ReactNode } from "react";
import type { LocalizedText, PluginLocalization } from "@vastplan/ui-contract";
import type { IconThemeTemplate, ThemeTemplate, UIRendererChoice } from "./renderer.js";
import type { ShellCompositionInput, ShellCompositionModel } from "./portal-runtime.js";
import type { PortalAccountSummary, PortalAppearanceSettings } from "./appearance.js";

export interface ShellBranding {
  name: string;
  logoURL?: string;
  shortName?: string;
}

export interface ShellTemplate {
  id: string;
  label: LocalizedText;
  module: { id: string; version: string; channel?: string };
}

export interface ShellTemplateSelection {
  id: string;
  options: Readonly<Record<string, unknown>>;
}

export interface UIShellProps {
  composition: ShellCompositionModel;
  template: ShellTemplateSelection;
  availableTemplates: readonly ShellTemplate[];
  onTemplateChange?(templateID: string): void;
  /** Renderer choice is optional UI chrome; Shells may surface it in account/settings slots. */
  renderers?: readonly UIRendererChoice[];
  renderer?: { id: string; options: Readonly<Record<string, unknown>> };
  onRendererChange?(rendererID: string): void;
  themeTemplates?: readonly ThemeTemplate[];
  themeTemplateID?: string;
  onThemeTemplateChange?(themeTemplateID: string): void;
  iconThemes?: readonly IconThemeTemplate[];
  iconThemeID?: string;
  onIconThemeChange?(iconThemeID: string): void;
  account: PortalAccountSummary;
  appearance: PortalAppearanceSettings;
  onAppearanceChange?(appearance: PortalAppearanceSettings): void;
  branding: ShellBranding;
  pathname: string;
  recoveryNotice?: ReactNode;
  onNavigate(pageID: string): void;
}

export {
  PortalAccountControl,
} from "./portal-account-control.js";

/** Owns stable shell semantics and a governed catalog of visual templates. */
export interface UIShellAdapter {
  id: "ui.structure.shell";
  uiContract: string;
  templates: readonly ShellTemplate[];
  defaultTemplate: string;
  compose(input: ShellCompositionInput): ShellCompositionModel;
  localization?: PluginLocalization;
}

/** One visual implementation selected from the governed Shell catalog. */
export interface UIShellLibrary {
  id: string;
  shell: "ui.structure.shell";
  uiContract: string;
  Shell: ComponentType<UIShellProps>;
  localization?: PluginLocalization;
}

export { PortalUIProvider, usePortalUI } from "./portal-ui-context.js";
