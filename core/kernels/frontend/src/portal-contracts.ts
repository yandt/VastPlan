import type { FrontendRuntimeEngine } from "@vastplan/frontend-engine-contract";
import type {
  FrontendPluginContext,
  FrontendPluginHotLifecycle,
  PluginLocalization,
  PortalLocalizationPolicy,
  PortalManagementService,
  PortalMessageCatalogs,
  PortalRegisteredPage,
  PortalRegisteredShellContribution,
  UIRenderAdapter,
  UIRenderer,
  UIShellAdapter,
  UIShellLibrary,
  UIWorkbenchAdapter,
} from "@vastplan/ui-primitives";
import type { RuntimeEngineSelection } from "./runtime-engine";

export interface PluginRef {
  id: string;
  version: string;
  channel?: string;
}

export interface RenderAdapterSelection extends PluginRef {
  uiContract: string;
  config: {
    defaultRenderer: string;
    allowedRenderers: readonly string[];
    userSelectable: boolean;
    rendererOptions?: Readonly<Record<string, { themeTemplate?: string; iconTheme?: string }>>;
  };
}

export interface ShellSelection extends PluginRef {
  uiContract: string;
  config: {
    navigationGroups?: readonly Record<string, unknown>[];
    defaultTemplate: string;
    allowedTemplates: readonly string[];
    userSelectable: boolean;
    templateOptions?: Readonly<Record<string, Readonly<Record<string, unknown>>>>;
  };
}

export interface WorkbenchSelection extends PluginRef {
  uiContract: string;
  config?: {
    collection?: {
      defaultDensity?: "compact" | "standard" | "comfortable";
      allowedDensities?: readonly ("compact" | "standard" | "comfortable")[];
    };
  };
}

export interface CompositionRef {
  id: string;
  revision: number;
  digest: string;
}

export interface PortalResolution {
  platformCatalog: CompositionRef;
  platformProfile: CompositionRef;
  applicationComposition: CompositionRef;
  managementBindingDigest: string;
  pluginOrigins: Readonly<Record<string, "platform-profile" | "application">>;
}

export interface PortalSpec {
  revision: number;
  id: string;
  tenantId: string;
  route: string;
  experience?: { permissions: readonly string[] };
  branding?: Record<string, unknown>;
  localization?: PortalLocalizationPolicy;
  updates?: { mode: "refresh" | "notify" | "automatic" };
  runtimeEngine: RuntimeEngineSelection;
  renderAdapter: RenderAdapterSelection;
  shell: ShellSelection;
  workbench: WorkbenchSelection;
  plugins: readonly PluginRef[];
  management: {
    tenantId: string;
    portalId: string;
    platformProfile: CompositionRef;
    services: readonly PortalManagementService[];
  };
  resolution: PortalResolution;
}

export interface RemoteProvenance {
  signed: boolean;
  firstParty: boolean;
  integrity: string;
}

export interface FrontendPluginModule {
  provenance: RemoteProvenance;
  runtimeEngine?: FrontendRuntimeEngine;
  renderAdapter?: UIRenderAdapter;
  renderer?: UIRenderer;
  shell?: UIShellAdapter;
  shellLibrary?: UIShellLibrary;
  workbench?: UIWorkbenchAdapter;
  register?(context: FrontendPluginContext): void | Promise<void>;
  hot?: FrontendPluginHotLifecycle;
  localization?: PluginLocalization;
}

export interface FrontendPluginLoader {
  load(ref: PluginRef): Promise<FrontendPluginModule>;
  dispose?(): void;
}

export interface PreparedFrontendPlugin {
  ref: Readonly<PluginRef>;
  module: FrontendPluginModule;
}

export interface PreparedPortal {
  portal: Readonly<PortalSpec>;
  runtimeEngine: FrontendRuntimeEngine;
  renderAdapter: UIRenderer;
  renderAdapterCatalog: UIRenderAdapter;
  shell: UIShellAdapter;
  shellLibrary: UIShellLibrary;
  workbench: UIWorkbenchAdapter;
  pages: readonly PortalRegisteredPage[];
  shellContributions: readonly PortalRegisteredShellContribution[];
  modules: readonly PreparedFrontendPlugin[];
  messageCatalogs: PortalMessageCatalogs;
  /** Releases generation-owned Blob URLs and other loader resources. */
  release?(): void;
}

export interface PortalPrepareOptions {
  generation?: string;
  signal?: AbortSignal;
  reason?: "bootstrap" | "replace";
  rendererID?: string;
  shellTemplateID?: string;
}
