import { createContext, createElement, useContext } from "react";
import type { ComponentType, ReactNode } from "react";
import type { FormSchema, FormValidationResult, JSONValue, UICapability } from "@vastplan/ui-contract";

export type { FormSchema, FormUISchema, FormValidationIssue, FormValidationResult, InteractionAuditEvent, InteractionRecord, InteractionResponse, InteractionState, JSONPrimitive, JSONSchema, JSONValue, UICapability } from "@vastplan/ui-contract";
export { jsonSchemaDialect } from "@vastplan/ui-contract";
export { uiContractVersion as portalUIContractVersion } from "@vastplan/ui-contract";
export { PortalInteractionClient, PortalInteractionError } from "./interaction-client.js";
export type { PortalFetch, PortalFetchResponse, PortalInteractionClientOptions } from "./interaction-client.js";
export { PortalControlClient, PortalControlError } from "./portal-control-client.js";
export type { PortalApplicationComposition, PortalAuditEvent, PortalCompositionRef, PortalControlClientOptions, PortalManagementBinding, PortalManagementGrant, PortalPluginRef, PortalRevision, PortalRevisionStatus } from "./portal-control-client.js";

export interface FormRendererProps {
  schema: FormSchema;
  value: Record<string, unknown>;
  onChange(value: Record<string, unknown>): void;
  readOnly?: boolean;
  submitting?: boolean;
  errors?: Readonly<Record<string, string>>;
  context?: Readonly<Record<string, unknown>>;
  validate?(request: { schema: FormSchema; value: Readonly<Record<string, unknown>>; context: Readonly<Record<string, unknown>>; signal: AbortSignal }): Promise<Readonly<Record<string, string>>>;
  validationDelayMs?: number;
  onValidationChange?(result: FormRendererValidationState): void;
}

export interface FormRendererValidationState extends FormValidationResult {
  errors: Readonly<Record<string, string>>;
  validating: boolean;
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

export type SpaceSize = "xs" | "sm" | "md" | "lg";
export type ResponsiveColumns = number | { xs?: number; sm?: number; md?: number; lg?: number; xl?: number };

export interface PortalShellProps {
  header?: ReactNode;
  navigation?: ReactNode;
  inspector?: ReactNode;
  statusBar?: ReactNode;
  children: ReactNode;
}

export interface StackProps {
  direction?: "row" | "column";
  gap?: SpaceSize;
  align?: "start" | "center" | "end" | "stretch";
  justify?: "start" | "center" | "end" | "between";
  wrap?: boolean;
  children: ReactNode;
}

export interface GridProps {
  columns?: ResponsiveColumns;
  gap?: SpaceSize;
  children: ReactNode;
}

export interface GridItemProps { span?: ResponsiveColumns; children: ReactNode; }

export interface PanelProps {
  title?: string;
  children: ReactNode;
}

export interface ButtonProps {
  children: ReactNode;
  onClick?(): void;
  disabled?: boolean;
  loading?: boolean;
  kind?: "primary" | "secondary" | "danger" | "text";
}

export interface BreadcrumbItem { id: string; label: string; href?: string; onSelect?(): void; }
export interface TabItem { id: string; label: ReactNode; content: ReactNode; disabled?: boolean; }

export interface DialogProps {
  open: boolean;
  title: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  width?: "sm" | "md" | "lg";
  onClose(): void;
}

export interface DrawerProps extends DialogProps { placement?: "top" | "right" | "bottom" | "left"; }

export interface CommandItem {
  id: string;
  title: string;
  description?: string;
  keywords?: string[];
  disabled?: boolean;
  run(): void;
}

export interface TableColumn {
  key: string;
  title: ReactNode;
  width?: number;
  render?(value: unknown, row: Readonly<Record<string, unknown>>, index: number): ReactNode;
}

export interface TableProps {
  columns: TableColumn[];
  rows: ReadonlyArray<Readonly<Record<string, unknown>>>;
  rowKey?: string | ((row: Readonly<Record<string, unknown>>) => string);
  loading?: boolean;
  empty?: ReactNode;
}

export interface DescriptionItem { id: string; label: ReactNode; value: ReactNode; }
export type StatusTone = "neutral" | "info" | "success" | "warning" | "error";
export const semanticIconNames = Object.freeze(["add", "remove", "edit", "search", "settings", "success", "warning", "error", "info", "close", "menu"] as const);
export type SemanticIconName = (typeof semanticIconNames)[number];
export interface SemanticThemeTokens {
  color: { canvas: string; surface: string; text: string; mutedText: string; border: string; primary: string; danger: string; };
  radius: { sm: number; md: number; lg: number; };
  spacing: Record<SpaceSize, number>;
}

export interface PortalUI {
  PortalShell: ComponentType<PortalShellProps>;
  Page: ComponentType<PageProps>;
  Panel: ComponentType<PanelProps>;
  Stack: ComponentType<StackProps>;
  Grid: ComponentType<GridProps>;
  GridItem: ComponentType<GridItemProps>;
  Divider: ComponentType<{ label?: ReactNode }>;
  Button: ComponentType<ButtonProps>;
  Menu: ComponentType<{ items: MenuItem[]; activeID?: string; onSelect?(id: string): void }>;
  Breadcrumb: ComponentType<{ items: BreadcrumbItem[] }>;
  Tabs: ComponentType<{ items: TabItem[]; activeID?: string; onChange?(id: string): void }>;
  CommandPalette: ComponentType<{ open: boolean; commands: CommandItem[]; query: string; onQueryChange(query: string): void; onClose(): void }>;
  Dialog: ComponentType<DialogProps>;
  Drawer: ComponentType<DrawerProps>;
  FormRenderer: ComponentType<FormRendererProps>;
  FilterBar: ComponentType<{ children: ReactNode; actions?: ReactNode }>;
  Table: ComponentType<TableProps>;
  Pagination: ComponentType<{ page: number; pageSize: number; total: number; disabled?: boolean; onChange(page: number, pageSize: number): void }>;
  Descriptions: ComponentType<{ title?: ReactNode; items: DescriptionItem[]; columns?: ResponsiveColumns }>;
  Status: ComponentType<{ tone?: StatusTone; children: ReactNode }>;
  Icon: ComponentType<{ name: SemanticIconName; label?: string; size?: "sm" | "md" | "lg" }>;
  theme: { mode: "light" | "dark" | "system"; tokens: SemanticThemeTokens };
  EmptyState: ComponentType<{ title: string; description?: string }>;
  ErrorState: ComponentType<{ title: string; retry?(): void }>;
  Skeleton: ComponentType<{ rows?: number }>;
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

export type NavigationZone = "primary" | "settings" | "secondary";

export const shellSlotIDs = Object.freeze([
  "shell.header.start", "shell.header.center", "shell.header.end",
  "shell.navigation.start", "shell.navigation.center", "shell.navigation.end",
  "shell.footer",
] as const);
export type ShellSlotID = (typeof shellSlotIDs)[number];

export const pageSlotIDs = Object.freeze([
  "page.header.start", "page.header.center", "page.header.end",
  "page.body.before", "page.body.main", "page.body.after", "page.aside",
] as const);
export type PageSlotID = (typeof pageSlotIDs)[number];

export type PortalSlotID = ShellSlotID | PageSlotID;

export interface PortalNavigationGroupDescriptor {
  id: string;
  label: string;
  zone: NavigationZone;
  icon: SemanticIconName;
  order?: number;
}

export interface PortalPageNavigation {
  id: string;
  label: string;
  zone: NavigationZone;
  /** References a group governed by the selected Shell composition. */
  groupID?: string;
  order?: number;
}

export interface PortalNavigationGroup extends PortalNavigationGroupDescriptor {
  pages: readonly PortalPageNavigation[];
}

export interface PortalSlotContribution<Slot extends PortalSlotID = PortalSlotID> {
  id: string;
  slot: Slot;
  component: ComponentType;
  order?: number;
}

export type PortalShellContribution = PortalSlotContribution<ShellSlotID>;
export type PortalPageSlotContribution = PortalSlotContribution<PageSlotID>;

export interface PortalPageDefinition {
  id: string;
  /** Portal-relative path. The trusted host mounts it below PortalSpec.route. */
  path: string;
  title: string;
  description?: string;
  navigation?: PortalPageNavigation;
  slots: readonly PortalPageSlotContribution[];
}

export interface PortalManagementCapability {
  capability: string;
  read?: readonly string[];
  write?: readonly string[];
}

export interface PortalManagementService {
  id: string;
  label?: string;
  logicalService: string;
  routingDomain: string;
  capabilities: readonly PortalManagementCapability[];
}

export interface PortalPluginRuntime {
  revision: number;
  id: string;
  tenantId: string;
  route: string;
  management: { services: readonly PortalManagementService[] };
}

export interface FrontendPluginContext {
	readonly portal: Readonly<PortalPluginRuntime>;
	/** Host-owned scope. Long-lived work must stop when this signal is aborted. */
	readonly lifecycle: Readonly<FrontendPluginLifecycleContext>;
	addPage(page: PortalPageDefinition): void;
	/** Platform-profile plugins only; application plugins cannot mutate global Shell regions. */
	addShellContribution(contribution: PortalShellContribution): void;
}

export interface FrontendPluginLifecycleContext {
  readonly pluginID: string;
  readonly generation: string;
  readonly signal: AbortSignal;
  readonly reason: "bootstrap" | "replace" | "shutdown";
}

/** Optional first-party lifecycle used by transactional Portal Generation swaps. */
export interface FrontendPluginHotLifecycle {
  capture?(context: Readonly<FrontendPluginLifecycleContext>): JSONValue | undefined | Promise<JSONValue | undefined>;
  restore?(state: JSONValue | undefined, context: Readonly<FrontendPluginLifecycleContext>): void | Promise<void>;
  dispose?(context: Readonly<FrontendPluginLifecycleContext>): void | Promise<void>;
}

export function managementServicesFor(portal: Readonly<PortalPluginRuntime>, capability: string): readonly PortalManagementService[] {
  return portal.management.services.filter((service) => service.capabilities.some((grant) => grant.capability === capability));
}

export function requireManagementService(portal: Readonly<PortalPluginRuntime>, capability: string): PortalManagementService {
  const matches = managementServicesFor(portal, capability);
  if (matches.length !== 1) {
    throw new Error(`Portal 必须为 ${capability} 精确绑定一个管理服务，当前为 ${matches.length} 个`);
  }
  return matches[0];
}

export interface PortalRegisteredPage extends PortalPageDefinition {
  pluginID: string;
}

export interface PortalRegisteredShellContribution extends PortalShellContribution {
  pluginID: string;
}

export interface ShellCompositionInput {
  pages: readonly PortalRegisteredPage[];
  shellContributions: readonly PortalRegisteredShellContribution[];
  activePageID?: string;
  config?: Readonly<Record<string, unknown>>;
}

export interface ShellCompositionModel {
  pages: readonly PortalRegisteredPage[];
  activePage?: PortalRegisteredPage;
  navigation: Readonly<Record<NavigationZone, readonly PortalNavigationGroup[]>>;
  shellSlots: Readonly<Partial<Record<ShellSlotID, readonly PortalRegisteredShellContribution[]>>>;
  pageSlots: Readonly<Partial<Record<PageSlotID, readonly PortalPageSlotContribution[]>>>;
}

/** Owns the stable shell/page topology, slot validation and deterministic order. */
export interface ShellCompositionAdapter {
  id: "ui.shell-composition";
  uiContract: string;
  compose(input: ShellCompositionInput): ShellCompositionModel;
}

export interface ShellBranding {
  name: string;
  logoURL?: string;
  shortName?: string;
}

export interface ShellLayoutProps {
  composition: ShellCompositionModel;
  branding: ShellBranding;
  config: Readonly<Record<string, unknown>>;
  pathname: string;
  recoveryNotice?: ReactNode;
  onNavigate(pageID: string): void;
}

/** Applies visual arrangement only; slot names and content come from composition. */
export interface ShellLayoutAdapter {
  id: "ui.shell-layout";
  uiContract: string;
  Shell: ComponentType<ShellLayoutProps>;
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
