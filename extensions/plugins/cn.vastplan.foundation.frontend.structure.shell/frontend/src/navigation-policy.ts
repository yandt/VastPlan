import { accountNavigationGroupID, accountSettingsNavigationGroupID, hostNavigationPluginID, message } from "@vastplan/ui-primitives";
import type {
  ActiveNavigationPath,
  NavigationZone,
  PortalNavigationCatalog,
  PortalNavigationGroupDescriptor,
  PortalNavigationIconSpec,
  PortalNavigationNodeRef,
  PortalPageNavigation,
  PortalResolvedPageNavigation,
} from "@vastplan/ui-primitives";

const namespace = "cn.vastplan.foundation.frontend.structure.shell";
const navigationZones = new Set<NavigationZone>(["primary", "secondary", "settings"]);

const semantic = (name: Extract<PortalNavigationIconSpec, { kind: "semantic" }>["name"]): PortalNavigationIconSpec => Object.freeze({ kind: "semantic", name });
const hostID = (nodeID: string): string => navigationNodeKey({ pluginID: hostNavigationPluginID, nodeID });

const defaultGroups: readonly PortalNavigationGroupDescriptor[] = [
  { id: hostID("primary"), label: message(namespace, "navigation.primary", "主要功能"), zone: "primary", icon: semantic("menu"), order: -1000 },
  { id: hostID("secondary"), label: message(namespace, "navigation.secondary", "辅助功能"), zone: "secondary", icon: semantic("info"), order: -1000 },
  { id: hostID("settings"), label: message(namespace, "navigation.settings", "系统管理"), zone: "settings", icon: semantic("settings"), order: 1000 },
  { id: hostID(accountNavigationGroupID), label: message(namespace, "navigation.account", "用户"), zone: "secondary", icon: semantic("info"), order: 2000 },
  { id: hostID(accountSettingsNavigationGroupID), parentID: hostID(accountNavigationGroupID), label: message(namespace, "navigation.accountSettings", "用户设置"), zone: "secondary", icon: semantic("settings"), order: 20 },
];

export interface NavigationPolicy {
  readonly groups: ReadonlyMap<string, PortalNavigationGroupDescriptor>;
  resolve(page: PortalPageNavigation): PortalResolvedPageNavigation;
  path(page: PortalPageNavigation): ActiveNavigationPath;
}

/** Compiles signed plugin defaults and the current Portal's bounded overrides once. */
export function compileNavigationPolicy(catalogs: readonly PortalNavigationCatalog[], config: Readonly<Record<string, unknown>> | undefined): NavigationPolicy {
  const groups = compileNavigationNodes(catalogs, config?.navigationOverrides);

  function resolve(page: PortalPageNavigation): PortalResolvedPageNavigation {
    const groupID = navigationNodeKey(page.parentMenuRef);
    const group = groups.get(groupID);
    if (group === undefined) throw new Error(`导航页面引用了未安装菜单: ${page.id}/${groupID}`);
    return Object.freeze({ ...page, zone: group.zone, groupID });
  }

  function path(page: PortalPageNavigation): ActiveNavigationPath {
    const resolved = resolve(page);
    const group = groups.get(resolved.groupID)!;
    return Object.freeze({ zone: group.zone, rootGroupID: group.parentID ?? group.id, ...(group.parentID === undefined ? {} : { childGroupID: group.id }), pageID: resolved.id });
  }

  return Object.freeze({ groups, resolve, path });
}

function compileNavigationNodes(catalogs: readonly PortalNavigationCatalog[], configured: unknown): ReadonlyMap<string, PortalNavigationGroupDescriptor> {
  const groups = new Map(defaultGroups.map((group) => [group.id, group]));
  for (const catalog of catalogs) {
    for (const node of catalog.nodes) {
      if (groups.has(node.id)) throw new Error(`导航节点全局身份重复: ${node.id}`);
      const parent = resolveDeclaredParent(node.parent, catalogs);
      groups.set(node.id, Object.freeze({ id: node.id, label: node.label, zone: node.zone, icon: node.icon, ...(parent === undefined ? {} : { parentID: navigationNodeKey(parent) }), ...(node.order === undefined ? {} : { order: node.order }) }));
    }
  }
  applyOverrides(groups, configured);
  validateTree(groups);
  return groups;
}

function resolveDeclaredParent(parent: PortalNavigationCatalog["nodes"][number]["parent"], catalogs: readonly PortalNavigationCatalog[]): PortalNavigationNodeRef | undefined {
  if (parent === undefined) return undefined;
  const keys = new Set(catalogs.flatMap((catalog) => catalog.nodes.map((node) => node.id)));
  const target = navigationNodeKey(parent);
  if (keys.has(target)) return parent;
  if (parent.mode === "optional" && parent.fallback !== undefined && keys.has(navigationNodeKey(parent.fallback))) return parent.fallback;
  throw new Error(`导航 required 父级不存在: ${target}`);
}

function applyOverrides(groups: Map<string, PortalNavigationGroupDescriptor>, configured: unknown): void {
  if (configured === undefined) return;
  if (!Array.isArray(configured) || configured.length > 512) throw new Error("shell.config.navigationOverrides 必须是有界数组");
  const seen = new Set<string>();
  for (const value of configured) {
    if (!isRecord(value) || typeof value.target !== "string" || !validGlobalID(value.target) || seen.has(value.target) || !groups.has(value.target) ||
        (value.hidden !== undefined && typeof value.hidden !== "boolean") ||
        (value.order !== undefined && (!Number.isSafeInteger(value.order) || Math.abs(Number(value.order)) > 1_000_000)) ||
        (value.parent !== undefined && (typeof value.parent !== "string" || !validGlobalID(value.parent) || !groups.has(value.parent) || value.parent === value.target)) ||
        (value.labels !== undefined && !validLabels(value.labels))) throw new Error("shell.config.navigationOverrides 包含无效、重复或未知覆盖");
    seen.add(value.target);
    const previous = groups.get(value.target)!;
    groups.set(value.target, Object.freeze({ ...previous, ...(value.hidden === undefined ? {} : { hidden: value.hidden }), ...(value.order === undefined ? {} : { order: Number(value.order) }), ...(value.parent === undefined ? {} : { parentID: value.parent }), ...(value.labels === undefined ? {} : { labels: Object.freeze({ ...value.labels }) }) }));
  }
}

function validateTree(groups: ReadonlyMap<string, PortalNavigationGroupDescriptor>): void {
  for (const descriptor of groups.values()) {
    if (!navigationZones.has(descriptor.zone)) throw new Error(`导航 zone 无效: ${descriptor.id}`);
    if (descriptor.parentID === undefined) continue;
    const parent = groups.get(descriptor.parentID);
    if (parent === undefined || parent.parentID !== undefined) throw new Error(`导航父级未知或深度超过一级菜单、二级菜单、页面: ${descriptor.id}`);
    if (parent.zone !== descriptor.zone) throw new Error(`导航子组不能跨 zone: ${descriptor.id}/${descriptor.parentID}`);
  }
}

export function navigationNodeKey(ref: PortalNavigationNodeRef): string { return `${ref.pluginID}/${ref.nodeID}`; }
function validGlobalID(value: string): boolean { const parts = value.split("/"); return parts.length === 2 && parts.every((part) => /^[a-z0-9][a-z0-9._-]{0,159}$/.test(part)); }
function validLabels(value: unknown): value is Record<string, string> {
  if (!isRecord(value) || Object.keys(value).length === 0 || Object.keys(value).length > 32) return false;
  return Object.entries(value).every(([locale, label]) => canonicalLocale(locale) === locale && typeof label === "string" && label.trim() !== "" && label.length <= 80);
}
function canonicalLocale(value: string): string | undefined { try { return Intl.getCanonicalLocales(value)[0]; } catch { return undefined; } }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
