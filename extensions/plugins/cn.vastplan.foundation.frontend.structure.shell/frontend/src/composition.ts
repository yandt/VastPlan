import { accountNavigationGroupID, pageSlotIDs, shellSlotIDs } from "@vastplan/ui-primitives";
import type {
  NavigationZone,
  PageSlotID,
  PortalNavigationGroup,
  PortalNavigationChildGroup,
  PortalPageNavigation,
  PortalResolvedPageNavigation,
  PortalPageSlotContribution,
  PortalRegisteredShellContribution,
  ShellCompositionInput,
  ShellCompositionModel,
  ShellSlotID,
} from "@vastplan/ui-primitives";
import { compileNavigationPolicy } from "./navigation-policy";

const shellSlots = new Set<ShellSlotID>(shellSlotIDs);
const pageSlots = new Set<PageSlotID>(pageSlotIDs);
const navigationZones = new Set<NavigationZone>(["primary", "secondary", "settings"]);

function ordered<T extends { id: string; order?: number }>(values: readonly T[]): readonly T[] {
  return [...values].sort((left, right) => (left.order ?? 0) - (right.order ?? 0) || left.id.localeCompare(right.id));
}

export function compose(input: ShellCompositionInput): ShellCompositionModel {
  const pages = Object.freeze([...input.pages]);
  const activePage = pages.find((page) => page.id === input.activePageID);
  const policy = compileNavigationPolicy(input.navigationCatalogs, input.config);
  const descriptors = policy.groups;
  const pagesByGroup = new Map<string, PortalResolvedPageNavigation[]>();
  for (const page of pages) {
    if (page.navigation === undefined) continue;
    const navigation = policy.resolve(page.navigation);
    const groupID = navigation.groupID;
    let groupPages = pagesByGroup.get(groupID);
    if (groupPages === undefined) {
      groupPages = [];
      pagesByGroup.set(groupID, groupPages);
    }
    groupPages.push(navigation);
  }

  const navigation: Record<NavigationZone, PortalNavigationGroup[]> = { primary: [], settings: [], secondary: [] };
  for (const descriptor of descriptors.values()) {
    if (descriptor.parentID !== undefined || descriptor.hidden === true) continue;
    const rootPages = ordered(pagesByGroup.get(descriptor.id) ?? []);
    const children: PortalNavigationChildGroup[] = [];
    for (const child of descriptors.values()) {
      if (child.parentID !== descriptor.id || child.hidden === true) continue;
      const childPages = ordered(pagesByGroup.get(child.id) ?? []);
      if (childPages.length === 0) continue;
      children.push(Object.freeze({ ...child, parentID: descriptor.id, pages: Object.freeze(childPages) }));
    }
    const orderedChildren = ordered(children);
    // 账户根分组同时承载 Shell 的稳定身份入口。即使账户功能插件尚未装配，
    // 也必须保留该分组，让布局继续显示头像并给出明确的未装配状态。
    if (rootPages.length === 0 && orderedChildren.length === 0 && descriptor.id !== `vastplan.host/${accountNavigationGroupID}`) continue;
    navigation[descriptor.zone].push(Object.freeze({ ...descriptor, parentID: undefined, pages: Object.freeze(rootPages), children: Object.freeze(orderedChildren) }));
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

  const activeNavigationPath = activePage?.navigation === undefined ? undefined : policy.path(activePage.navigation);
  return Object.freeze({
    pages,
    activePage,
    activeNavigationPath,
    navigation: Object.freeze({
      primary: Object.freeze(navigation.primary),
      settings: Object.freeze(navigation.settings),
      secondary: Object.freeze(navigation.secondary),
    }),
    shellSlots: Object.freeze(shellNormalized),
    pageSlots: Object.freeze(pageNormalized),
  });
}
