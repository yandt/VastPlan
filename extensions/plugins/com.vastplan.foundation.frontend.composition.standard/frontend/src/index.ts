import { pageSlotIDs, semanticIconNames, shellSlotIDs } from "@vastplan/portal-ui";
import type {
  NavigationZone,
  PageSlotID,
  PortalNavigationGroup,
  PortalNavigationGroupDescriptor,
  PortalPageNavigation,
  PortalPageSlotContribution,
  PortalRegisteredShellContribution,
  SemanticIconName,
  ShellCompositionAdapter,
  ShellCompositionInput,
  ShellCompositionModel,
  ShellSlotID,
} from "@vastplan/portal-ui";

const shellSlots = new Set<ShellSlotID>(shellSlotIDs);
const pageSlots = new Set<PageSlotID>(pageSlotIDs);
const navigationZones = new Set<NavigationZone>(["primary", "secondary", "settings"]);
const semanticIcons = new Set<SemanticIconName>(semanticIconNames);
const defaultGroups: readonly PortalNavigationGroupDescriptor[] = [
  { id: "primary", label: "主要功能", zone: "primary", icon: "menu", order: 10 },
  { id: "secondary", label: "辅助功能", zone: "secondary", icon: "info", order: 20 },
  { id: "settings", label: "系统设置", zone: "settings", icon: "settings", order: 100 },
];

function ordered<T extends { id: string; order?: number }>(values: readonly T[]): readonly T[] {
  return [...values].sort((left, right) => (left.order ?? 0) - (right.order ?? 0) || left.id.localeCompare(right.id));
}

function compose(input: ShellCompositionInput): ShellCompositionModel {
  const pages = Object.freeze([...input.pages]);
  const activePage = pages.find((page) => page.id === input.activePageID);
  const descriptors = navigationGroups(input.config);
  const pagesByGroup = new Map<string, PortalPageNavigation[]>();
  for (const page of pages) {
    const navigation = page.navigation;
    if (navigation === undefined) continue;
    const groupID = navigation.groupID ?? navigation.zone;
    const descriptor = descriptors.get(groupID);
    if (descriptor === undefined) throw new Error(`导航引用了未治理的分组: ${groupID}`);
    if (descriptor.zone !== navigation.zone) throw new Error(`导航分组与语义区不一致: ${navigation.id}/${groupID}`);
    let groupPages = pagesByGroup.get(groupID);
    if (groupPages === undefined) {
      groupPages = [];
      pagesByGroup.set(groupID, groupPages);
    }
    groupPages.push(navigation);
  }

  const navigation: Record<NavigationZone, PortalNavigationGroup[]> = { primary: [], settings: [], secondary: [] };
  for (const descriptor of descriptors.values()) {
    const groupPages = ordered(pagesByGroup.get(descriptor.id) ?? []);
    if (groupPages.length === 0) continue;
    navigation[descriptor.zone].push(Object.freeze({ ...descriptor, pages: Object.freeze(groupPages) }));
  }
  for (const zone of navigationZones) navigation[zone] = [...ordered(navigation[zone])];

  const shellGrouped: Partial<Record<ShellSlotID, PortalRegisteredShellContribution[]>> = {};
  for (const contribution of input.shellContributions) {
    if (!shellSlots.has(contribution.slot)) throw new Error(`不支持的 Shell Slot: ${String(contribution.slot)}`);
    (shellGrouped[contribution.slot] ??= []).push(contribution);
  }
  const shellNormalized: Partial<Record<ShellSlotID, readonly PortalRegisteredShellContribution[]>> = {};
  for (const [slot, contributions] of Object.entries(shellGrouped)) shellNormalized[slot as ShellSlotID] = Object.freeze(ordered(contributions));

  const pageGrouped: Partial<Record<PageSlotID, PortalPageSlotContribution[]>> = {};
  for (const contribution of activePage?.slots ?? []) {
    if (!pageSlots.has(contribution.slot)) throw new Error(`不支持的 Page Slot: ${String(contribution.slot)}`);
    (pageGrouped[contribution.slot] ??= []).push(contribution);
  }
  const pageNormalized: Partial<Record<PageSlotID, readonly PortalPageSlotContribution[]>> = {};
  for (const [slot, contributions] of Object.entries(pageGrouped)) pageNormalized[slot as PageSlotID] = Object.freeze(ordered(contributions));

  return Object.freeze({
    pages,
    activePage,
    navigation: Object.freeze({
      primary: Object.freeze(navigation.primary),
      settings: Object.freeze(navigation.settings),
      secondary: Object.freeze(navigation.secondary),
    }),
    shellSlots: Object.freeze(shellNormalized),
    pageSlots: Object.freeze(pageNormalized),
  });
}

function navigationGroups(config: Readonly<Record<string, unknown>> | undefined): ReadonlyMap<string, PortalNavigationGroupDescriptor> {
  const groups = new Map(defaultGroups.map((group) => [group.id, group]));
  const configured = config?.navigationGroups;
  if (configured === undefined) return groups;
  if (!Array.isArray(configured)) throw new Error("composition.config.navigationGroups 必须是数组");
  const configuredIDs = new Set<string>();
  for (const value of configured) {
    if (!isRecord(value) || typeof value.id !== "string" || !validID(value.id) || configuredIDs.has(value.id) ||
        typeof value.label !== "string" || value.label.trim() === "" || value.label.length > 80 ||
        typeof value.zone !== "string" || !navigationZones.has(value.zone as NavigationZone) ||
        typeof value.icon !== "string" || !semanticIcons.has(value.icon as SemanticIconName) ||
        (value.order !== undefined && (!Number.isSafeInteger(value.order) || Math.abs(value.order as number) > 1_000_000))) {
      throw new Error("composition.config.navigationGroups 包含无效或重复分组");
    }
    const previous = groups.get(value.id);
    if (previous !== undefined && previous.zone !== value.zone) throw new Error(`内建导航分组不能跨语义区覆盖: ${value.id}`);
    configuredIDs.add(value.id);
    groups.set(value.id, Object.freeze({ id: value.id, label: value.label.trim(), zone: value.zone as NavigationZone, icon: value.icon as SemanticIconName, order: value.order as number | undefined }));
  }
  return groups;
}

function validID(value: string): boolean {
  return /^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$/.test(value);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

const adapter: ShellCompositionAdapter = { id: "ui.shell-composition", uiContract: "1.0.0", compose };
export default adapter;
