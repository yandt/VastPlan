import { pageSlotIDs, shellSlotIDs } from "@vastplan/ui-primitives";
import type {
  LocalizedText,
  PluginLocalization,
  PortalLocalizationPolicy,
  UICapability,
  UIRenderAdapter,
  UIRenderer,
  UIShellAdapter,
} from "@vastplan/ui-primitives";
import { PortalAssemblyError } from "./portal-errors";
import type {
  CompositionRef,
  FrontendPluginModule,
  PluginRef,
  PortalSpec,
  RenderAdapterSelection,
  ShellSelection,
} from "./portal-contracts";

export const requiredCapabilities: readonly UICapability[] = [
  "layout",
  "menu",
  "overlay",
  "form",
  "data",
  "feedback",
  "theme",
];

export const standardPageSlots = new Set<string>(pageSlotIDs);
export const standardShellSlots = new Set<string>(shellSlotIDs);

export function validatePortalShape(portal: PortalSpec): void {
  if (!Number.isSafeInteger(portal.revision) || portal.revision <= 0 || !portal.id || !portal.tenantId || !portal.route.startsWith("/")) {
    throw new PortalAssemblyError("PORTAL_INVALID", "Portal 必须包含 revision、ID、租户和绝对根路由");
  }
  if ((portal.branding !== undefined && !isJSONRecord(portal.branding)) ||
      (portal.localization !== undefined && !validLocalizationPolicy(portal.localization)) ||
      !isJSONRecord(portal.shell.config) || !isJSONRecord(portal.renderAdapter.config)) {
    throw new PortalAssemblyError("PORTAL_INVALID", "Portal 品牌、Renderer 与 Shell 配置必须是 JSON 对象");
  }
  if (portal.experience !== undefined && (!Array.isArray(portal.experience.permissions) || portal.experience.permissions.some((permission) => !managementName(permission)) || new Set(portal.experience.permissions).size !== portal.experience.permissions.length)) {
    throw new PortalAssemblyError("PORTAL_EXPERIENCE_INVALID", "Portal 权限体验投影无效");
  }
  const refs = [portal.resolution.platformCatalog, portal.resolution.platformProfile, portal.resolution.applicationComposition];
  if (refs.some((ref) => !ref.id || !Number.isSafeInteger(ref.revision) || ref.revision <= 0 || !/^[a-f0-9]{64}$/.test(ref.digest))) {
    throw new PortalAssemblyError("RESOLUTION_INVALID", "Portal 输入解析锁无效");
  }
  if (!portal.runtimeEngine || !/^[a-z][a-z0-9-]{0,63}$/.test(portal.runtimeEngine.family) || !portal.runtimeEngine.engineContract) {
    throw new PortalAssemblyError("RUNTIME_ENGINE_SELECTION_INVALID", "Portal Runtime Engine 选择无效");
  }
  const foundations = [portal.runtimeEngine, portal.renderAdapter, portal.shell, portal.workbench];
  if (new Set(foundations.map((item) => item.id)).size !== foundations.length || foundations.some((selected) => portal.plugins.filter((ref) => samePlugin(ref, selected)).length !== 1)) {
    throw new PortalAssemblyError("SHELL_FOUNDATION_SELECTION", "Portal 必须精确包含相互独立的 Runtime Engine、设计系统、Shell 与 Workbench 插件");
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
  validateManagementServices(portal);
}

function validateManagementServices(portal: PortalSpec): void {
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

export function assertTrustedFirstParty(module: FrontendPluginModule, pluginID: string): void {
  if (!module.provenance.signed || !module.provenance.firstParty || !module.provenance.integrity) {
    throw new PortalAssemblyError("UNTRUSTED_REMOTE", `拒绝加载未签名或非第一方远程模块: ${pluginID}`);
  }
}

export function validThemeTemplateCatalog(adapter: UIRenderer): boolean {
  if (adapter.themeTemplates.length === 0) return false;
  const identifiers = new Set<string>();
  for (const template of adapter.themeTemplates) {
    if (!/^[a-z][a-z0-9-]{0,63}$/.test(template.id) || identifiers.has(template.id)) return false;
    if (template.scheme !== "light" && template.scheme !== "dark" && template.scheme !== "high-contrast") return false;
    identifiers.add(template.id);
  }
  return true;
}

export function validIconThemeCatalog(renderer: UIRenderer): boolean {
  if (renderer.iconThemes.length === 0 || !/^[a-z][a-z0-9-]{0,63}$/.test(renderer.defaultIconTheme)) return false;
  const identifiers = new Set<string>();
  for (const theme of renderer.iconThemes) {
    if (!/^[a-z][a-z0-9-]{0,63}$/.test(theme.id) || identifiers.has(theme.id) || !validLocalizedText(theme.label) ||
        (theme.source !== "canonical" && theme.source !== "renderer-native")) return false;
    identifiers.add(theme.id);
  }
  return identifiers.has(renderer.defaultIconTheme);
}

export function validRendererCatalog(adapter: UIRenderAdapter): boolean {
  if (!/^[a-z][a-z0-9-]{0,63}$/.test(adapter.defaultRenderer) || adapter.renderers.length === 0) return false;
  const identifiers = new Set<string>();
  const modules = new Set<string>();
  for (const renderer of adapter.renderers) {
    if (!/^[a-z][a-z0-9-]{0,63}$/.test(renderer.id) || identifiers.has(renderer.id) || !renderer.framework || !validLocalizedText(renderer.label) ||
        !validPluginRef(renderer.module) || modules.has(moduleKey(renderer.module))) return false;
    identifiers.add(renderer.id);
    modules.add(moduleKey(renderer.module));
  }
  return identifiers.has(adapter.defaultRenderer);
}

export function validRenderAdapterConfig(config: RenderAdapterSelection["config"], adapter: UIRenderAdapter): boolean {
  if (!/^[a-z][a-z0-9-]{0,63}$/.test(config.defaultRenderer) || !Array.isArray(config.allowedRenderers) || config.allowedRenderers.length === 0 || typeof config.userSelectable !== "boolean") return false;
  const declared = new Set(adapter.renderers.map((renderer) => renderer.id));
  const allowed = new Set<string>();
  for (const renderer of config.allowedRenderers) {
    if (!/^[a-z][a-z0-9-]{0,63}$/.test(renderer) || !declared.has(renderer) || allowed.has(renderer)) return false;
    allowed.add(renderer);
  }
  if (!allowed.has(config.defaultRenderer)) return false;
  for (const [renderer, options] of Object.entries(config.rendererOptions ?? {})) {
    if (!allowed.has(renderer) || !isJSONRecord(options) ||
        (options.themeTemplate !== undefined && !/^[a-z][a-z0-9-]{0,63}$/.test(options.themeTemplate)) ||
        (options.iconTheme !== undefined && !/^[a-z][a-z0-9-]{0,63}$/.test(options.iconTheme)) ||
        !validSelectableCatalog(options.allowedThemeTemplates, options.themeUserSelectable, options.themeTemplate) ||
        !validSelectableCatalog(options.allowedIconThemes, options.iconUserSelectable, options.iconTheme)) return false;
  }
  return true;
}

function validSelectableCatalog(values: unknown, selectable: unknown, selected: unknown): boolean {
  if (selectable !== undefined && typeof selectable !== "boolean") return false;
  if (values === undefined) return selectable !== true;
  if (!Array.isArray(values) || values.length > 16 || values.some((value) => typeof value !== "string" || !/^[a-z][a-z0-9-]{0,63}$/.test(value)) || new Set(values).size !== values.length) return false;
  return (selectable !== true || values.length > 0) && (selected === undefined || values.includes(selected));
}

export function validShellTemplateCatalog(shell: UIShellAdapter): boolean {
  if (shell.templates.length === 0 || !/^[a-z][a-z0-9-]{0,63}$/.test(shell.defaultTemplate)) return false;
  const identifiers = new Set<string>();
  const modules = new Set<string>();
  for (const template of shell.templates) {
    if (!/^[a-z][a-z0-9-]{0,63}$/.test(template.id) || identifiers.has(template.id) || !validLocalizedText(template.label) ||
        !validPluginRef(template.module) || modules.has(moduleKey(template.module))) return false;
    identifiers.add(template.id);
    modules.add(moduleKey(template.module));
  }
  return identifiers.has(shell.defaultTemplate);
}

export function validShellConfig(config: ShellSelection["config"], shell: UIShellAdapter): boolean {
  if (!/^[a-z][a-z0-9-]{0,63}$/.test(config.defaultTemplate) || !Array.isArray(config.allowedTemplates) || config.allowedTemplates.length === 0 || typeof config.userSelectable !== "boolean") return false;
  const declared = new Set(shell.templates.map((template) => template.id));
  const allowed = new Set<string>();
  for (const template of config.allowedTemplates) {
    if (!/^[a-z][a-z0-9-]{0,63}$/.test(template) || !declared.has(template) || allowed.has(template)) return false;
    allowed.add(template);
  }
  if (!allowed.has(config.defaultTemplate)) return false;
  for (const [template, options] of Object.entries(config.templateOptions ?? {})) {
    if (!allowed.has(template) || !isJSONRecord(options)) return false;
  }
  return true;
}

export function requiredModule(modules: ReadonlyMap<string, FrontendPluginModule>, ref: PluginRef): FrontendPluginModule {
  const module = modules.get(moduleKey(ref));
  if (module === undefined) throw new PortalAssemblyError("MODULE_NOT_LOADED", `已锁定模块未加载: ${ref.id}`);
  return module;
}

export function moduleKey(ref: PluginRef): string {
  return `${ref.id}@${ref.version}/${ref.channel ?? "stable"}`;
}

export function samePlugin(left: PluginRef, right: PluginRef): boolean {
  return left.id === right.id && left.version === right.version && (left.channel ?? "stable") === (right.channel ?? "stable");
}

export function managementName(value: string): boolean {
  return /^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$/.test(value) && value !== "*";
}

export function validLocalizedText(value: LocalizedText): boolean {
  if (typeof value === "string") return value.trim() !== "" && value.length <= 240;
  return isJSONRecord(value) && managementName(value.namespace) && managementName(value.key) && typeof value.fallback === "string" && value.fallback.trim() !== "" && value.fallback.length <= 240;
}

export function validateLocalization(pluginID: string, value: PluginLocalization): PluginLocalization {
  const locales = Object.keys(value.messages);
  if (locales.length === 0 || locales.length > 32 || typeof value.defaultLocale !== "string") throw new PortalAssemblyError("LOCALIZATION_INVALID", `插件语言资源无效: ${pluginID}`);
  let canonical: string[];
  let canonicalDefault: string;
  try {
    canonical = Intl.getCanonicalLocales(locales);
    canonicalDefault = Intl.getCanonicalLocales(value.defaultLocale)[0];
  } catch {
    throw new PortalAssemblyError("LOCALIZATION_INVALID", `插件包含非法 locale: ${pluginID}`);
  }
  if (canonical.length !== locales.length) throw new PortalAssemblyError("LOCALIZATION_INVALID", `插件包含重复 locale: ${pluginID}`);
  if (!canonical.includes(canonicalDefault)) throw new PortalAssemblyError("LOCALIZATION_DEFAULT_MISSING", `插件默认语言资源缺失: ${pluginID}`);
  const messages: Record<string, Readonly<Record<string, string>>> = {};
  let total = 0;
  for (let index = 0; index < locales.length; index += 1) {
    const copy: Record<string, string> = {};
    for (const [key, text] of Object.entries(value.messages[locales[index]])) {
      if (!managementName(key) || typeof text !== "string" || text.length > 8_192) throw new PortalAssemblyError("LOCALIZATION_MESSAGE_INVALID", `插件语言消息无效: ${pluginID}/${key}`);
      total += text.length;
      if (total > 1_048_576) throw new PortalAssemblyError("LOCALIZATION_TOO_LARGE", `插件语言资源超过上限: ${pluginID}`);
      copy[key] = text;
    }
    messages[canonical[index]] = Object.freeze(copy);
  }
  return Object.freeze({ defaultLocale: canonicalDefault, messages: Object.freeze(messages) });
}

export function mergeLocalization(base: PluginLocalization, extra: PluginLocalization): PluginLocalization {
  const messages: Record<string, Record<string, string>> = Object.fromEntries(Object.entries(base.messages).map(([locale, catalog]) => [locale, { ...catalog }]));
  for (const [locale, catalog] of Object.entries(extra.messages)) messages[locale] = { ...(messages[locale] ?? {}), ...catalog };
  return { defaultLocale: base.defaultLocale, messages };
}

export function mountPortalPagePath(portalRoute: string, pagePath: string): string | undefined {
  if (!pagePath.startsWith("/") || pagePath.includes("//") || pagePath.includes("\\") || pagePath.includes("%") || pagePath.includes("?") || pagePath.includes("#") ||
      pagePath.split("/").some((segment) => segment === "." || segment === "..")) return undefined;
  const route = portalRoute === "/" ? "" : portalRoute.replace(/\/$/, "");
  return pagePath === "/" ? (route || "/") : `${route}${pagePath}`;
}

function validPluginRef(ref: PluginRef): boolean {
  return /^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$/.test(ref.id) &&
    /^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.test(ref.version) &&
    (ref.channel === undefined || /^[a-z][a-z0-9-]{0,63}$/.test(ref.channel));
}

function validLocalizationPolicy(value: PortalLocalizationPolicy): boolean {
  if (typeof value.defaultLocale !== "string" || !Array.isArray(value.supportedLocales) || value.supportedLocales.length === 0 || value.supportedLocales.length > 32) return false;
  try {
    const supported = Intl.getCanonicalLocales(value.supportedLocales);
    return supported.length === value.supportedLocales.length && supported.includes(Intl.getCanonicalLocales(value.defaultLocale)[0]);
  } catch {
    return false;
  }
}

function sameCompositionRef(left: CompositionRef, right: CompositionRef): boolean {
  return left.id === right.id && left.revision === right.revision && left.digest === right.digest;
}

function isJSONRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
