import type { DesignSystemAdapter, FrontendPluginContext, PortalRegisteredPage, ShellCompositionAdapter, ShellLayoutAdapter, UICapability } from "@vastplan/portal-ui";

export interface PluginRef {
  id: string;
  version: string;
  channel?: string;
}

export interface DesignSystemSelection extends PluginRef {
  uiContract: string;
}

export interface ShellCompositionSelection extends PluginRef { uiContract: string; }
export interface ShellLayoutSelection extends PluginRef { uiContract: string; config?: Record<string, unknown>; }

export interface PortalSpec {
  revision: number;
  id: string;
  tenantId: string;
  route: string;
  branding?: Record<string, unknown>;
  designSystem: DesignSystemSelection;
  composition: ShellCompositionSelection;
  layout: ShellLayoutSelection;
  plugins: PluginRef[];
  resolution: PortalResolution;
}

export interface CompositionRef {
  id: string;
  revision: number;
  digest: string;
}

export interface PortalResolution {
  platformProfile: CompositionRef;
  applicationComposition: CompositionRef;
  pluginOrigins: Record<string, "platform-profile" | "application">;
}

export interface RemoteProvenance {
  signed: boolean;
  firstParty: boolean;
  integrity: string;
}

export interface FrontendPluginModule {
  provenance: RemoteProvenance;
  designSystem?: DesignSystemAdapter;
  composition?: ShellCompositionAdapter;
  layout?: ShellLayoutAdapter;
  register?(context: FrontendPluginContext): void | Promise<void>;
}

export interface FrontendPluginLoader {
  load(ref: PluginRef): Promise<FrontendPluginModule>;
}

export interface PreparedPortal {
  portal: Readonly<PortalSpec>;
  designSystem: DesignSystemAdapter;
  composition: ShellCompositionAdapter;
  layout: ShellLayoutAdapter;
  pages: readonly PortalRegisteredPage[];
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
const standardSlots = new Set([
  "shell.header.start", "shell.header.center", "shell.header.end",
  "shell.navigation.before", "shell.navigation.after",
  "page.header.start", "page.header.center", "page.header.end",
  "page.body.before", "page.body.main", "page.body.after", "page.aside", "shell.footer",
]);

/**
 * Security boundary for browser plugin assembly. It never accepts a remote merely
 * because it named itself a design system: provenance and UI-contract checks happen
 * before its code is allowed to register routes or UI slots.
 */
export class PortalRuntime {
  public constructor(private readonly loader: FrontendPluginLoader) {}

  public async prepare(portal: PortalSpec): Promise<PreparedPortal> {
    this.validatePortalShape(portal);
    // Start every governed fetch immediately. Trust and registration checks are
    // still applied in deterministic profile order after all bytes arrive.
    const loaded = await Promise.all(portal.plugins.map(async (ref) => ({ ref, module: await this.loader.load(ref) })));
    const modules = new Map(loaded.map((item) => [moduleKey(item.ref), item.module]));
    const designSystemModule = requiredModule(modules, portal.designSystem);
    this.assertTrustedFirstParty(designSystemModule, portal.designSystem.id);
    const designSystem = designSystemModule.designSystem;
    if (designSystem === undefined) {
      throw new PortalAssemblyError("DESIGN_SYSTEM_MISSING", "指定插件没有 ui.design-system 贡献");
    }
    if (designSystem.id !== "ui.design-system") {
      throw new PortalAssemblyError("DESIGN_SYSTEM_INVALID", "设计系统贡献 ID 必须为 ui.design-system");
    }
    if (!contractSatisfies(designSystem.uiContract, portal.designSystem.uiContract)) {
      throw new PortalAssemblyError("UI_CONTRACT_INCOMPATIBLE", "设计系统与 Portal 的 UI 契约不兼容");
    }
    const capabilities = new Set(designSystem.capabilities);
    if (!requiredCapabilities.every((capability) => capabilities.has(capability))) {
      throw new PortalAssemblyError("DESIGN_SYSTEM_INCOMPLETE", "设计系统未实现 Portal 所需的全部 UI 能力");
    }

    const compositionModule = requiredModule(modules, portal.composition);
    this.assertTrustedFirstParty(compositionModule, portal.composition.id);
    const composition = compositionModule.composition;
    if (composition?.id !== "ui.shell-composition" || !contractSatisfies(composition.uiContract, portal.composition.uiContract)) {
      throw new PortalAssemblyError("SHELL_COMPOSITION_INVALID", "Shell 组合插件缺失或 UI 契约不兼容");
    }
    const layoutModule = requiredModule(modules, portal.layout);
    this.assertTrustedFirstParty(layoutModule, portal.layout.id);
    const layout = layoutModule.layout;
    if (layout?.id !== "ui.shell-layout" || typeof layout.Shell !== "function" || !contractSatisfies(layout.uiContract, portal.layout.uiContract)) {
      throw new PortalAssemblyError("SHELL_LAYOUT_INVALID", "Shell 布局插件缺失或 UI 契约不兼容");
    }

    const pages: PortalRegisteredPage[] = [];
    const seenPageIDs = new Set<string>();
    const seenPaths = new Set<string>();
    const seenNavigationIDs = new Set<string>();
    const seenSlotIDs = new Set<string>();
    const portalSnapshot = Object.freeze({ ...portal, plugins: [...portal.plugins] });

    for (const ref of portal.plugins) {
      if ([portal.designSystem, portal.composition, portal.layout].some((foundation) => samePlugin(ref, foundation))) {
        continue;
      }
      const plugin = requiredModule(modules, ref);
      this.assertTrustedFirstParty(plugin, ref.id);
      if (plugin.designSystem !== undefined || plugin.composition !== undefined || plugin.layout !== undefined) {
        throw new PortalAssemblyError("SECOND_SHELL_FOUNDATION", "功能插件不能注册第二个设计系统、Shell 组合或布局");
      }
      const context: FrontendPluginContext = {
        portal: portalSnapshot,
        addPage: (page) => {
          if (!page.id || !page.path.startsWith("/") || !page.title || seenPageIDs.has(page.id) || seenPaths.has(page.path) || !Array.isArray(page.slots)) {
            throw new PortalAssemblyError("PAGE_REJECTED", `页面 ID/路径非法或重复: ${page.id || page.path}`);
          }
          if (!page.slots.some((slot) => slot.slot === "page.body.main")) {
            throw new PortalAssemblyError("PAGE_MAIN_MISSING", `页面必须填充 page.body.main: ${page.id}`);
          }
          if (page.navigation !== undefined && (seenNavigationIDs.has(page.navigation.id) || !["primary", "settings", "secondary"].includes(page.navigation.zone))) {
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
          seenPaths.add(page.path);
          if (page.navigation !== undefined) seenNavigationIDs.add(page.navigation.id);
          pages.push({ ...page, slots: [...page.slots], pluginID: ref.id });
        },
      };
      await plugin.register?.(context);
    }
    return Object.freeze({ portal: portalSnapshot, designSystem, composition, layout, pages });
  }

  private validatePortalShape(portal: PortalSpec): void {
    if (!Number.isSafeInteger(portal.revision) || portal.revision <= 0 || !portal.id || !portal.tenantId || !portal.route.startsWith("/")) {
      throw new PortalAssemblyError("PORTAL_INVALID", "Portal 必须包含 revision、ID、租户和绝对根路由");
    }
    const refs = [portal.resolution.platformProfile, portal.resolution.applicationComposition];
    if (refs.some((ref) => !ref.id || !Number.isSafeInteger(ref.revision) || ref.revision <= 0 || !/^[a-f0-9]{64}$/.test(ref.digest))) {
      throw new PortalAssemblyError("RESOLUTION_INVALID", "Portal 输入解析锁无效");
    }
    const foundations = [portal.designSystem, portal.composition, portal.layout];
    if (new Set(foundations.map((item) => item.id)).size !== foundations.length || foundations.some((selected) => portal.plugins.filter((ref) => samePlugin(ref, selected)).length !== 1)) {
      throw new PortalAssemblyError("SHELL_FOUNDATION_SELECTION", "Portal 必须精确包含相互独立的设计系统、Shell 组合与布局插件");
    }
    const pluginIDs = new Set(portal.plugins.map((ref) => ref.id));
    if (foundations.some((selected) => portal.resolution.pluginOrigins[selected.id] !== "platform-profile") ||
        Object.keys(portal.resolution.pluginOrigins).length !== pluginIDs.size ||
        [...pluginIDs].some((id) => portal.resolution.pluginOrigins[id] === undefined)) {
      throw new PortalAssemblyError("ORIGIN_LOCK_INVALID", "Portal 插件来源锁缺失或设计系统并非平台基线");
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
