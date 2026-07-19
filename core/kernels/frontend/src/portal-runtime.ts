import { createElement } from "react";
import { message, pageSlotIDs, shellSlotIDs } from "@vastplan/ui-primitives";
import type { UIRenderAdapter, FrontendPluginContext, FrontendPluginHotLifecycle, LocalizedText, PluginLocalization, PortalLocalizationPolicy, PortalManagementService, PortalMessageCatalogs, PortalRegisteredPage, PortalRegisteredShellContribution, StructureCompositionAdapter, StructureLayoutAdapter, UICapability, UIWorkbenchAdapter } from "@vastplan/ui-primitives";

export interface PluginRef {
  id: string;
  version: string;
  channel?: string;
}

export interface RenderAdapterSelection extends PluginRef {
  uiContract: string;
}

export interface StructureCompositionSelection extends PluginRef { uiContract: string; config?: Record<string, unknown>; }
export interface StructureLayoutSelection extends PluginRef { uiContract: string; config?: Record<string, unknown>; }
export interface WorkbenchSelection extends PluginRef { uiContract: string; }

export interface PortalSpec {
  revision: number;
  id: string;
  tenantId: string;
  route: string;
  branding?: Record<string, unknown>;
  localization?: PortalLocalizationPolicy;
  renderAdapter: RenderAdapterSelection;
  structureComposition: StructureCompositionSelection;
  structureLayout: StructureLayoutSelection;
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

export interface RemoteProvenance {
  signed: boolean;
  firstParty: boolean;
  integrity: string;
}

export interface FrontendPluginModule {
  provenance: RemoteProvenance;
  renderAdapter?: UIRenderAdapter;
  structureComposition?: StructureCompositionAdapter;
  structureLayout?: StructureLayoutAdapter;
  workbench?: UIWorkbenchAdapter;
  register?(context: FrontendPluginContext): void | Promise<void>;
  hot?: FrontendPluginHotLifecycle;
  localization?: PluginLocalization;
}

export interface FrontendPluginLoader {
  load(ref: PluginRef): Promise<FrontendPluginModule>;
}

export interface PreparedPortal {
  portal: Readonly<PortalSpec>;
  renderAdapter: UIRenderAdapter;
  structureComposition: StructureCompositionAdapter;
  structureLayout: StructureLayoutAdapter;
  workbench: UIWorkbenchAdapter;
  pages: readonly PortalRegisteredPage[];
  shellContributions: readonly PortalRegisteredShellContribution[];
  modules: readonly PreparedFrontendPlugin[];
  messageCatalogs: PortalMessageCatalogs;
}

export interface PreparedFrontendPlugin {
  ref: Readonly<PluginRef>;
  module: FrontendPluginModule;
}

export interface PortalPrepareOptions {
  generation?: string;
  signal?: AbortSignal;
  reason?: "bootstrap" | "replace";
}

const requiredCapabilities: readonly UICapability[] = [
  "layout",
  "menu",
  "overlay",
  "form",
  "data",
  "feedback",
  "theme",
];
const standardSlots = new Set<string>(pageSlotIDs);
const standardShellSlots = new Set<string>(shellSlotIDs);

/**
 * Security boundary for browser plugin assembly. It never accepts a remote merely
 * because it named itself a design system: provenance and UI-contract checks happen
 * before its code is allowed to register routes or UI slots.
 */
export class PortalRuntime {
  public constructor(private readonly loader: FrontendPluginLoader) {}

  public async prepare(portal: PortalSpec, options: PortalPrepareOptions = {}): Promise<PreparedPortal> {
    this.validatePortalShape(portal);
    // Start every governed fetch immediately. Trust and registration checks are
    // still applied in deterministic profile order after all bytes arrive.
    const loaded = await Promise.all(portal.plugins.map(async (ref) => ({ ref, module: await this.loader.load(ref) })));
    const modules = new Map(loaded.map((item) => [moduleKey(item.ref), item.module]));
    const renderAdapterModule = requiredModule(modules, portal.renderAdapter);
    this.assertTrustedFirstParty(renderAdapterModule, portal.renderAdapter.id);
    const renderAdapter = renderAdapterModule.renderAdapter;
    if (renderAdapter === undefined) {
      throw new PortalAssemblyError("DESIGN_SYSTEM_MISSING", "指定插件没有 ui.render.adapter 贡献");
    }
    if (renderAdapter.id !== "ui.render.adapter") {
      throw new PortalAssemblyError("DESIGN_SYSTEM_INVALID", "设计系统贡献 ID 必须为 ui.render.adapter");
    }
    if (!contractSatisfies(renderAdapter.uiContract, portal.renderAdapter.uiContract)) {
      throw new PortalAssemblyError("UI_CONTRACT_INCOMPATIBLE", "设计系统与 Portal 的 UI 契约不兼容");
    }
    const capabilities = new Set(renderAdapter.capabilities);
    if (!requiredCapabilities.every((capability) => capabilities.has(capability))) {
      throw new PortalAssemblyError("DESIGN_SYSTEM_INCOMPLETE", "设计系统未实现 Portal 所需的全部 UI 能力");
    }

    const compositionModule = requiredModule(modules, portal.structureComposition);
    this.assertTrustedFirstParty(compositionModule, portal.structureComposition.id);
    const composition = compositionModule.structureComposition;
    if (composition?.id !== "ui.structure.composition" || !contractSatisfies(composition.uiContract, portal.structureComposition.uiContract)) {
      throw new PortalAssemblyError("SHELL_COMPOSITION_INVALID", "Shell 组合插件缺失或 UI 契约不兼容");
    }
    const layoutModule = requiredModule(modules, portal.structureLayout);
    this.assertTrustedFirstParty(layoutModule, portal.structureLayout.id);
    const layout = layoutModule.structureLayout;
    if (layout?.id !== "ui.structure.layout" || typeof layout.Shell !== "function" || !contractSatisfies(layout.uiContract, portal.structureLayout.uiContract)) {
      throw new PortalAssemblyError("SHELL_LAYOUT_INVALID", "Shell 布局插件缺失或 UI 契约不兼容");
    }
    const workbenchModule = requiredModule(modules, portal.workbench);
    this.assertTrustedFirstParty(workbenchModule, portal.workbench.id);
    const workbench = workbenchModule.workbench;
    if (workbench?.id !== "ui.workflow.workbench" || typeof workbench.CollectionPage !== "function" || !contractSatisfies(workbench.uiContract, portal.workbench.uiContract)) {
      throw new PortalAssemblyError("WORKBENCH_INVALID", "UI Workbench 插件缺失或 UI 契约不兼容");
    }

    const pages: PortalRegisteredPage[] = [];
    const seenPageIDs = new Set<string>();
    const seenPaths = new Set<string>();
    const seenNavigationIDs = new Set<string>();
    const seenSlotIDs = new Set<string>();
    const seenShellContributionIDs = new Set<string>();
    const shellContributions: PortalRegisteredShellContribution[] = [];
    const portalSnapshot = snapshotPortal(portal);
    const generation = options.generation ?? `portal-${portal.revision}`;
    const signal = options.signal ?? new AbortController().signal;
    const reason = options.reason ?? "bootstrap";
    const messageCatalogs: Record<string, PluginLocalization> = {};
    for (const { ref, module } of loaded) {
      if (module.localization === undefined) throw new PortalAssemblyError("LOCALIZATION_REQUIRED", `UI 插件必须声明语言资源: ${ref.id}`);
      const localization = validateLocalization(ref.id, module.localization);
      if (module.provenance.firstParty && (!Object.hasOwn(localization.messages, "zh-CN") || !Object.hasOwn(localization.messages, "en-US"))) {
        throw new PortalAssemblyError("LOCALIZATION_FIRST_PARTY_INCOMPLETE", `第一方 UI 插件必须包含 zh-CN 与 en-US: ${ref.id}`);
      }
      messageCatalogs[ref.id] = localization;
    }

    for (const ref of portal.plugins) {
      if ([portal.renderAdapter, portal.structureComposition, portal.structureLayout, portal.workbench].some((foundation) => samePlugin(ref, foundation))) {
        continue;
      }
      const plugin = requiredModule(modules, ref);
      this.assertTrustedFirstParty(plugin, ref.id);
      if (plugin.renderAdapter !== undefined || plugin.structureComposition !== undefined || plugin.structureLayout !== undefined || plugin.workbench !== undefined) {
        throw new PortalAssemblyError("SECOND_SHELL_FOUNDATION", "功能插件不能注册第二个设计系统、Shell 组合、布局或 Workbench");
      }
      const context: FrontendPluginContext = {
        portal: portalSnapshot,
        lifecycle: Object.freeze({ pluginID: ref.id, generation, signal, reason }),
        i18n: Object.freeze({ message: (key, fallback, values) => message(ref.id, key, fallback, values) }),
        addShellContribution: (contribution) => {
          const key = `${ref.id}/${contribution.id}`;
          if (portal.resolution.pluginOrigins[ref.id] !== "platform-profile") {
            throw new PortalAssemblyError("SHELL_CONTRIBUTION_ORIGIN", `应用插件不能贡献全局 Shell 区域: ${ref.id}`);
          }
          if (!managementName(contribution.id) || !standardShellSlots.has(contribution.slot) || typeof contribution.component !== "function" || seenShellContributionIDs.has(key)) {
            throw new PortalAssemblyError("SHELL_CONTRIBUTION_REJECTED", `Shell 贡献非法或重复: ${key}`);
          }
          seenShellContributionIDs.add(key);
          shellContributions.push({ ...contribution, pluginID: ref.id });
        },
        addPage: (page) => {
          const mountedPath = mountPortalPagePath(portal.route, page.path);
          if (!page.id || mountedPath === undefined || !validLocalizedText(page.title) || (page.description !== undefined && !validLocalizedText(page.description)) || seenPageIDs.has(page.id) || seenPaths.has(mountedPath) || !Array.isArray(page.slots)) {
            throw new PortalAssemblyError("PAGE_REJECTED", `页面 ID/路径非法或重复: ${page.id || page.path}`);
          }
          if (!page.slots.some((slot) => slot.slot === "page.body.main")) {
            throw new PortalAssemblyError("PAGE_MAIN_MISSING", `页面必须填充 page.body.main: ${page.id}`);
          }
          if (page.navigation !== undefined && (!managementName(page.navigation.id) || !validLocalizedText(page.navigation.label) ||
              seenNavigationIDs.has(page.navigation.id) || !["primary", "settings", "secondary"].includes(page.navigation.zone) ||
              (page.navigation.groupID !== undefined && !managementName(page.navigation.groupID)))) {
            throw new PortalAssemblyError("NAVIGATION_REJECTED", `导航 ID 重复或语义区无效: ${page.navigation.id}`);
          }
          for (const slot of page.slots) {
            const slotKey = `${page.id}/${slot.id}`;
            if (!slot.id || !standardSlots.has(slot.slot) || seenSlotIDs.has(slotKey) || typeof slot.component !== "function") {
              throw new PortalAssemblyError("SLOT_REJECTED", `Slot 贡献非法或重复: ${slotKey}`);
            }
            seenSlotIDs.add(slotKey);
          }
          seenPageIDs.add(page.id);
          seenPaths.add(mountedPath);
          if (page.navigation !== undefined) seenNavigationIDs.add(page.navigation.id);
          pages.push({ ...page, path: mountedPath, slots: [...page.slots], pluginID: ref.id });
        },
        addCollectionPage: (page) => {
          if (!page.id || !page.collection.id || page.collection.view !== "table" || page.collection.query.mode !== "page" || page.collection.columns.length === 0 || typeof page.load !== "function") {
            throw new PortalAssemblyError("WORKBENCH_PAGE_REJECTED", `集合页面定义无效: ${page.id}`);
          }
          const Page = () => createElement(workbench.CollectionPage, { page, preferenceScope: `${portal.tenantId}/${portal.id}` });
          context.addPage({ id: page.id, path: page.path, title: page.title, description: page.description, navigation: page.navigation, slots: [{ id: "workbench.collection", slot: "page.body.main", component: Page }] });
        },
      };
      await plugin.register?.(context);
    }
    const preparedModules = portal.plugins.map((ref) => Object.freeze({ ref: Object.freeze({ ...ref }), module: requiredModule(modules, ref) }));
    return Object.freeze({ portal: portalSnapshot, renderAdapter, structureComposition: composition, structureLayout: layout, workbench, pages: Object.freeze(pages), shellContributions: Object.freeze(shellContributions), modules: Object.freeze(preparedModules), messageCatalogs: Object.freeze(messageCatalogs) });
  }

  private validatePortalShape(portal: PortalSpec): void {
    if (!Number.isSafeInteger(portal.revision) || portal.revision <= 0 || !portal.id || !portal.tenantId || !portal.route.startsWith("/")) {
      throw new PortalAssemblyError("PORTAL_INVALID", "Portal 必须包含 revision、ID、租户和绝对根路由");
    }
    if ((portal.branding !== undefined && !isJSONRecord(portal.branding)) ||
        (portal.localization !== undefined && !validLocalizationPolicy(portal.localization)) ||
        (portal.structureComposition.config !== undefined && !isJSONRecord(portal.structureComposition.config)) ||
        (portal.structureLayout.config !== undefined && !isJSONRecord(portal.structureLayout.config))) {
      throw new PortalAssemblyError("PORTAL_INVALID", "Portal 品牌与 Shell 配置必须是 JSON 对象");
    }
    const refs = [portal.resolution.platformCatalog, portal.resolution.platformProfile, portal.resolution.applicationComposition];
    if (refs.some((ref) => !ref.id || !Number.isSafeInteger(ref.revision) || ref.revision <= 0 || !/^[a-f0-9]{64}$/.test(ref.digest))) {
      throw new PortalAssemblyError("RESOLUTION_INVALID", "Portal 输入解析锁无效");
    }
    const foundations = [portal.renderAdapter, portal.structureComposition, portal.structureLayout, portal.workbench];
    if (new Set(foundations.map((item) => item.id)).size !== foundations.length || foundations.some((selected) => portal.plugins.filter((ref) => samePlugin(ref, selected)).length !== 1)) {
      throw new PortalAssemblyError("SHELL_FOUNDATION_SELECTION", "Portal 必须精确包含相互独立的设计系统、Shell 组合、布局与 Workbench 插件");
    }
    const pluginIDs = new Set(portal.plugins.map((ref) => ref.id));
    if (foundations.some((selected) => portal.resolution.pluginOrigins[selected.id] !== "platform-profile") ||
        Object.keys(portal.resolution.pluginOrigins).length !== pluginIDs.size ||
        [...pluginIDs].some((id) => portal.resolution.pluginOrigins[id] === undefined)) {
      throw new PortalAssemblyError("ORIGIN_LOCK_INVALID", "Portal 插件来源锁缺失或设计系统并非平台基线");
    }
    if (portal.management.tenantId !== portal.tenantId || portal.management.portalId !== portal.id ||
        !sameCompositionRef(portal.management.platformProfile, portal.resolution.platformProfile) ||
        !/^[a-f0-9]{64}$/.test(portal.resolution.managementBindingDigest) || portal.management.services.length === 0) {
      throw new PortalAssemblyError("MANAGEMENT_BINDING_INVALID", "Portal 管理绑定与解析锁不一致");
    }
    const serviceIDs = new Set<string>();
    const serviceTargets = new Set<string>();
    for (const service of portal.management.services) {
      const target = `${service.logicalService}\u0000${service.routingDomain}`;
      if (!managementName(service.id) || !managementName(service.logicalService) || !managementName(service.routingDomain) ||
          serviceIDs.has(service.id) || serviceTargets.has(target) || service.capabilities.length === 0) {
        throw new PortalAssemblyError("MANAGEMENT_SERVICE_INVALID", `Portal 管理服务重复或无效: ${service.id}`);
      }
      serviceIDs.add(service.id);
      serviceTargets.add(target);
      const capabilities = new Set<string>();
      for (const grant of service.capabilities) {
        const operations = [...(grant.read ?? []), ...(grant.write ?? [])];
        if (!managementName(grant.capability) || capabilities.has(grant.capability) || operations.length === 0 ||
            operations.some((operation) => !managementName(operation)) || new Set(operations).size !== operations.length) {
          throw new PortalAssemblyError("MANAGEMENT_GRANT_INVALID", `Portal 管理授权无效: ${service.id}/${grant.capability}`);
        }
        capabilities.add(grant.capability);
      }
    }
  }

  private assertTrustedFirstParty(module: FrontendPluginModule, pluginID: string): void {
    if (!module.provenance.signed || !module.provenance.firstParty || !module.provenance.integrity) {
      throw new PortalAssemblyError("UNTRUSTED_REMOTE", `拒绝加载未签名或非第一方远程模块: ${pluginID}`);
    }
  }
}

function requiredModule(modules: ReadonlyMap<string, FrontendPluginModule>, ref: PluginRef): FrontendPluginModule {
  const module = modules.get(moduleKey(ref));
  if (module === undefined) throw new PortalAssemblyError("MODULE_NOT_LOADED", `已锁定模块未加载: ${ref.id}`);
  return module;
}

function moduleKey(ref: PluginRef): string { return `${ref.id}@${ref.version}/${ref.channel ?? "stable"}`; }

function managementName(value: string): boolean {
  return /^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$/.test(value) && value !== "*";
}

function isJSONRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function validLocalizedText(value: LocalizedText): boolean {
  if (typeof value === "string") return value.trim() !== "" && value.length <= 240;
  return isJSONRecord(value) && managementName(value.namespace) && managementName(value.key) && typeof value.fallback === "string" && value.fallback.trim() !== "" && value.fallback.length <= 240;
}

function validLocalizationPolicy(value: PortalLocalizationPolicy): boolean {
  if (typeof value.defaultLocale !== "string" || !Array.isArray(value.supportedLocales) || value.supportedLocales.length === 0 || value.supportedLocales.length > 32) return false;
  try {
    const supported = Intl.getCanonicalLocales(value.supportedLocales);
    return supported.length === value.supportedLocales.length && supported.includes(Intl.getCanonicalLocales(value.defaultLocale)[0]);
  } catch { return false; }
}

function validateLocalization(pluginID: string, value: PluginLocalization): PluginLocalization {
  const locales = Object.keys(value.messages);
  if (locales.length === 0 || locales.length > 32 || typeof value.defaultLocale !== "string") throw new PortalAssemblyError("LOCALIZATION_INVALID", `插件语言资源无效: ${pluginID}`);
  let canonical: string[];
  let canonicalDefault: string;
  try { canonical = Intl.getCanonicalLocales(locales); canonicalDefault = Intl.getCanonicalLocales(value.defaultLocale)[0]; } catch { throw new PortalAssemblyError("LOCALIZATION_INVALID", `插件包含非法 locale: ${pluginID}`); }
  if (canonical.length !== locales.length) throw new PortalAssemblyError("LOCALIZATION_INVALID", `插件包含重复 locale: ${pluginID}`);
  if (!canonical.includes(canonicalDefault)) throw new PortalAssemblyError("LOCALIZATION_DEFAULT_MISSING", `插件默认语言资源缺失: ${pluginID}`);
  const messages: Record<string, Readonly<Record<string, string>>> = {};
  let total = 0;
  for (let index = 0; index < locales.length; index += 1) {
    const entries = value.messages[locales[index]];
    const copy: Record<string, string> = {};
    for (const [key, text] of Object.entries(entries)) {
      if (!managementName(key) || typeof text !== "string" || text.length > 8_192) throw new PortalAssemblyError("LOCALIZATION_MESSAGE_INVALID", `插件语言消息无效: ${pluginID}/${key}`);
      total += text.length;
      if (total > 1_048_576) throw new PortalAssemblyError("LOCALIZATION_TOO_LARGE", `插件语言资源超过上限: ${pluginID}`);
      copy[key] = text;
    }
    messages[canonical[index]] = Object.freeze(copy);
  }
  return Object.freeze({ defaultLocale: canonicalDefault, messages: Object.freeze(messages) });
}

function mountPortalPagePath(portalRoute: string, pagePath: string): string | undefined {
  if (!pagePath.startsWith("/") || pagePath.includes("//") || pagePath.includes("\\") || pagePath.includes("%") || pagePath.includes("?") || pagePath.includes("#") ||
      pagePath.split("/").some((segment) => segment === "." || segment === "..")) {
    return undefined;
  }
  const route = portalRoute === "/" ? "" : portalRoute.replace(/\/$/, "");
  return pagePath === "/" ? (route || "/") : `${route}${pagePath}`;
}

function sameCompositionRef(left: CompositionRef, right: CompositionRef): boolean {
  return left.id === right.id && left.revision === right.revision && left.digest === right.digest;
}

function snapshotPortal(portal: PortalSpec): Readonly<PortalSpec> {
  const services = portal.management.services.map((service) => Object.freeze({
    ...service,
    capabilities: Object.freeze(service.capabilities.map((grant) => Object.freeze({
      ...grant,
      read: grant.read === undefined ? undefined : Object.freeze([...grant.read]),
      write: grant.write === undefined ? undefined : Object.freeze([...grant.write]),
    }))),
  }));
  return Object.freeze({
    ...portal,
    branding: portal.branding === undefined ? undefined : freezeJSONRecord(portal.branding),
    localization: portal.localization === undefined ? undefined : Object.freeze({ defaultLocale: portal.localization.defaultLocale, supportedLocales: Object.freeze([...portal.localization.supportedLocales]) }),
    renderAdapter: Object.freeze({ ...portal.renderAdapter }),
    structureComposition: Object.freeze({ ...portal.structureComposition, config: portal.structureComposition.config === undefined ? undefined : freezeJSONRecord(portal.structureComposition.config) }),
    structureLayout: Object.freeze({ ...portal.structureLayout, config: portal.structureLayout.config === undefined ? undefined : freezeJSONRecord(portal.structureLayout.config) }),
    plugins: Object.freeze(portal.plugins.map((ref) => Object.freeze({ ...ref }))),
    management: Object.freeze({ ...portal.management, services: Object.freeze(services) }),
    resolution: Object.freeze({ ...portal.resolution, pluginOrigins: Object.freeze({ ...portal.resolution.pluginOrigins }) }),
  });
}

function freezeJSONRecord(value: Readonly<Record<string, unknown>>): Readonly<Record<string, unknown>> {
  const copy: Record<string, unknown> = {};
  for (const [key, item] of Object.entries(value)) copy[key] = freezeJSONValue(item);
  return Object.freeze(copy);
}

function freezeJSONValue(value: unknown): unknown {
  if (Array.isArray(value)) return Object.freeze(value.map(freezeJSONValue));
  if (typeof value === "object" && value !== null) return freezeJSONRecord(value as Record<string, unknown>);
  return value;
}

export class PortalAssemblyError extends Error {
  public constructor(public readonly code: string, message: string) {
    super(message);
    this.name = "PortalAssemblyError";
  }
}

function samePlugin(left: PluginRef, right: PluginRef): boolean {
  return left.id === right.id && left.version === right.version && (left.channel ?? "stable") === (right.channel ?? "stable");
}

/** Limited, fail-closed semver matcher for the v1 public UI contract. */
export function contractSatisfies(actual: string, requested: string): boolean {
  const actualMatch = /^(\d+)\.(\d+)\.(\d+)$/.exec(actual);
  const requestedMatch = /^\^(\d+)\.(\d+)\.(\d+)$/.exec(requested);
  if (actualMatch === null || requestedMatch === null) {
    return false;
  }
  const [actualMajor, actualMinor, actualPatch] = actualMatch.slice(1).map(Number);
  const [requestedMajor, requestedMinor, requestedPatch] = requestedMatch.slice(1).map(Number);
  return actualMajor === requestedMajor && (actualMinor > requestedMinor || (actualMinor === requestedMinor && actualPatch >= requestedPatch));
}
