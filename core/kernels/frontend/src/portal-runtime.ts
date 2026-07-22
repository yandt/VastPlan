import { createElement } from "react";
import { message, semanticIconNames } from "@vastplan/ui-primitives";
import { defineMasterDetailPage, defineRecordDetailPage, defineTreeDetailPage } from "@vastplan/workbench-sdk";
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
    const foundations = await loadPortalFoundations(this.loader, portal, options);
    const modules = new Map(foundations.loaded.map((item) => [moduleKey(item.ref), item.module]));
    const workbenchModule = requiredModule(modules, portal.workbench);
    assertTrustedFirstParty(workbenchModule, portal.workbench.id);
    const workbench = workbenchModule.workbench;
    if (workbench?.id !== "ui.workflow.workbench" || typeof workbench.CollectionPage !== "function" || typeof workbench.CollectionPageActions !== "function" || typeof workbench.RecordPage !== "function" || typeof workbench.RecordPageActions !== "function" || !contractSatisfies(workbench.uiContract, portal.workbench.uiContract)) {
      throw new PortalAssemblyError("WORKBENCH_INVALID", "UI Workbench 插件缺失或 UI 契约不兼容");
    }

    const messageCatalogs = collectLocalization(portal, foundations.loaded, foundations.renderer);
    const portalSnapshot = snapshotPortal(portal);
    const registration = createRegistrationState();
    const generation = options.generation ?? `portal-${portal.revision}`;
    const signal = options.signal ?? new AbortController().signal;
    const reason = options.reason ?? "bootstrap";

    for (const ref of portal.plugins) {
      if (isFoundationOrDeferred(ref, portal, foundations.rendererModuleKeys, foundations.shellLibraryModuleKeys)) continue;
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
      });
      await plugin.register?.(context);
    }

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
}

function createPluginContext(input: ContextInput): FrontendPluginContext {
  const { portal, portalSnapshot, ref, generation, signal, reason, workbench, preferences, registration } = input;
  const context: FrontendPluginContext = {
    portal: portalSnapshot,
    lifecycle: Object.freeze({ pluginID: ref.id, generation, signal, reason }),
    i18n: Object.freeze({ message: (key, fallback, values) => message(ref.id, key, fallback, values) }),
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
			const projectedPage = projectCollectionActions(portal, page);
      if (!projectedPage.id || !projectedPage.collection.id || !["table", "cards"].includes(projectedPage.collection.view) || !["page", "cursor"].includes(projectedPage.collection.query.mode) ||
          (projectedPage.collection.view === "table" && projectedPage.collection.columns.length === 0) || (projectedPage.collection.view === "cards" && projectedPage.collection.card === undefined) || typeof projectedPage.load !== "function" ||
          (projectedPage.loadSummary !== undefined && typeof projectedPage.loadSummary !== "function") || (projectedPage.runAction !== undefined && typeof projectedPage.runAction !== "function") ||
          (projectedPage.overlays ?? []).some((overlay) => !overlay.id || !["dialog", "drawer"].includes(overlay.surface) || typeof overlay.load !== "function") ||
          (projectedPage.collection.actions ?? []).some((action) => (action.placement === "page.primary" || action.placement === "page.secondary") && (action.icon === undefined || !semanticIconNames.includes(action.icon)))) {
        throw new PortalAssemblyError("WORKBENCH_PAGE_REJECTED", `集合页面定义无效: ${projectedPage.id}`);
      }
      const Page = () => createElement(workbench.CollectionPage, { page: projectedPage, preferenceScope: `${portal.tenantId}/${portal.id}`, preferences, presentation: portal.workbench.config });
      const PageActions = () => createElement(workbench.CollectionPageActions, { page: projectedPage });
      context.addPage({ id: projectedPage.id, path: projectedPage.path, title: projectedPage.title, description: projectedPage.description, navigation: projectedPage.navigation, slots: [
        { id: "workbench.collection.actions", slot: "page.header.end", component: PageActions },
        { id: "workbench.collection", slot: "page.body.main", component: Page },
      ] });
    },
    addFormPage: (page) => {
      if (!experienceAllows(portal, page.requiredPermissions) || !experienceAllowsAny(portal, page.requiredAnyPermissions)) return;
      if (!page.id || !page.form?.id || page.form.workflow.surface !== "page" || typeof page.form.submit !== "function") {
        throw new PortalAssemblyError("WORKBENCH_PAGE_REJECTED", `表单页面定义无效: ${page.id}`);
      }
      const Page = () => createElement(workbench.FormPage, { page });
      context.addPage({ id: page.id, path: page.path, title: page.title, description: page.description, navigation: page.navigation, slots: [{ id: "workbench.form", slot: "page.body.main", component: Page }] });
    },
    addRecordPage: (page) => {
      if (!experienceAllows(portal, page.requiredPermissions) || !experienceAllowsAny(portal, page.requiredAnyPermissions)) return;
      const projected = projectRecordActions(portal, validateRecordPage(page));
      const Page = () => createElement(workbench.RecordPage, { page: projected });
      const PageActions = () => createElement(workbench.RecordPageActions, { page: projected });
      context.addPage({ id: projected.id, path: projected.path, title: projected.title, description: projected.description, navigation: projected.navigation, slots: [
        { id: "workbench.record.actions", slot: "page.header.end", component: PageActions },
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
	return { ...page, collection: { ...page.collection, ...(actions === undefined ? {} : { actions }) } };
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
  return { ...page, ...(actions === undefined ? {} : { actions }) };
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
  if (!page.slots.some((slot) => slot.slot === "page.body.main")) throw new PortalAssemblyError("PAGE_MAIN_MISSING", `页面必须填充 page.body.main: ${page.id}`);
  if (page.navigation !== undefined && (!managementName(page.navigation.id) || !validLocalizedText(page.navigation.label) || state.navigationIDs.has(page.navigation.id) ||
      !["primary", "settings", "secondary"].includes(page.navigation.zone) || (page.navigation.groupID !== undefined && !managementName(page.navigation.groupID)))) {
    throw new PortalAssemblyError("NAVIGATION_REJECTED", `导航 ID 重复或语义区无效: ${page.navigation.id}`);
  }
  for (const slot of page.slots) {
    const slotKey = `${page.id}/${slot.id}`;
    if (!slot.id || !standardPageSlots.has(slot.slot) || state.slotIDs.has(slotKey) || typeof slot.component !== "function") {
      throw new PortalAssemblyError("SLOT_REJECTED", `Slot 贡献非法或重复: ${slotKey}`);
    }
    state.slotIDs.add(slotKey);
  }
  state.pageIDs.add(page.id);
  state.paths.add(mountedPath);
  if (page.navigation !== undefined) state.navigationIDs.add(page.navigation.id);
  state.pages.push({ ...page, path: mountedPath, slots: [...page.slots], pluginID: ref.id });
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
  rendererModuleKeys: ReadonlySet<string>,
  shellLibraryModuleKeys: ReadonlySet<string>,
): boolean {
  return [portal.runtimeEngine, portal.renderAdapter, portal.shell, portal.workbench].some((foundation) => samePlugin(ref, foundation)) ||
    rendererModuleKeys.has(moduleKey(ref)) || shellLibraryModuleKeys.has(moduleKey(ref));
}
