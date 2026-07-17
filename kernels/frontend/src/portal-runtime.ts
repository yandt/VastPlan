import type { DesignSystemAdapter, UICapability } from "@vastplan/portal-ui";

export interface PluginRef {
  id: string;
  version: string;
  channel?: string;
}

export interface DesignSystemSelection extends PluginRef {
  uiContract: string;
}

export interface PortalSpec {
  id: string;
  tenant: string;
  route: string;
  designSystem: DesignSystemSelection;
  plugins: PluginRef[];
}

export interface RemoteProvenance {
  signed: boolean;
  firstParty: boolean;
  integrity: string;
}

export interface FrontendPluginModule {
  provenance: RemoteProvenance;
  designSystem?: DesignSystemAdapter;
  register?(context: FrontendPluginContext): void | Promise<void>;
}

export interface FrontendPluginLoader {
  load(ref: PluginRef): Promise<FrontendPluginModule>;
}

export interface FrontendPluginContext {
  readonly portal: Readonly<PortalSpec>;
  addRoute(route: PluginRoute): void;
  addMenu(item: PluginMenuItem): void;
}

export interface PluginRoute {
  path: string;
  pluginID: string;
}

export interface PluginMenuItem {
  id: string;
  title: string;
  route: string;
}

export interface PreparedPortal {
  portal: Readonly<PortalSpec>;
  designSystem: DesignSystemAdapter;
  routes: readonly PluginRoute[];
  menus: readonly PluginMenuItem[];
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

/**
 * Security boundary for browser plugin assembly. It never accepts a remote merely
 * because it named itself a design system: provenance and UI-contract checks happen
 * before its code is allowed to register routes or UI slots.
 */
export class PortalRuntime {
  public constructor(private readonly loader: FrontendPluginLoader) {}

  public async prepare(portal: PortalSpec): Promise<PreparedPortal> {
    this.validatePortalShape(portal);
    const designSystemModule = await this.loader.load(portal.designSystem);
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

    const routes: PluginRoute[] = [];
    const menus: PluginMenuItem[] = [];
    const seenRoutes = new Set<string>();
    const seenMenus = new Set<string>();
    const context: FrontendPluginContext = {
      portal: Object.freeze({ ...portal, plugins: [...portal.plugins] }),
      addRoute: (route) => {
        if (!route.path.startsWith("/") || seenRoutes.has(route.path)) {
          throw new PortalAssemblyError("ROUTE_REJECTED", `插件路由非法或重复: ${route.path}`);
        }
        seenRoutes.add(route.path);
        routes.push({ ...route });
      },
      addMenu: (item) => {
        if (seenMenus.has(item.id) || !seenRoutes.has(item.route)) {
          throw new PortalAssemblyError("MENU_REJECTED", `菜单必须唯一且指向已注册路由: ${item.id}`);
        }
        seenMenus.add(item.id);
        menus.push({ ...item });
      },
    };

    for (const ref of portal.plugins) {
      if (samePlugin(ref, portal.designSystem)) {
        continue;
      }
      const plugin = await this.loader.load(ref);
      this.assertTrustedFirstParty(plugin, ref.id);
      if (plugin.designSystem !== undefined) {
        throw new PortalAssemblyError("SECOND_DESIGN_SYSTEM", "同一 Portal 不允许第二个设计系统");
      }
      await plugin.register?.(context);
    }
    return Object.freeze({ portal: context.portal, designSystem, routes, menus });
  }

  private validatePortalShape(portal: PortalSpec): void {
    if (!portal.id || !portal.tenant || !portal.route.startsWith("/")) {
      throw new PortalAssemblyError("PORTAL_INVALID", "Portal 必须包含 ID、租户和绝对根路由");
    }
    const count = portal.plugins.filter((ref) => samePlugin(ref, portal.designSystem)).length;
    if (count !== 1) {
      throw new PortalAssemblyError("DESIGN_SYSTEM_SELECTION", "Portal 插件列表必须精确包含一个已选设计系统");
    }
  }

  private assertTrustedFirstParty(module: FrontendPluginModule, pluginID: string): void {
    if (!module.provenance.signed || !module.provenance.firstParty || !module.provenance.integrity) {
      throw new PortalAssemblyError("UNTRUSTED_REMOTE", `拒绝加载未签名或非第一方远程模块: ${pluginID}`);
    }
  }
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
