import { accountNavigationGroupID, accountSettingsNavigationGroupID, message, semanticIconNames } from "@vastplan/ui-primitives";
import type {
  ActiveNavigationPath,
  NavigationZone,
  PortalNavigationGroupDescriptor,
  PortalPageNavigation,
  SemanticIconName,
} from "@vastplan/ui-primitives";

const namespace = "cn.vastplan.foundation.frontend.structure.shell";
const navigationZones = new Set<NavigationZone>(["primary", "secondary", "settings"]);
const semanticIcons = new Set<SemanticIconName>(semanticIconNames);

const defaultGroups: readonly PortalNavigationGroupDescriptor[] = [
  { id: "primary", label: message(namespace, "navigation.primary", "主要功能"), zone: "primary", icon: "menu", order: 10 },
  { id: "secondary", label: message(namespace, "navigation.secondary", "辅助功能"), zone: "secondary", icon: "info", order: 20 },
  { id: "settings", label: message(namespace, "navigation.settings", "系统管理"), zone: "settings", icon: "settings", order: 100 },
  { id: accountNavigationGroupID, label: message(namespace, "navigation.account", "用户"), zone: "secondary", icon: "info", order: 1000 },
  { id: accountSettingsNavigationGroupID, parentID: accountNavigationGroupID, label: message(namespace, "navigation.accountSettings", "用户设置"), zone: "secondary", icon: "settings", order: 20 },
];

export interface NavigationPolicy {
  readonly groups: ReadonlyMap<string, PortalNavigationGroupDescriptor>;
  resolve(page: PortalPageNavigation): PortalPageNavigation;
  path(page: PortalPageNavigation): ActiveNavigationPath;
}

/**
 * Compiles one trusted Portal Profile navigation policy. Functional plugins
 * declare a stable semanticID and a self-contained fallback placement; only
 * this compiled policy may remap that intent for the current Portal.
 */
export function compileNavigationPolicy(config: Readonly<Record<string, unknown>> | undefined): NavigationPolicy {
  const groups = navigationGroups(config?.navigationGroups);
  const placements = navigationPlacements(config?.navigationPlacements, groups);

  function resolve(page: PortalPageNavigation): PortalPageNavigation {
    if (page.semanticID !== undefined && !validID(page.semanticID)) throw new Error(`导航语义 id 无效: ${page.id}`);
    const governedGroupID = page.semanticID === undefined ? undefined : placements.get(page.semanticID);
    const groupID = governedGroupID ?? page.groupID ?? page.zone;
    const group = groups.get(groupID);
    if (group === undefined) throw new Error(`导航引用了未治理的分组: ${groupID}`);
    if (governedGroupID === undefined && group.zone !== page.zone) throw new Error(`导航分组与语义区不一致: ${page.id}/${groupID}`);
    return Object.freeze({ ...page, zone: group.zone, groupID });
  }

  function path(page: PortalPageNavigation): ActiveNavigationPath {
    const resolved = resolve(page);
    const group = groups.get(resolved.groupID ?? resolved.zone);
    if (group === undefined) throw new Error(`导航引用了未治理的分组: ${resolved.groupID ?? resolved.zone}`);
    return Object.freeze({
      zone: group.zone,
      rootGroupID: group.parentID ?? group.id,
      ...(group.parentID === undefined ? {} : { childGroupID: group.id }),
      pageID: resolved.id,
    });
  }

  return Object.freeze({ groups, resolve, path });
}

function navigationGroups(configured: unknown): ReadonlyMap<string, PortalNavigationGroupDescriptor> {
  const groups = new Map(defaultGroups.map((group) => [group.id, group]));
  if (configured === undefined) return groups;
  if (!Array.isArray(configured)) throw new Error("composition.config.navigationGroups 必须是数组");
  const configuredIDs = new Set<string>();
  for (const value of configured) {
    if (!isRecord(value) || typeof value.id !== "string" || !validID(value.id) || configuredIDs.has(value.id) ||
        typeof value.label !== "string" || value.label.trim() === "" || value.label.length > 80 ||
        typeof value.zone !== "string" || !navigationZones.has(value.zone as NavigationZone) ||
        typeof value.icon !== "string" || !semanticIcons.has(value.icon as SemanticIconName) ||
        (value.parentID !== undefined && (typeof value.parentID !== "string" || !validID(value.parentID) || value.parentID === value.id)) ||
        (value.order !== undefined && (!Number.isSafeInteger(value.order) || Math.abs(value.order as number) > 1_000_000))) {
      throw new Error("composition.config.navigationGroups 包含无效或重复分组");
    }
    const previous = groups.get(value.id);
    if (previous !== undefined && (previous.zone !== value.zone || value.parentID !== undefined)) throw new Error(`内建导航分组不能跨语义区或改为子组: ${value.id}`);
    configuredIDs.add(value.id);
    groups.set(value.id, Object.freeze({ id: value.id, label: value.label.trim(), zone: value.zone as NavigationZone, icon: value.icon as SemanticIconName, parentID: value.parentID as string | undefined, order: value.order as number | undefined }));
  }
  for (const descriptor of groups.values()) {
    if (descriptor.parentID === undefined) continue;
    const parent = groups.get(descriptor.parentID);
    if (parent === undefined) throw new Error(`导航子组引用了未知根组: ${descriptor.id}/${descriptor.parentID}`);
    if (parent.parentID !== undefined) throw new Error(`导航深度超过 root group → child group → page: ${descriptor.id}`);
    if (parent.zone !== descriptor.zone) throw new Error(`导航子组不能跨语义区: ${descriptor.id}/${descriptor.parentID}`);
  }
  return groups;
}

function navigationPlacements(configured: unknown, groups: ReadonlyMap<string, PortalNavigationGroupDescriptor>): ReadonlyMap<string, string> {
  const placements = new Map<string, string>();
  if (configured === undefined) return placements;
  if (!Array.isArray(configured)) throw new Error("composition.config.navigationPlacements 必须是数组");
  for (const value of configured) {
    if (!isRecord(value) || Object.keys(value).some((key) => key !== "semanticID" && key !== "groupID") ||
        typeof value.semanticID !== "string" || !validID(value.semanticID) || placements.has(value.semanticID) ||
        typeof value.groupID !== "string" || !validID(value.groupID)) {
      throw new Error("composition.config.navigationPlacements 包含无效或重复映射");
    }
    if (!groups.has(value.groupID)) throw new Error(`导航语义映射引用了未知分组: ${value.semanticID}/${value.groupID}`);
    placements.set(value.semanticID, value.groupID);
  }
  return placements;
}

function validID(value: string): boolean {
  return /^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$/.test(value);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
