import type { MenuItem } from "./primitives.js";
import type { PortalI18n } from "./i18n.js";
import { message } from "@vastplan/ui-contract";
import { usePortalI18n } from "./i18n.js";
import { usePortalUI } from "./portal-ui-context.js";
import type { ShellCompositionModel, PortalNavigationGroup, PortalPageNavigation } from "./portal-runtime.js";

const namespace = "cn.vastplan.foundation.frontend.structure.shell";

/** Stable action ID lets every Shell render logout without confusing it with a navigable page. */
export const accountLogoutMenuItemID = "account.logout";

/** Builds the common user menu from the same composed account navigation tree as every Shell. */
export function accountMenuItems(group: PortalNavigationGroup, composition: ShellCompositionModel, i18n: Pick<PortalI18n, "text">, includeLogout: boolean): MenuItem[] {
  const pages = [
    ...group.pages.map((page) => navigationItem(page, composition, i18n)),
    ...group.children.filter((child) => child.pages.length > 0).map((child) => ({
      id: `group:${child.id}`,
      label: i18n.text(child.label),
      children: child.pages.map((page) => navigationItem(page, composition, i18n)),
    })),
  ];
  if (includeLogout) pages.push({ id: accountLogoutMenuItemID, label: i18n.text(message(namespace, "account.logout", "退出登录")) });
  return pages;
}

/** Renderer-owned account menu: pages are navigable, while logout remains a trusted host action. */
export function PortalAccountMenu({ group, composition, activeID, onNavigate, onLogout }: {
  group: PortalNavigationGroup;
  composition: ShellCompositionModel;
  activeID?: string;
  onNavigate(id: string): void;
  onLogout?(): Promise<void>;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const items = accountMenuItems(group, composition, i18n, onLogout !== undefined).map((item) => item.id === accountLogoutMenuItemID
    ? { ...item, icon: <ui.Icon name="logout" size="sm" label={i18n.text(message(namespace, "account.logout", "退出登录"))} /> }
    : item);
  if (items.length === 0) return <div className="vp-account-menu-empty">{i18n.text(message(namespace, "navigation.accountUnavailable", "个人中心尚未装配"))}</div>;
  return <ui.Menu variant="navigation" size="sm" items={items} activeID={activeID} onSelect={(id) => {
    if (id === accountLogoutMenuItemID) { void onLogout?.(); return; }
    onNavigate(id);
  }} />;
}

function navigationItem(page: PortalPageNavigation, composition: ShellCompositionModel, i18n: Pick<PortalI18n, "text">): MenuItem {
  return { id: page.id, label: i18n.text(page.label), href: composition.pages.find((candidate) => candidate.navigation?.id === page.id)?.path };
}
