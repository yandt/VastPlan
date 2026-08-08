import { accountNavigationNodeID, semanticIconNames } from "@vastplan/ui-primitives";
import type {
  NavigationZone,
  PortalNavigationCollection,
  PortalNavigationGroup,
  PortalNavigationIconSpec,
  PortalResolvedPageNavigation,
} from "@vastplan/ui-primitives";

const zones: readonly NavigationZone[] = ["primary", "secondary", "settings"];
const semanticIcons = new Set<string>(semanticIconNames);

interface FolderConfig {
  id: string;
  serviceID: string;
  label: string;
  labels?: Readonly<Record<string, string>>;
  icon?: PortalNavigationIconSpec;
  members: readonly string[];
  order?: number;
}

export function compileNavigationCollections(
  navigation: Readonly<Record<NavigationZone, readonly PortalNavigationGroup[]>>,
  config: Readonly<Record<string, unknown>> | undefined,
): Readonly<Record<NavigationZone, readonly PortalNavigationCollection[]>> {
  const roots = new Map(zones.flatMap((zone) => navigation[zone].map((group) => [group.id, group] as const)));
  const assigned = new Map<string, Set<string>>();
  const collections: Record<NavigationZone, PortalNavigationCollection[]> = { primary: [], secondary: [], settings: [] };

  for (const folder of parseFolders(config?.navigationFolders)) {
    const groups = folder.members.map((member) => {
      const root = roots.get(member);
      if (root === undefined || root.id === accountNavigationNodeID) throw new Error(`导航文件夹引用了未知或受保护 root: ${member}`);
      const scoped = groupForService(root, folder.serviceID);
      if (scoped === undefined) throw new Error(`导航文件夹成员不属于目标服务: ${folder.serviceID}/${member}`);
      return scoped;
    });
    const zone = groups[0]!.zone;
    if (groups.some((group) => group.zone !== zone)) throw new Error(`导航文件夹不能跨 zone: ${folder.id}`);
    for (const group of groups) {
      const services = assigned.get(group.id) ?? new Set<string>();
      if (services.has(folder.serviceID)) throw new Error(`导航 root 在同一服务被重复收纳: ${folder.serviceID}/${group.id}`);
      services.add(folder.serviceID);
      assigned.set(group.id, services);
    }
    const icon = folder.icon ?? Object.freeze({ kind: "composite" as const, items: Object.freeze(groups.slice(0, 4).map((group) => group.icon)) });
    collections[zone].push(Object.freeze({
      kind: "folder",
      id: `folder:${folder.serviceID}/${folder.id}`,
      label: folder.label,
      ...(folder.labels === undefined ? {} : { labels: folder.labels }),
      zone,
      icon,
      ...(folder.order === undefined ? {} : { order: folder.order }),
      groups: Object.freeze(groups),
    }));
  }

  for (const zone of zones) {
    for (const root of navigation[zone]) {
      const remainder = withoutServices(root, assigned.get(root.id));
      if (remainder === undefined) continue;
      collections[zone].push(Object.freeze({
        kind: "group",
        id: `group:${root.id}`,
        label: remainder.label,
        ...(remainder.labels === undefined ? {} : { labels: remainder.labels }),
        zone,
        icon: remainder.icon,
        order: remainder.order,
        groups: Object.freeze([remainder]),
      }));
    }
    collections[zone].sort((left, right) => (left.order ?? 0) - (right.order ?? 0) || left.id.localeCompare(right.id));
  }
  return Object.freeze({
    primary: Object.freeze(collections.primary),
    secondary: Object.freeze(collections.secondary),
    settings: Object.freeze(collections.settings),
  });
}

export function collectionForPage(
  collections: Readonly<Record<NavigationZone, readonly PortalNavigationCollection[]>>,
  pageID: string | undefined,
): PortalNavigationCollection | undefined {
  if (pageID === undefined) return undefined;
  return zones.flatMap((zone) => collections[zone]).find((collection) => collection.groups.some((group) => groupHasPage(group, pageID)));
}

function groupForService(group: PortalNavigationGroup, serviceID: string): PortalNavigationGroup | undefined {
  return filterGroup(group, (page) => page.managementServiceID === serviceID);
}

function withoutServices(group: PortalNavigationGroup, serviceIDs: ReadonlySet<string> | undefined): PortalNavigationGroup | undefined {
  if (serviceIDs === undefined || serviceIDs.size === 0) return group;
  return filterGroup(group, (page) => page.managementServiceID === undefined || !serviceIDs.has(page.managementServiceID));
}

function filterGroup(group: PortalNavigationGroup, include: (page: PortalResolvedPageNavigation) => boolean): PortalNavigationGroup | undefined {
  const pages = group.pages.filter(include);
  const children = group.children.flatMap((child) => {
    const childPages = child.pages.filter(include);
    return childPages.length === 0 ? [] : [Object.freeze({ ...child, pages: Object.freeze(childPages) })];
  });
  if (pages.length === 0 && children.length === 0) return undefined;
  return Object.freeze({ ...group, pages: Object.freeze(pages), children: Object.freeze(children) });
}

function groupHasPage(group: PortalNavigationGroup, pageID: string): boolean {
  return group.pages.some((page) => page.id === pageID) || group.children.some((child) => child.pages.some((page) => page.id === pageID));
}

function parseFolders(value: unknown): readonly FolderConfig[] {
  if (value === undefined) return [];
  if (!Array.isArray(value) || value.length > 128) throw new Error("shell.config.navigationFolders 必须是有界数组");
  const ids = new Set<string>();
  return Object.freeze(value.map((candidate) => {
    if (!isRecord(candidate) || !onlyKeys(candidate, ["id", "serviceId", "label", "labels", "icon", "members", "order"]) ||
        !validID(candidate.id) || !validID(candidate.serviceId) || typeof candidate.label !== "string" || candidate.label.trim() === "" || candidate.label.length > 80 ||
        ids.has(`${candidate.serviceId}/${candidate.id}`) || !Array.isArray(candidate.members) || candidate.members.length < 2 || candidate.members.length > 64 ||
        candidate.members.some((member) => typeof member !== "string" || !validGlobalID(member)) || new Set(candidate.members).size !== candidate.members.length ||
        (candidate.labels !== undefined && !validLabels(candidate.labels)) ||
        (candidate.order !== undefined && (!Number.isSafeInteger(candidate.order) || Math.abs(Number(candidate.order)) > 1_000_000))) {
      throw new Error("shell.config.navigationFolders 包含无效文件夹");
    }
    const icon = parseIcon(candidate.icon);
    ids.add(`${candidate.serviceId}/${candidate.id}`);
    return Object.freeze({
      id: candidate.id,
      serviceID: candidate.serviceId,
      label: candidate.label,
      ...(candidate.labels === undefined ? {} : { labels: Object.freeze({ ...candidate.labels }) }),
      ...(icon === undefined ? {} : { icon }),
      members: Object.freeze([...candidate.members]),
      ...(candidate.order === undefined ? {} : { order: Number(candidate.order) }),
    });
  }));
}

function parseIcon(value: unknown): PortalNavigationIconSpec | undefined {
  if (value === undefined) return undefined;
  if (!isRecord(value) || !onlyKeys(value, ["kind", "name"]) || value.kind !== "semantic" || typeof value.name !== "string" || !semanticIcons.has(value.name)) {
    throw new Error("导航文件夹图标必须来自语义图标词表");
  }
  return Object.freeze({ kind: "semantic", name: value.name as Extract<PortalNavigationIconSpec, { kind: "semantic" }>["name"] });
}

function validID(value: unknown): value is string { return typeof value === "string" && /^[a-z0-9][a-z0-9._-]{0,159}$/.test(value); }
function validGlobalID(value: string): boolean { const parts = value.split("/"); return parts.length === 2 && parts.every(validID); }
function validLabels(value: unknown): value is Record<string, string> {
  return isRecord(value) && Object.keys(value).length > 0 && Object.keys(value).length <= 32 && Object.entries(value).every(([locale, label]) => canonicalLocale(locale) === locale && typeof label === "string" && label.trim() !== "" && label.length <= 80);
}
function canonicalLocale(value: string): string | undefined { try { return Intl.getCanonicalLocales(value)[0]; } catch { return undefined; } }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function onlyKeys(value: Record<string, unknown>, allowed: readonly string[]): boolean { const keys = new Set(allowed); return Object.keys(value).every((key) => keys.has(key)); }
