import { createContext, createElement, useContext } from "react";
import type { ComponentType, ReactNode } from "react";
import type { FormSchema, UICapability } from "@vastplan/ui-contract";

export type { FormCondition, FormField, FormFieldType, FormOption, FormSchema, FormValidation, UICapability } from "@vastplan/ui-contract";
export { uiContractVersion as portalUIContractVersion } from "@vastplan/ui-contract";

export interface FormRendererProps {
  schema: FormSchema;
  value: Record<string, unknown>;
  onChange(value: Record<string, unknown>): void;
  readOnly?: boolean;
  submitting?: boolean;
}

export interface MenuItem {
  id: string;
  label: string;
  icon?: ReactNode;
  href?: string;
  disabled?: boolean;
  children?: MenuItem[];
}

export interface PageProps {
  title?: string;
  children: ReactNode;
  actions?: ReactNode;
}

export interface PanelProps {
  title?: string;
  children: ReactNode;
}

export interface ButtonProps {
  children: ReactNode;
  onClick?(): void;
  disabled?: boolean;
  loading?: boolean;
}

export interface PortalUI {
  Page: ComponentType<PageProps>;
  Panel: ComponentType<PanelProps>;
  Button: ComponentType<ButtonProps>;
  Menu: ComponentType<{ items: MenuItem[]; activeID?: string; onSelect?(id: string): void }>;
  FormRenderer: ComponentType<FormRendererProps>;
  EmptyState: ComponentType<{ title: string; description?: string }>;
  ErrorState: ComponentType<{ title: string; retry?(): void }>;
  Busy: ComponentType<{ label?: string }>;
  notify(message: { title: string; content?: string; kind?: "info" | "success" | "warning" | "error" }): void;
  confirm(message: { title: string; content?: string }): Promise<boolean>;
}

export interface DesignSystemAdapter {
  id: "ui.design-system";
  framework: string;
  uiContract: string;
  capabilities: readonly UICapability[];
  Provider: ComponentType<{ children: ReactNode }>;
}

const portalUIContext = createContext<PortalUI | null>(null);

export function PortalUIProvider({ ui, children }: { ui: PortalUI; children?: ReactNode }) {
  return createElement(portalUIContext.Provider, { value: ui }, children);
}

/** Obtains the Portal-selected design system without exposing its underlying framework. */
export function usePortalUI(): PortalUI {
  const ui = useContext(portalUIContext);
  if (ui === null) {
    throw new Error("Portal UI 未初始化：功能插件只能在设计系统 Provider 内渲染");
  }
  return ui;
}
