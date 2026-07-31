import type { ComponentType, ReactNode } from "react";
import type { CollectionPreference } from "@vastplan/frontend-engine-contract";
import type { DashboardGridLayouts, DashboardGridSpec, JSONValue, LocalizedText, MessageDescriptor, MessageValues, PluginLocalization } from "@vastplan/ui-contract";
import type { CollectionPageDefinition, FormPageDefinition, PageActionHostDefinition, RecordPageDefinition, WorkspacePageDefinition } from "@vastplan/workbench-sdk";
import type { SemanticIconName } from "./icon.js";

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
  label: LocalizedText;
  zone: NavigationZone;
  icon: SemanticIconName;
  /** Child groups reference a root group in the same zone. Omitted for roots. */
  parentID?: string;
  order?: number;
}

export interface PortalPageNavigation {
  id: string;
  label: LocalizedText;
  zone: NavigationZone;
  /** References a group governed by the selected Shell composition. */
  groupID?: string;
  order?: number;
}

export interface PortalNavigationChildGroup extends PortalNavigationGroupDescriptor {
  parentID: string;
  pages: readonly PortalPageNavigation[];
}

export interface PortalNavigationGroup extends PortalNavigationGroupDescriptor {
  parentID?: undefined;
  pages: readonly PortalPageNavigation[];
  children: readonly PortalNavigationChildGroup[];
}

export interface ActiveNavigationPath {
  zone: NavigationZone;
  rootGroupID: string;
  childGroupID?: string;
  pageID: string;
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
  title: LocalizedText;
  description?: LocalizedText;
  navigation?: PortalPageNavigation;
  slots: readonly PortalPageSlotContribution[];
}

export interface PortalManagementCapability {
  capability: string;
  read?: readonly string[];
  write?: readonly string[];
}

export interface PortalManagementAPI {
  id: string;
  contractId: string;
  contractVersion: string;
  contractDigest: string;
}

export interface PortalManagementService {
  id: string;
  label?: string;
  logicalService: string;
  routingDomain: string;
  capabilities: readonly PortalManagementCapability[];
  apis?: readonly PortalManagementAPI[];
}

export interface PortalPluginRuntime {
  revision: number;
  id: string;
  tenantId: string;
  route: string;
  /** Short-lived, session-specific UX projection; never an authorization proof. */
  experience?: { permissions: readonly string[] };
  management: { services: readonly PortalManagementService[] };
}

export interface FrontendPluginContext {
	readonly portal: Readonly<PortalPluginRuntime>;
	/** Host-owned scope. Long-lived work must stop when this signal is aborted. */
	readonly lifecycle: Readonly<FrontendPluginLifecycleContext>;
	readonly i18n: Readonly<{
		message(key: string, fallback: string, values?: MessageValues): MessageDescriptor;
	}>;
	addPage(page: PortalPageDefinition): void;
	/** Registers a governed collection page. Functional plugins should prefer this over addPage. */
	addCollectionPage<Row extends Record<string, unknown>>(page: CollectionPageDefinition<Row>): void;
	addWorkspacePage(page: WorkspacePageDefinition): void;
	addFormPage(page: FormPageDefinition): void;
	addRecordPage<Row extends Record<string, unknown>>(page: RecordPageDefinition<Row>): void;
	/** Platform-profile plugins only; application plugins cannot mutate global Shell regions. */
	addShellContribution(contribution: PortalShellContribution): void;
}

export interface PageRefreshSignal {
  subscribe(listener: () => void): () => void;
  getSnapshot(): number;
}

/** Foundation Workbench runtime; page actions are hosted independently from body patterns. */
export interface UIWorkbenchAdapter {
  id: "ui.workflow.workbench";
  uiContract: string;
  CollectionPage: ComponentType<{ page: CollectionPageDefinition; preferenceScope: string; preferences?: WorkbenchPreferencePort; presentation?: { collection?: { defaultDensity?: "compact" | "standard" | "comfortable"; allowedDensities?: readonly ("compact" | "standard" | "comfortable")[] } }; refreshSignal?: PageRefreshSignal }>;
  WorkspacePage: ComponentType<{ page: WorkspacePageDefinition; preferenceScope: string; preferences?: WorkbenchPreferencePort; presentation?: { collection?: { defaultDensity?: "compact" | "standard" | "comfortable"; allowedDensities?: readonly ("compact" | "standard" | "comfortable")[] } }; refreshSignal?: PageRefreshSignal }>;
  PageActionHost: ComponentType<{ definition: PageActionHostDefinition; onRefresh(): void }>;
  FormPage: ComponentType<{ page: FormPageDefinition }>;
  RecordPage: ComponentType<{ page: RecordPageDefinition; refreshSignal?: PageRefreshSignal }>;
  /** Optional heavyweight dashboard Pattern. Its implementation must stay in a deferred module chunk. */
  loadDashboardGrid?: () => Promise<ComponentType<DashboardGridRuntimeProps>>;
  localization?: PluginLocalization;
}

export interface DashboardGridRuntimeProps {
  spec: DashboardGridSpec;
  /** Trusted host-resolved card content; functional plugins never pass React nodes through the serialized contract. */
  cards: Readonly<Record<string, ReactNode>>;
  layouts?: DashboardGridLayouts;
  editable?: boolean;
  onLayoutChange?(layouts: DashboardGridLayouts): void;
}

/** Narrow user-preference boundary. Workbench never receives identity, transport, or the full Portal preference document. */
export interface WorkbenchPreferencePort {
  readCollection(collectionID: string): CollectionPreference | undefined;
  writeCollection(collectionID: string, preference: CollectionPreference): Promise<CollectionPreference>;
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
  activeNavigationPath?: ActiveNavigationPath;
  navigation: Readonly<Record<NavigationZone, readonly PortalNavigationGroup[]>>;
  shellSlots: Readonly<Partial<Record<ShellSlotID, readonly PortalRegisteredShellContribution[]>>>;
  pageSlots: Readonly<Partial<Record<PageSlotID, readonly PortalPageSlotContribution[]>>>;
}
