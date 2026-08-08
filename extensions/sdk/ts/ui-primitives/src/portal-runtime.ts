import type { ComponentType, ReactNode } from "react";
import type { IconGlyphDefinition } from "@vastplan/icon-catalog/semantic";
import type { CollectionPreference } from "@vastplan/frontend-engine-contract";
import type { PluginExtensionAccess } from "@vastplan/plugin-extension-contract";
import type { DashboardGridLayouts, DashboardGridSpec, JSONValue, LocalizedText, MessageDescriptor, MessageValues, PageBodyLayout, PluginLocalization } from "@vastplan/ui-contract";
import type { CollectionPageDefinition, FormPageDefinition, PageActionHostDefinition, RecordPageDefinition, WorkspacePageDefinition } from "@vastplan/workbench-sdk";
import type { SemanticIconName } from "./icon.js";

export type NavigationZone = "primary" | "settings" | "secondary";

/** The authenticated account avatar is the only Shell-owned navigation anchor. */
export const hostNavigationPluginID = "vastplan.host" as const;
export const accountNavigationGroupID = "account" as const;
export const accountNavigationNodeID = `${hostNavigationPluginID}/${accountNavigationGroupID}` as const;
export const accountNavigationNodeRef: PortalNavigationNodeRef = Object.freeze({ pluginID: hostNavigationPluginID, nodeID: accountNavigationGroupID });
export const accountPageExtensionPointID = "cn.vastplan.foundation.frontend.identity.account-center.page" as const;

export interface PortalNavigationNodeRef {
  pluginID: string;
  nodeID: string;
}

export type PortalNavigationIconState = "normal" | "active" | "loading" | "error";
export type PortalNavigationIconMotion = "none" | "pulse" | "spin" | "draw";
export type PortalNavigationIconSpec =
  | { kind: "semantic"; name: SemanticIconName }
  | { kind: "custom"; pluginID: string; name: string; states: Readonly<Partial<Record<PortalNavigationIconState, IconGlyphDefinition>>> & { normal: IconGlyphDefinition }; motion: PortalNavigationIconMotion };

export type PortalNavigationPresentationIcon =
  | PortalNavigationIconSpec
  | { kind: "composite"; items: readonly PortalNavigationIconSpec[] };

export interface PortalNavigationParentRef extends PortalNavigationNodeRef {
  mode: "required" | "optional";
  fallback?: PortalNavigationNodeRef;
}

export interface PortalNavigationCatalogNode {
  id: string;
  ref: PortalNavigationNodeRef;
  label: LocalizedText;
  zone: NavigationZone;
  icon: PortalNavigationIconSpec;
  parent?: PortalNavigationParentRef;
  order?: number;
}

export interface PortalNavigationCatalog {
  pluginID: string;
  nodes: readonly PortalNavigationCatalogNode[];
}

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
  icon: PortalNavigationIconSpec;
  /** Portal-owned localized label overrides keyed by canonical locale. */
  labels?: Readonly<Record<string, string>>;
  /** Hides this node from navigation without changing page authorization. */
  hidden?: boolean;
  /** Child groups reference a root group in the same zone. Omitted for roots. */
  parentID?: string;
  order?: number;
}

export interface PortalPageNavigation {
  id: string;
  label: LocalizedText;
  /** Owner-bound reference to a plugin menu node or a trusted host anchor. */
  parentMenuRef: PortalNavigationNodeRef;
  /** Host-bound management service scope used only for service-owned presentation policies. */
  managementServiceID?: string;
  order?: number;
}

export interface PortalResolvedPageNavigation extends PortalPageNavigation {
  zone: NavigationZone;
  groupID: string;
}

export interface PortalNavigationChildGroup extends PortalNavigationGroupDescriptor {
  parentID: string;
  pages: readonly PortalResolvedPageNavigation[];
}

export interface PortalNavigationGroup extends PortalNavigationGroupDescriptor {
  parentID?: undefined;
  pages: readonly PortalResolvedPageNavigation[];
  children: readonly PortalNavigationChildGroup[];
}

/** A layout entry. Folders collect root slices without becoming navigation parents. */
export interface PortalNavigationCollection {
  kind: "group" | "folder";
  id: string;
  label: LocalizedText;
  labels?: Readonly<Record<string, string>>;
  zone: NavigationZone;
  icon: PortalNavigationPresentationIcon;
  order?: number;
  groups: readonly PortalNavigationGroup[];
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
  /** Semantic content width. The Shell owns centering and the executable width recipe; omitted pages default to large. */
  bodyLayout?: PageBodyLayout;
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

export interface PortalManagedResource {
  kind: "service-unit";
  kernel: "backend";
  deployment: string;
  unitId: string;
}

export interface PortalManagementService {
  id: string;
  label?: string;
  logicalService: string;
  routingDomain: string;
  resource?: PortalManagedResource;
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
		/** Signed, Generation-scoped plugin-extension graph visible to this plugin. */
		readonly extensions: PluginExtensionAccess;
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
  navigationCatalogs: readonly PortalNavigationCatalog[];
  activePageID?: string;
  config?: Readonly<Record<string, unknown>>;
}

export interface ShellCompositionModel {
  pages: readonly PortalRegisteredPage[];
  activePage?: PortalRegisteredPage;
  activeNavigationPath?: ActiveNavigationPath;
  activeNavigationCollectionID?: string;
  navigation: Readonly<Record<NavigationZone, readonly PortalNavigationGroup[]>>;
  navigationCollections: Readonly<Record<NavigationZone, readonly PortalNavigationCollection[]>>;
  shellSlots: Readonly<Partial<Record<ShellSlotID, readonly PortalRegisteredShellContribution[]>>>;
  pageSlots: Readonly<Partial<Record<PageSlotID, readonly PortalPageSlotContribution[]>>>;
}
