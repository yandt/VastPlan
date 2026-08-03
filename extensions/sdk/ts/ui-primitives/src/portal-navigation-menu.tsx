import type { ReactNode } from "react";
import { message } from "@vastplan/ui-contract";
import type { PortalI18n } from "./i18n.js";
import type { MenuItem, MenuProps } from "./primitives.js";
import { usePortalI18n } from "./i18n.js";
import { usePortalUI } from "./portal-ui-context.js";
import { accountNavigationNodeID, type PortalNavigationGroup, type PortalPageNavigation, type ShellCompositionModel } from "./portal-runtime.js";

const namespace = "cn.vastplan.foundation.frontend.structure.shell";

/** Stable action ID lets every Shell render logout without confusing it with a navigable page. */
export const accountLogoutMenuItemID = "account.logout";

export interface PortalNavigationMenuProps {
  groups: readonly PortalNavigationGroup[];
  composition: ShellCompositionModel;
  activeID?: string;
  /** The Shell selects placement; the Renderer owns the corresponding DOM and interaction. */
  presentation?: MenuProps["presentation"];
  expandedIDs?: readonly string[];
  onExpandedChange?(ids: readonly string[]): void;
  onNavigate(id: string): void;
  onLogout?(): Promise<void>;
  empty?: ReactNode;
}

/**
 * Converts the one composed navigation model into renderer-neutral Menu data.
 * A trusted logout action belongs to the account root only; pages remain ordinary leaves.
 */
export function composedNavigationMenuItems(groups: readonly PortalNavigationGroup[], composition: ShellCompositionModel, i18n: Pick<PortalI18n, "text" | "locale">, includeLogout: boolean): MenuItem[] {
  if (groups.length === 1) return menuItemsForGroup(groups[0], composition, i18n, includeLogout);
  return groups.map((group) => ({
    id: `group:${group.id}`,
    label: navigationLabel(group, i18n),
    children: menuItemsForGroup(group, composition, i18n, includeLogout),
  }));
}

/** Backward-compatible account-only data helper. */
export function accountMenuItems(group: PortalNavigationGroup, composition: ShellCompositionModel, i18n: Pick<PortalI18n, "text">, includeLogout: boolean): MenuItem[] {
  const items = groupNavigationItems(group, composition, i18n);
  if (includeLogout) items.push({ id: accountLogoutMenuItemID, label: i18n.text(message(namespace, "account.logout", "退出登录")) });
  return items;
}

/**
 * The only shared visual bridge from the composed navigation model to the current Renderer.
 * Shell Libraries select inline versus popup placement but never recreate navigation rows themselves.
 */
export function PortalNavigationMenu({ groups, composition, activeID, presentation = "popup", expandedIDs, onExpandedChange, onNavigate, onLogout, empty }: PortalNavigationMenuProps) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const items = composedNavigationMenuItems(groups, composition, i18n, onLogout !== undefined);
  if (items.length === 0) return <>{empty ?? null}</>;
  return <ui.Menu
    variant="navigation"
    size="sm"
    presentation={presentation}
    items={items}
    activeID={activeID}
    expandedIDs={expandedIDs}
    onExpandedChange={onExpandedChange}
    onSelect={(id) => {
      if (id === accountLogoutMenuItemID) {
        void onLogout?.();
        return;
      }
      onNavigate(id);
    }}
  />;
}

function menuItemsForGroup(group: PortalNavigationGroup, composition: ShellCompositionModel, i18n: Pick<PortalI18n, "text">, includeLogout: boolean): MenuItem[] {
  return group.id === accountNavigationNodeID && includeLogout
    ? accountMenuItems(group, composition, i18n, true)
    : groupNavigationItems(group, composition, i18n);
}

function groupNavigationItems(group: PortalNavigationGroup, composition: ShellCompositionModel, i18n: Pick<PortalI18n, "text">): MenuItem[] {
  return [
    ...group.pages.map((page) => navigationPageItem(page, composition, i18n)),
    ...group.children.filter((child) => child.pages.length > 0).map((child) => ({
      id: `group:${child.id}`,
      label: i18n.text(child.label),
      children: child.pages.map((page) => navigationPageItem(page, composition, i18n)),
    })),
  ];
}

function navigationPageItem(page: PortalPageNavigation, composition: ShellCompositionModel, i18n: Pick<PortalI18n, "text">): MenuItem {
  return { id: page.id, label: i18n.text(page.label), href: composition.pages.find((candidate) => candidate.navigation?.id === page.id)?.path };
}

function navigationLabel(group: PortalNavigationGroup, i18n: Pick<PortalI18n, "text" | "locale">): string {
  return group.labels?.[i18n.locale] ?? i18n.text(group.label);
}
