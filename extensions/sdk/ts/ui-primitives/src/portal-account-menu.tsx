import { message } from "@vastplan/ui-contract";
import { usePortalI18n } from "./i18n.js";
import { PortalNavigationMenu } from "./portal-navigation-menu.js";
import type { ShellCompositionModel, PortalNavigationGroup } from "./portal-runtime.js";

const namespace = "cn.vastplan.foundation.frontend.structure.shell";

export { accountLogoutMenuItemID, accountMenuItems } from "./portal-navigation-menu.js";

/** Renderer-owned account menu: pages are navigable, while logout remains a trusted host action. */
export function PortalAccountMenu({ group, composition, activeID, onNavigate, onLogout }: {
  group: PortalNavigationGroup;
  composition: ShellCompositionModel;
  activeID?: string;
  onNavigate(id: string): void;
  onLogout?(): Promise<void>;
}) {
  const i18n = usePortalI18n();
  return <PortalNavigationMenu groups={[group]} composition={composition} activeID={activeID} onNavigate={onNavigate} onLogout={onLogout} empty={<div className="vp-account-menu-empty">{i18n.text(message(namespace, "navigation.accountUnavailable", "个人中心尚未装配"))}</div>} />;
}
