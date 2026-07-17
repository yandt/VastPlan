import { createContext, createElement, useContext } from "react";
import type { ComponentType, ReactNode } from "react";

/** The single UI contract version accepted by the first Portal release. */
export const portalUIContractVersion = "1.0.0" as const;

export type UICapability =
  | "layout"
  | "menu"
  | "overlay"
  | "form"
  | "data"
  | "feedback"
  | "theme";

export type FormFieldType =
  | "text"
  | "textarea"
  | "number"
  | "boolean"
  | "select"
  | "multiSelect"
  | "date"
  | "object"
  | "array"
  | "secretRef";

export interface FormCondition {
  key: string;
  equals?: unknown;
  notEquals?: unknown;
}

export interface FormValidation {
  required?: boolean;
  min?: number;
  max?: number;
  pattern?: string;
  message?: string;
}

export interface FormOption {
  label: string;
  value: string | number | boolean;
  disabled?: boolean;
}

/** A framework-neutral form definition. `secretRef` is always a reference, never a secret value. */
export interface FormField {
  key: string;
  type: FormFieldType;
  title: string;
  help?: string;
  defaultValue?: unknown;
  options?: FormOption[];
  validation?: FormValidation;
  visibleWhen?: FormCondition;
  readOnly?: boolean;
  disabled?: boolean;
  fields?: FormField[];
}

export interface FormSchema {
  id: string;
  title?: string;
  fields: FormField[];
}

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
