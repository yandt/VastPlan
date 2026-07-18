import type { NavigationZone, PortalPageNavigation, PortalSlotContribution, ShellCompositionAdapter, ShellCompositionInput, ShellCompositionModel, ShellSlotID } from "@vastplan/portal-ui";

const slots = new Set<ShellSlotID>([
  "shell.header.start", "shell.header.center", "shell.header.end",
  "shell.navigation.before", "shell.navigation.after",
  "page.header.start", "page.header.center", "page.header.end",
  "page.body.before", "page.body.main", "page.body.after",
  "page.aside", "shell.footer",
]);

function ordered<T extends { id: string; order?: number }>(values: readonly T[]): readonly T[] {
  return [...values].sort((left, right) => (left.order ?? 0) - (right.order ?? 0) || left.id.localeCompare(right.id));
}

function compose(input: ShellCompositionInput): ShellCompositionModel {
  const pages = Object.freeze([...input.pages]);
  const activePage = pages.find((page) => page.id === input.activePageID);
  const navigation: Record<NavigationZone, PortalPageNavigation[]> = { primary: [], settings: [], secondary: [] };
  for (const page of pages) if (page.navigation !== undefined) navigation[page.navigation.zone].push(page.navigation);

  const grouped: Partial<Record<ShellSlotID, PortalSlotContribution[]>> = {};
  for (const contribution of activePage?.slots ?? []) {
    if (!slots.has(contribution.slot)) throw new Error(`不支持的 Shell Slot: ${String(contribution.slot)}`);
    (grouped[contribution.slot] ??= []).push(contribution);
  }
  const normalized: Partial<Record<ShellSlotID, readonly PortalSlotContribution[]>> = {};
  for (const [slot, contributions] of Object.entries(grouped)) normalized[slot as ShellSlotID] = ordered(contributions);
  return Object.freeze({
    pages,
    activePage,
    navigation: Object.freeze({ primary: ordered(navigation.primary), settings: ordered(navigation.settings), secondary: ordered(navigation.secondary) }),
    slots: Object.freeze(normalized),
  });
}

const adapter: ShellCompositionAdapter = { id: "ui.shell-composition", uiContract: "1.0.0", compose };
export default adapter;
