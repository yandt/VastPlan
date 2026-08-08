import { createElement } from "react";
import { message, pageBodyLayouts, semanticIconNames } from "@vastplan/ui-primitives";
import { defineCollectionPage, defineMasterDetailPage, defineRecordDetailPage, defineTreeDetailPage, defineWorkspacePage } from "@vastplan/workbench-sdk";
import type {
  FrontendPluginContext,
  PluginLocalization,
  PortalRegisteredPage,
  PortalRegisteredShellContribution,
} from "@vastplan/ui-primitives";
import { contractSatisfies } from "./contract-version";
import { PortalAssemblyError } from "./portal-errors";
import type {
  FrontendPluginLoader,
  PluginRef,
  PortalPrepareOptions,
  PortalSpec,
  PreparedPortal,
} from "./portal-contracts";
import { loadPortalFoundations } from "./portal-foundations";
import { PageHelpButton } from "./page-help-button";
import { navigationCatalogsFromIndex } from "./navigation-contributions";
import { createPageRefreshController } from "./page-refresh-controller";
import { createPluginExtensionAccess, emptyPortalExtensionGraph, validateExtensionGraphForPortal, validateFrontendPageExtensions } from "./plugin-extensions";
import { snapshotPortal } from "./portal-snapshot";
import {
  assertTrustedFirstParty,
  managementName,
  mergeLocalization,
  moduleKey,
  mountPortalPagePath,
  requiredModule,
  samePlugin,
  standardPageSlots,
  standardShellSlots,
  validateLocalization,
  validatePortalShape,
  validLocalizedText,
} from "./portal-validation";

export { contractSatisfies } from "./contract-version";
export { PortalAssemblyError } from "./portal-errors";
export type { RuntimeEngineSelection } from "./runtime-engine";
export type * from "./portal-contracts";

/**
 * Browser assembly security boundary. Foundation loading, structural validation,
 * immutable snapshots and feature registration live in separate modules so this
 * class only coordinates one prepare workflow.
 */
export class PortalRuntime {
  public constructor(private readonly loader: FrontendPluginLoader) {}

  public async prepare(portal: PortalSpec, options: PortalPrepareOptions = {}): Promise<PreparedPortal> {
    try {
      return await this.assemble(portal, options);
    } catch (error) {
      this.releaseLoader();
      throw error;
    }
  }

  private async assemble(portal: PortalSpec, options: PortalPrepareOptions): Promise<PreparedPortal> {
    validatePortalShape(portal);
    const extensionGraph = options.extensions ?? emptyPortalExtensionGraph;
    validateExtensionGraphForPortal(extensionGraph, portal.plugins);
    const foundations = await loadPortalFoundations(this.loader, portal, options);
    const modules = new Map(foundations.loaded.map((item) => [moduleKey(item.ref), item.module]));
    // A development source catalog may reference a newer SemVer while the
    // active RuntimeSpec remains the canonical lookup identity. Only the
    // trusted loader can opt into this alias; production loaders keep exact keys.
    for (const { ref, module } of foundations.loaded) {
      if (this.loader.canLoad?.(ref) !== true) continue;
      const active = portal.plugins.find((candidate) => candidate.id === ref.id);
      if (active !== undefined) modules.set(moduleKey(active), module);
    }
    const workbenchModule = requiredModule(modules, portal.workbench);
    assertTrustedFirstParty(workbenchModule, portal.workbench.id);
    const workbench = workbenchModule.workbench;
    if (workbench?.id !== "ui.workflow.workbench" || typeof workbench.CollectionPage !== "function" || typeof workbench.WorkspacePage !== "function" || typeof workbench.PageActionHost !== "function" || typeof workbench.RecordPage !== "function" || !contractSatisfies(workbench.uiContract, portal.workbench.uiContract)) {
      throw new PortalAssemblyError("WORKBENCH_INVALID", "UI Workbench 插件缺失或 UI 契约不兼容");
    }

    const messageCatalogs = collectLocalization(portal, foundations.loaded, foundations.renderer);
    const navigationCatalogs = navigationCatalogsFromIndex(options.contributions);
    const portalSnapshot = snapshotPortal(portal);
    const registration = createRegistrationState();
    const generation = options.generation ?? `portal-${portal.revision}`;
    const signal = options.signal ?? new AbortController().signal;
    const reason = options.reason ?? "bootstrap";

    for (const ref of portal.plugins) {
      if (isFoundationOrDeferred(ref, portal, foundations.rendererModuleIDs, foundations.shellLibraryModuleIDs)) continue;
      const plugin = requiredModule(modules, ref);
      assertTrustedFirstParty(plugin, ref.id);
      if (plugin.runtimeEngine !== undefined || plugin.renderAdapter !== undefined || plugin.shell !== undefined || plugin.workbench !== undefined) {
        throw new PortalAssemblyError("SECOND_SHELL_FOUNDATION", "功能插件不能注册第二个 Runtime Engine、设计系统、Shell 或 Workbench");
      }
      const context = createPluginContext({
        portal,
        portalSnapshot,
        ref,
        generation,
        signal,
        reason,
        workbench,
        preferences: options.preferences,
        registration,
        extensionGraph,
      });
      await plugin.register?.(context);
    }
    validateFrontendPageExtensions(registration.pages, extensionGraph);

    const preparedModules = foundations.loaded.map(({ ref, module }) => Object.freeze({ ref: Object.freeze({ ...ref }), module }));
    return Object.freeze({
      portal: portalSnapshot,
      runtimeEngine: foundations.runtimeEngine,
      renderAdapter: foundations.renderer,
      themeTemplateID: foundations.themeTemplateID,
      iconThemeID: foundations.iconThemeID,
      renderAdapterCatalog: foundations.renderAdapterCatalog,
      shell: foundations.shell,
      shellLibrary: foundations.shellLibrary,
      workbench,
      pages: Object.freeze(registration.pages),
      navigationCatalogs,
      shellContributions: Object.freeze(registration.shellContributions),
      modules: Object.freeze(preparedModules),
      messageCatalogs: Object.freeze(messageCatalogs),
      release: () => this.releaseLoader(),
    });
  }

  private releaseLoader(): void {
    this.loader.dispose?.();
  }
}

interface RegistrationState {
  pages: PortalRegisteredPage[];
  shellContributions: PortalRegisteredShellContribution[];
  pageIDs: Set<string>;
  paths: Set<string>;
  navigationIDs: Set<string>;
  slotIDs: Set<string>;
  shellContributionIDs: Set<string>;
}

function createRegistrationState(): RegistrationState {
  return {
    pages: [],
    shellContributions: [],
    pageIDs: new Set(),
    paths: new Set(),
    navigationIDs: new Set(),
    slotIDs: new Set(),
    shellContributionIDs: new Set(),
  };
}

interface ContextInput {
  portal: PortalSpec;
  portalSnapshot: Readonly<PortalSpec>;
  ref: PluginRef;
  generation: string;
  signal: AbortSignal;
  reason: "bootstrap" | "replace";
  workbench: NonNullable<ReturnType<typeof requiredModule>["workbench"]>;
  preferences?: import("@vastplan/ui-primitives").WorkbenchPreferencePort;
  registration: RegistrationState;
  extensionGraph: import("@vastplan/plugin-extension-contract").PortalExtensionGraph;
}

function createPluginContext(input: ContextInput): FrontendPluginContext {
  const { portal, portalSnapshot, ref, generation, signal, reason, workbench, preferences, registration, extensionGraph } = input;
  const context: FrontendPluginContext = {
    portal: portalSnapshot,
    lifecycle: Object.freeze({ pluginID: ref.id, generation, signal, reason }),
    i18n: Object.freeze({ message: (key, fallback, values) => message(ref.id, key, fallback, values) }),
    extensions: createPluginExtensionAccess(extensionGraph, ref.id),
    addShellContribution: (contribution) => {
      const key = `${ref.id}/${contribution.id}`;
      if (portal.resolution.pluginOrigins[ref.id] !== "platform-profile") {
        throw new PortalAssemblyError("SHELL_CONTRIBUTION_ORIGIN", `应用插件不能贡献全局 Shell 区域: ${ref.id}`);
      }
      if (!managementName(contribution.id) || !standardShellSlots.has(contribution.slot) || typeof contribution.component !== "function" || registration.shellContributionIDs.has(key)) {
        throw new PortalAssemblyError("SHELL_CONTRIBUTION_REJECTED", `Shell 贡献非法或重复: ${key}`);
      }
      registration.shellContributionIDs.add(key);
      registration.shellContributions.push({ ...contribution, pluginID: ref.id });
    },
    addPage: (page) => registerPage(portal, ref, registration, page),
    addCollectionPage: (page) => {
			if (!experienceAllows(portal, page.requiredPermissions) || !experienceAllowsAny(portal, page.requiredAnyPermissions)) return;
			const projectedPage = projectCollectionActions(portal, validateCollectionPage(page));
      if (!projectedPage.id || !projectedPage.collection.id || !["table", "cards"].includes(projectedPage.collection.view) || !["page", "cursor"].includes(projectedPage.collection.query.mode) ||
          (projectedPage.collection.view === "table" && projectedPage.collection.columns.length === 0) || (projectedPage.collection.view === "cards" && projectedPage.collection.card === undefined) || typeof projectedPage.load !== "function" ||
          (projectedPage.loadSummary !== undefined && typeof projectedPage.loadSummary !== "function") || (projectedPage.runAction !== undefined && typeof projectedPage.runAction !== "function") ||
          (projectedPage.overlays ?? []).some((overlay) => !overlay.id || !["dialog", "drawer"].includes(overlay.surface) || typeof overlay.load !== "function") ||
          [...(projectedPage.collection.actions ?? []), ...(projectedPage.pageActions ?? [])].some((action) => action.icon === undefined || !semanticIconNames.includes(action.icon))) {
        throw new PortalAssemblyError("WORKBENCH_PAGE_REJECTED", `集合页面定义无效: ${projectedPage.id}`);
      }
      const refresh = createPageRefreshController();
      const Page = () => createElement(workbench.CollectionPage, { page: projectedPage, preferenceScope: `${portal.tenantId}/${portal.id}`, preferences, presentation: portal.workbench.config, refreshSignal: refresh });
      const PageActions = () => createElement(workbench.PageActionHost, { definition: pageActionDefinition(projectedPage), onRefresh: refresh.invalidate });
      context.addPage({ id: projectedPage.id, path: projectedPage.path, title: projectedPage.title, description: projectedPage.description, bodyLayout: projectedPage.bodyLayout, navigation: projectedPage.navigation, slots: [
        ...((projectedPage.pageActions?.length ?? 0) === 0 ? [] : [{ id: "page.actions", slot: "page.header.end" as const, component: PageActions, order: 100 }]),
        { id: "workbench.collection", slot: "page.body.main", component: Page },
      ] });
    },
    addWorkspacePage: (page) => {
      if (!experienceAllows(portal, page.requiredPermissions) || !experienceAllowsAny(portal, page.requiredAnyPermissions)) return;
      const validated = validateWorkspace(page);
      const sections = validated.sections
        .filter((section) => experienceAllows(portal, section.page.requiredPermissions) && experienceAllowsAny(portal, section.page.requiredAnyPermissions))
        .map((section) => ({ ...section, page: projectCollectionActions(portal, section.page) }));
      if (sections.length === 0) return;
      const projected = { ...validated, sections };
      const actions = sections.flatMap((section) => [...(section.page.collection.actions ?? []), ...(section.page.pageActions ?? [])]);
      if (actions.some((action) => action.icon === undefined || !semanticIconNames.includes(action.icon))) {
        throw new PortalAssemblyError("WORKBENCH_PAGE_REJECTED", `Workspace 页面动作定义无效: ${projected.id}`);
      }
      const refresh = createPageRefreshController();
      const Page = () => createElement(workbench.WorkspacePage, { page: projected, preferenceScope: `${portal.tenantId}/${portal.id}`, preferences, presentation: portal.workbench.config, refreshSignal: refresh });
      const definition = workspacePageActionDefinition(projected);
      const PageActions = () => createElement(workbench.PageActionHost, { definition, onRefresh: refresh.invalidate });
      context.addPage({ id: projected.id, path: projected.path, title: projected.title, description: projected.description, bodyLayout: projected.bodyLayout, navigation: projected.navigation, slots: [
        ...(definition.actions.length === 0 ? [] : [{ id: "page.actions", slot: "page.header.end" as const, component: PageActions, order: 100 }]),
        { id: "workbench.workspace", slot: "page.body.main", component: Page },
      ] });
    },
    addFormPage: (page) => {
      if (!experienceAllows(portal, page.requiredPermissions) || !experienceAllowsAny(portal, page.requiredAnyPermissions)) return;
      if (!page.id || !page.form?.id || page.form.workflow.surface !== "page" || typeof page.form.submit !== "function") {
        throw new PortalAssemblyError("WORKBENCH_PAGE_REJECTED", `表单页面定义无效: ${page.id}`);
      }
      const projected = projectPageActions(portal, page);
      const refresh = createPageRefreshController();
      const Page = () => createElement(workbench.FormPage, { page: projected });
      const PageActions = () => createElement(workbench.PageActionHost, { definition: pageActionDefinition(projected), onRefresh: refresh.invalidate });
      context.addPage({ id: projected.id, path: projected.path, title: projected.title, description: projected.description, bodyLayout: projected.bodyLayout, navigation: projected.navigation, slots: [
        ...((projected.pageActions?.length ?? 0) === 0 ? [] : [{ id: "page.actions", slot: "page.header.end" as const, component: PageActions, order: 100 }]),
        { id: "workbench.form", slot: "page.body.main", component: Page },
      ] });
    },
    addRecordPage: (page) => {
      if (!experienceAllows(portal, page.requiredPermissions) || !experienceAllowsAny(portal, page.requiredAnyPermissions)) return;
      const projected = projectRecordActions(portal, validateRecordPage(page));
      const refresh = createPageRefreshController();
      const Page = () => createElement(workbench.RecordPage, { page: projected, refreshSignal: refresh });
      const PageActions = () => createElement(workbench.PageActionHost, { definition: pageActionDefinition(projected), onRefresh: refresh.invalidate });
      context.addPage({ id: projected.id, path: projected.path, title: projected.title, description: projected.description, bodyLayout: projected.bodyLayout, navigation: projected.navigation, slots: [
        ...((projected.pageActions?.length ?? 0) === 0 ? [] : [{ id: "page.actions", slot: "page.header.end" as const, component: PageActions, order: 100 }]),
        { id: "workbench.record", slot: "page.body.main", component: Page },
      ] });
    },
  };
  return context;
}

function experienceAllows(portal: PortalSpec, required: readonly string[] | undefined): boolean {
	if (required === undefined || required.length === 0) return true;
	const granted = new Set(portal.experience?.permissions ?? []);
	return required.every((permission) => granted.has(permission));
}

function experienceAllowsAny(portal: PortalSpec, required: readonly string[] | undefined): boolean {
	if (required === undefined || required.length === 0) return true;
	const granted = new Set(portal.experience?.permissions ?? []);
	return required.some((permission) => granted.has(permission));
}

function projectCollectionActions<Row extends Record<string, unknown>>(portal: PortalSpec, page: import("@vastplan/workbench-sdk").CollectionPageDefinition<Row>): import("@vastplan/workbench-sdk").CollectionPageDefinition<Row> {
  const actions = page.collection.actions?.filter((action) => experienceAllows(portal, action.requiredPermissions));
  return { ...projectPageActions(portal, page), collection: { ...page.collection, ...(actions === undefined ? {} : { actions }) } };
}

function validateCollectionPage<Row extends Record<string, unknown>>(page: import("@vastplan/workbench-sdk").CollectionPageDefinition<Row>): import("@vastplan/workbench-sdk").CollectionPageDefinition<Row> {
  try {
    return defineCollectionPage(page);
  } catch (error) {
    throw new PortalAssemblyError("WORKBENCH_PAGE_REJECTED", error instanceof Error ? error.message : `集合页面定义无效: ${page.id}`);
  }
}

function validateWorkspace(page: import("@vastplan/workbench-sdk").WorkspacePageDefinition): import("@vastplan/workbench-sdk").WorkspacePageDefinition {
  try {
    return defineWorkspacePage(page);
  } catch (error) {
    throw new PortalAssemblyError("WORKBENCH_PAGE_REJECTED", error instanceof Error ? error.message : `Workspace 页面定义无效: ${page.id}`);
  }
}

function validateRecordPage<Row extends Record<string, unknown>>(page: import("@vastplan/workbench-sdk").RecordPageDefinition<Row>): import("@vastplan/workbench-sdk").RecordPageDefinition<Row> {
  try {
    if (page.pattern === "master-detail") return defineMasterDetailPage(page);
    if (page.pattern === "tree-detail") return defineTreeDetailPage(page);
    return defineRecordDetailPage(page);
  } catch (error) {
    throw new PortalAssemblyError("WORKBENCH_PAGE_REJECTED", error instanceof Error ? error.message : `记录页面定义无效: ${page.id}`);
  }
}

function projectRecordActions<Row extends Record<string, unknown>>(portal: PortalSpec, page: import("@vastplan/workbench-sdk").RecordPageDefinition<Row>): import("@vastplan/workbench-sdk").RecordPageDefinition<Row> {
  const actions = page.actions?.filter((action) => experienceAllows(portal, action.requiredPermissions));
  return { ...projectPageActions(portal, page), ...(actions === undefined ? {} : { actions }) };
}

function projectPageActions<Page extends { pageActions?: readonly import("@vastplan/ui-contract").PageActionSpec[] }>(portal: PortalSpec, page: Page): Page {
  const pageActions = page.pageActions?.filter((action) => experienceAllows(portal, action.requiredPermissions));
  return { ...page, ...(pageActions === undefined ? {} : { pageActions }) };
}

function pageActionDefinition(page: import("@vastplan/workbench-sdk").CollectionPageDefinition | import("@vastplan/workbench-sdk").FormPageDefinition | import("@vastplan/workbench-sdk").RecordPageDefinition): import("@vastplan/workbench-sdk").PageActionHostDefinition {
  const workflows = page as typeof page & { forms?: import("@vastplan/workbench-sdk").WorkbenchFormDefinition[]; overlays?: import("@vastplan/workbench-sdk").WorkbenchOverlayDefinition[] };
  return Object.freeze({ id: page.id, actions: page.pageActions ?? [], ...(workflows.forms === undefined ? {} : { forms: workflows.forms }), ...(workflows.overlays === undefined ? {} : { overlays: workflows.overlays }), ...(page.runPageAction === undefined ? {} : { runAction: page.runPageAction }) });
}

function workspacePageActionDefinition(page: import("@vastplan/workbench-sdk").WorkspacePageDefinition): import("@vastplan/workbench-sdk").PageActionHostDefinition {
  const actions = page.sections.flatMap((section) => section.page.pageActions ?? []);
  const formIDs = new Set(actions.flatMap((action) => action.form === undefined ? [] : [action.form]));
  const overlayIDs = new Set(actions.flatMap((action) => action.overlay === undefined ? [] : [action.overlay]));
  const forms = page.sections.flatMap((section) => section.page.forms?.filter((form) => formIDs.has(form.id)) ?? []);
  const overlays = page.sections.flatMap((section) => section.page.overlays?.filter((overlay) => overlayIDs.has(overlay.id)) ?? []);
  const runAction = async (context: import("@vastplan/workbench-sdk").PageActionContext, signal: AbortSignal) => {
    const section = page.sections.find((candidate) => candidate.page.pageActions?.some((action) => action.id === context.action.id));
    return section?.page.runPageAction?.(context, signal);
  };
  return Object.freeze({ id: page.id, actions: Object.freeze(actions), ...(forms.length === 0 ? {} : { forms: Object.freeze(forms) }), ...(overlays.length === 0 ? {} : { overlays: Object.freeze(overlays) }), ...(actions.some((action) => action.form === undefined && action.overlay === undefined) ? { runAction } : {}) });
}

function registerPage(
  portal: PortalSpec,
  ref: PluginRef,
  state: RegistrationState,
  page: Parameters<FrontendPluginContext["addPage"]>[0],
): void {
  const mountedPath = mountPortalPagePath(portal.route, page.path);
  if (!page.id || mountedPath === undefined || !validLocalizedText(page.title) || (page.description !== undefined && !validLocalizedText(page.description)) || state.pageIDs.has(page.id) || state.paths.has(mountedPath) || !Array.isArray(page.slots)) {
    throw new PortalAssemblyError("PAGE_REJECTED", `页面 ID/路径非法或重复: ${page.id || page.path}`);
  }
  if (page.bodyLayout !== undefined && !pageBodyLayouts.includes(page.bodyLayout)) {
    throw new PortalAssemblyError("PAGE_BODY_LAYOUT_REJECTED", `页面 bodyLayout 无效: ${page.id}/${String(page.bodyLayout)}`);
  }
  if (!page.slots.some((slot) => slot.slot === "page.body.main")) throw new PortalAssemblyError("PAGE_MAIN_MISSING", `页面必须填充 page.body.main: ${page.id}`);
  if (page.navigation !== undefined && (!managementName(page.navigation.id) || !validLocalizedText(page.navigation.label) || state.navigationIDs.has(page.navigation.id) ||
      !managementName(page.navigation.parentMenuRef?.pluginID) || !managementName(page.navigation.parentMenuRef?.nodeID) ||
      (page.navigation.managementServiceID !== undefined && !portal.management.services.some((service) => service.id === page.navigation!.managementServiceID)))) {
    throw new PortalAssemblyError("NAVIGATION_REJECTED", `导航 ID 重复或语义区无效: ${page.navigation.id}`);
  }
  const slots = [...page.slots, { id: "system.page.help", slot: "page.header.end" as const, component: PageHelpButton, order: 1_000_000 }];
  for (const slot of slots) {
    const slotKey = `${page.id}/${slot.id}`;
    if (!slot.id || !standardPageSlots.has(slot.slot) || state.slotIDs.has(slotKey) || typeof slot.component !== "function") {
      throw new PortalAssemblyError("SLOT_REJECTED", `Slot 贡献非法或重复: ${slotKey}`);
    }
    state.slotIDs.add(slotKey);
  }
  state.pageIDs.add(page.id);
  state.paths.add(mountedPath);
  if (page.navigation !== undefined) state.navigationIDs.add(page.navigation.id);
  state.pages.push({ ...page, path: mountedPath, slots, pluginID: ref.id });
}

function collectLocalization(
  portal: PortalSpec,
  loaded: readonly { ref: PluginRef; module: ReturnType<typeof requiredModule> }[],
  renderer: NonNullable<ReturnType<typeof requiredModule>["renderer"]>,
): Record<string, PluginLocalization> {
  const catalogs: Record<string, PluginLocalization> = {};
  for (const { ref, module } of loaded) {
    if (module.localization === undefined) throw new PortalAssemblyError("LOCALIZATION_REQUIRED", `UI 插件必须声明语言资源: ${ref.id}`);
    const localization = validateLocalization(ref.id, module.localization);
    if (module.provenance.firstParty && (!Object.hasOwn(localization.messages, "zh-CN") || !Object.hasOwn(localization.messages, "en-US"))) {
      throw new PortalAssemblyError("LOCALIZATION_FIRST_PARTY_INCOMPLETE", `第一方 UI 插件必须包含 zh-CN 与 en-US: ${ref.id}`);
    }
    catalogs[ref.id] = localization;
  }
  if (renderer.localization === undefined) throw new PortalAssemblyError("LOCALIZATION_REQUIRED", `Renderer 必须声明语言资源: ${renderer.id}`);
  const rendererLocalization = validateLocalization(renderer.id, renderer.localization);
  const adapterLocalization = catalogs[portal.renderAdapter.id];
  catalogs[portal.renderAdapter.id] = adapterLocalization === undefined ? rendererLocalization : mergeLocalization(adapterLocalization, rendererLocalization);
  return catalogs;
}

function isFoundationOrDeferred(
  ref: PluginRef,
  portal: PortalSpec,
  rendererModuleIDs: ReadonlySet<string>,
  shellLibraryModuleIDs: ReadonlySet<string>,
): boolean {
  return [portal.runtimeEngine, portal.renderAdapter, portal.shell, portal.workbench].some((foundation) => samePlugin(ref, foundation)) ||
    rendererModuleIDs.has(ref.id) || shellLibraryModuleIDs.has(ref.id);
}
