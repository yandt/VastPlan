import { createElement, useEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent, type ReactNode } from "react";
import { uiContractVersion } from "@vastplan/ui-contract";
import {
  accountNavigationNodeID,
  accountLogoutMenuItemID,
  accountMenuItems,
  message,
  NavigationIcon,
  portalPageRhythm,
  PortalAccountControl,
  resolvePageBodyMaxWidth,
  usePortalI18n,
  usePortalUI,
  type MenuItem,
  type NavigationZone,
  type PageSlotID,
  type PortalNavigationGroup,
  type UIShellProps,
  type ShellSlotID,
} from "@vastplan/ui-primitives";
import { hasRegionContent } from "./region-visibility";

const shellHeaderSlots = ["shell.header.start", "shell.header.center", "shell.header.end"] as const;
const shellNavigationSlots = ["shell.navigation.start", "shell.navigation.center", "shell.navigation.end"] as const;

export function StandardShell(props: UIShellProps) {
  const { composition, branding, template, pathname, recoveryNotice, onNavigate } = props;
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const [mobileOpen, setMobileOpen] = useState(false);
  const shellTheme = {
    "--vp-shell-canvas": ui.theme.tokens.color.canvas,
    "--vp-shell-surface": ui.theme.tokens.color.surface,
    "--vp-shell-text": ui.theme.tokens.color.text,
    "--vp-shell-muted": ui.theme.tokens.color.mutedText,
    "--vp-shell-border": ui.theme.tokens.color.border,
    "--vp-shell-primary": ui.theme.tokens.color.primary,
    "--vp-shell-hover": ui.theme.tokens.color.hover,
    "--vp-shell-selected": ui.theme.tokens.color.selected,
    "--vp-shell-focus": ui.theme.tokens.color.focusRing,
    "--vp-shell-bar-height": `${ui.theme.tokens.shell.barHeight}px`,
    "--vp-shell-rail-width": `${ui.theme.tokens.shell.railWidth}px`,
    "--vp-shell-navigation-width": `${ui.theme.tokens.shell.navigationWidth}px`,
    "--vp-shell-navigation-compact-width": `${ui.theme.tokens.shell.navigationCompactWidth}px`,
    "--vp-shell-focus-width": `${ui.theme.tokens.focus.width}px`,
    "--vp-shell-touch-minimum": `${ui.theme.tokens.touch.minimum}px`,
    "--vp-shell-motion-fast": `${ui.theme.tokens.motion.fast}ms`,
    "--vp-page-content-start": `${portalPageRhythm.contentStart}px`,
  } as CSSProperties;
  const shellPageContained = template.options.pageBodyWidth === "contained";
  const allGroups = useMemo(() => groups(composition, ["primary", "secondary", "settings"]), [composition]);
  const activeGroup = allGroups.find((group) => group.id === composition.activeNavigationPath?.rootGroupID);
  const groupKey = allGroups.map((group) => group.id).join("\u0000");
  const [selectedGroupID, setSelectedGroupID] = useState(activeGroup?.id ?? allGroups[0]?.id);
  const [pendingGroupNavigationID, setPendingGroupNavigationID] = useState<string>();
  useEffect(() => {
    setSelectedGroupID((selected) => activeGroup?.id ?? (allGroups.some((group) => group.id === selected) ? selected : allGroups[0]?.id));
  }, [activeGroup?.id, groupKey]);
  const selectedGroup = allGroups.find((group) => group.id === selectedGroupID) ?? allGroups[0];
  const shellHeaderVisible = hasRegionContent(composition, { shellSlots: shellHeaderSlots });
  const navigationVisible = hasRegionContent(composition, { intrinsic: branding.name !== "", navigation: true, shellSlots: shellNavigationSlots });
  const accountGroup = composition.navigation.secondary.find((group) => group.id === accountNavigationNodeID);
  // 系统管理仍保留 settings 语义区，便于权限裁剪与其他 Shell 使用同一组合模型；
  // 标准侧栏仅改变其视觉位置，使其成为图标主轨中最后一个一级入口。
  const mainGroups = navigationRailGroups(composition);
  const brand = <Brand name={branding.name} shortName={branding.shortName} logoURL={branding.logoURL} compact />;

  const header = shellHeaderVisible ? <header className="vp-shell-header">
    <div className="vp-shell-header-side">{shellSlot(composition.shellSlots, "shell.header.start")}</div>
    <div className="vp-shell-header-center">{shellSlot(composition.shellSlots, "shell.header.center")}</div>
    <div className="vp-shell-header-side vp-shell-header-end">{shellSlot(composition.shellSlots, "shell.header.end")}</div>
  </header> : null;

  const navigate = (navigationID: string) => {
    const page = composition.pages.find((candidate) => candidate.navigation?.id === navigationID);
    if (page === undefined) return;
    onNavigate(page.id);
    setMobileOpen(false);
  };
  const selectGroup = (id: string) => {
    if (id === selectedGroup?.id) return;
    setSelectedGroupID(id);
    setPendingGroupNavigationID(id);
  };
  useEffect(() => {
    if (pendingGroupNavigationID === undefined || selectedGroup?.id !== pendingGroupNavigationID) return;
    setPendingGroupNavigationID(undefined);
    const firstPageID = firstNavigablePageID(selectedGroup);
    if (firstPageID !== undefined) navigate(firstPageID);
  }, [pendingGroupNavigationID, selectedGroup]);

  const page = composition.activePage;
  const pageWidth = resolvePageBodyMaxWidth(page?.bodyLayout, shellPageContained);
  const pageHeader = page === undefined ? null : <header className="vp-page-header">
    <div className="vp-page-header-side">{pageSlot(composition.pageSlots, "page.header.start")}<div className="vp-page-title-copy"><h1 className="vp-page-title" tabIndex={-1}>{i18n.text(page.title)}</h1>{page.description === undefined ? null : <p className="vp-page-description">{i18n.text(page.description)}</p>}</div></div>
    <div className="vp-page-header-center">{pageSlot(composition.pageSlots, "page.header.center")}</div>
    <div className="vp-page-header-side vp-page-header-end">{pageSlot(composition.pageSlots, "page.header.end")}</div>
  </header>;
  const pageBody = <div className="vp-page-scroller"><main className="vp-page" data-page-body-layout={page?.bodyLayout ?? "large"} style={{ maxWidth: pageWidth }}>
    {recoveryNotice}
    {page === undefined ? <ui.EmptyState title={i18n.text(message(namespace, "page.notFound", "页面不存在"))} description={i18n.text(message(namespace, "page.pathMissing", "Portal 没有注册路径 {path}", { path: pathname }))} /> : <>
      {pageSlot(composition.pageSlots, "page.body.before")}
      <div className="vp-page-body-row"><section className="vp-page-body-main">{pageSlot(composition.pageSlots, "page.body.main")}</section>{hasRegionContent(composition, { pageSlots: ["page.aside"] }) ? <aside className="vp-page-aside">{pageSlot(composition.pageSlots, "page.aside")}</aside> : null}</div>
      {pageSlot(composition.pageSlots, "page.body.after")}
    </>}
  </main></div>;

  const mobileItems: MenuItem[] = allGroups.map((group) => ({
    id: `group:${group.id}`,
    label: navigationLabel(group, i18n),
    icon: <NavigationIcon icon={group.icon} label={navigationLabel(group, i18n)} />,
    children: group.id === accountNavigationNodeID ? accountMenuItems(group, composition, i18n, props.onLogout !== undefined) : [
      ...group.pages.map((item) => ({ id: item.id, label: i18n.text(item.label), href: pagePath(composition, item.id) })),
      ...group.children.map((child) => ({ id: `group:${child.id}`, label: i18n.text(child.label), children: child.pages.map((item) => ({ id: item.id, label: i18n.text(item.label), href: pagePath(composition, item.id) })) })),
    ],
  }));
  const selectMobileMenu = (id: string) => {
    if (id === accountLogoutMenuItemID) {
      void props.onLogout?.();
      return;
    }
    navigate(id);
  };

  return <div className="vp-shell-root" style={shellTheme}>
    <style>{standardShellCSS}</style>
    {header}
    <div className="vp-shell-frame">
      {navigationVisible ? <DesktopNavigation
        branding={brand}
        composition={composition}
        mainGroups={mainGroups}
        selectedGroup={selectedGroup}
        account={accountGroup === undefined ? null : <PortalAccountControl account={props.account} selected={selectedGroup?.id === accountGroup.id} onSelect={() => selectGroup(accountGroup.id)} />}
        onSelectGroup={selectGroup}
        onNavigate={navigate}
        onLogout={props.onLogout}
      /> : null}
      <div className="vp-shell-content">
        {navigationVisible ? <div className="vp-mobile-header"><button type="button" className="vp-mobile-menu-button" aria-label={i18n.text(message(namespace, "navigation.open", "打开主菜单"))} onClick={() => setMobileOpen(true)}><ui.Icon name="menu" /></button><Brand name={branding.name} shortName={branding.shortName} logoURL={branding.logoURL} /><span className="vp-mobile-preferences"><PortalAccountControl account={props.account} onSelect={() => setMobileOpen(true)} /></span></div> : null}
        {pageHeader}
        {pageBody}
      </div>
    </div>
    {hasRegionContent(composition, { shellSlots: ["shell.footer"] }) ? <footer className="vp-shell-footer">{shellSlot(composition.shellSlots, "shell.footer")}</footer> : null}
    <ui.Drawer open={mobileOpen} title={branding.name} placement="left" width="sm" onClose={() => setMobileOpen(false)}>
      <nav aria-label={i18n.text(message(namespace, "navigation.mobile", "移动主菜单"))}><ui.Menu items={mobileItems} activeID={page?.navigation?.id} onSelect={selectMobileMenu} /></nav>
    </ui.Drawer>
  </div>;
}

function DesktopNavigation({ branding, composition, mainGroups, selectedGroup, account, onSelectGroup, onNavigate, onLogout }: {
  branding: ReactNode;
  composition: UIShellProps["composition"];
  mainGroups: readonly PortalNavigationGroup[];
  selectedGroup: PortalNavigationGroup | undefined;
  account: ReactNode;
  onSelectGroup(id: string): void;
  onNavigate(id: string): void;
  onLogout?(): Promise<void>;
}) {
  const i18n = usePortalI18n();
  const panelRef = useRef<HTMLElement>(null);
  const selectedButtonRef = useRef<HTMLButtonElement>(null);
  const panelID = selectedGroup === undefined ? undefined : `vp-navigation-panel-${selectedGroup.id}`;
  const focusPanel = () => panelRef.current?.querySelector<HTMLElement>("button, a, [tabindex]:not([tabindex='-1'])")?.focus();
  const groupButton = (group: PortalNavigationGroup) => <RailButton
    key={group.id}
    group={group}
    selected={group.id === selectedGroup?.id}
    controls={group.id === selectedGroup?.id ? panelID : undefined}
    buttonRef={group.id === selectedGroup?.id ? selectedButtonRef : undefined}
    onSelect={() => onSelectGroup(group.id)}
    onOpen={focusPanel}
  />;
  return <div className="vp-desktop-navigation">
    <aside className="vp-navigation-rail" aria-label={i18n.text(message(namespace, "navigation.groups", "主菜单分组"))} onKeyDown={moveRailFocus}>
      <div className="vp-navigation-start">{branding}{shellSlot(composition.shellSlots, "shell.navigation.start")}</div>
      <div className="vp-navigation-center">{shellSlot(composition.shellSlots, "shell.navigation.center")}{mainGroups.map(groupButton)}</div>
      <div className="vp-navigation-end">{shellSlot(composition.shellSlots, "shell.navigation.end")}<div className="vp-navigation-account">{account}</div></div>
    </aside>
    {selectedGroup === undefined ? null : <aside id={panelID} ref={panelRef} className="vp-navigation-panel" aria-label={i18n.text(message(namespace, "navigation.secondaryLabel", "{group}二级导航", { group: navigationLabel(selectedGroup, i18n) }))} onKeyDown={(event) => returnToRail(event, selectedButtonRef)}>
      <header className="vp-navigation-panel-header"><span className="vp-navigation-panel-icon"><IconForGroup group={selectedGroup} /></span><strong>{navigationLabel(selectedGroup, i18n)}</strong></header>
      <nav className="vp-navigation-panel-body" aria-label={navigationLabel(selectedGroup, i18n)}>
        <SecondLevelMenu group={selectedGroup} composition={composition} onNavigate={onNavigate} onLogout={onLogout} />
      </nav>
    </aside>}
  </div>;
}

function RailButton({ group, selected, controls, buttonRef, onSelect, onOpen }: {
  group: PortalNavigationGroup;
  selected: boolean;
  controls?: string;
  buttonRef?: React.RefObject<HTMLButtonElement>;
  onSelect(): void;
  onOpen(): void;
}) {
  const i18n = usePortalI18n();
  const label = navigationLabel(group, i18n);
  return <button ref={buttonRef} type="button" className="vp-rail-button" data-selected={selected || undefined} aria-label={label} title={label} aria-pressed={selected} aria-controls={controls} onClick={onSelect} onKeyDown={(event) => {
    if (event.key === "ArrowRight" && selected) { event.preventDefault(); onOpen(); }
  }}><NavigationIcon icon={group.icon} state={selected ? "active" : "normal"} size="lg" /></button>;
}

function IconForGroup({ group }: { group: PortalNavigationGroup }) {
  return <NavigationIcon icon={group.icon} />;
}

function SecondLevelMenu({ group, composition, onNavigate, onLogout }: { group: PortalNavigationGroup; composition: UIShellProps["composition"]; onNavigate(id: string): void; onLogout?(): Promise<void> }) {
  const i18n = usePortalI18n();
  const activePageID = composition.activeNavigationPath?.pageID;
  const activeChildID = composition.activeNavigationPath?.rootGroupID === group.id ? composition.activeNavigationPath.childGroupID : undefined;
  const storageKey = `${namespace}.open-child-groups`;
  const [openGroups, setOpenGroups] = useState<ReadonlySet<string>>(() => readOpenGroups(storageKey, activeChildID));
  useEffect(() => {
    if (activeChildID === undefined) return;
    setOpenGroups((current) => current.has(activeChildID) ? current : new Set([...current, activeChildID]));
  }, [activeChildID]);
  const setOpen = (id: string, open: boolean) => setOpenGroups((current) => {
    const next = new Set(current);
    if (open) next.add(id); else next.delete(id);
    writeOpenGroups(storageKey, next);
    return next;
  });
  const includesLogout = group.id === accountNavigationNodeID && onLogout !== undefined;
  if (group.pages.length === 0 && group.children.length === 0 && !includesLogout) {
    return <p className="vp-navigation-empty">{i18n.text(message(namespace, "navigation.accountUnavailable", "个人中心尚未装配"))}</p>;
  }
  return <div className="vp-navigation-tree">
    {group.pages.length === 0 ? null : <ul className="vp-navigation-page-list vp-navigation-root-pages">
      {group.pages.map((item) => <NavigationLink key={item.id} id={item.id} label={i18n.text(item.label)} href={pagePath(composition, item.id)} active={item.id === activePageID} onNavigate={onNavigate} />)}
    </ul>}
    {group.children.map((child) => {
      const open = openGroups.has(child.id);
      const panelID = `vp-navigation-child-${child.id}`;
      return <section key={child.id} className="vp-navigation-child" data-active={child.id === activeChildID || undefined}>
        <button type="button" className="vp-navigation-child-trigger" aria-expanded={open} aria-controls={panelID} onClick={() => setOpen(child.id, !open)}>
          <span>{i18n.text(child.label)}</span><span className="vp-navigation-chevron" aria-hidden="true">›</span>
        </button>
        {open ? <ul id={panelID} className="vp-navigation-page-list">
          {child.pages.map((item) => <NavigationLink key={item.id} id={item.id} label={i18n.text(item.label)} href={pagePath(composition, item.id)} active={item.id === activePageID} onNavigate={onNavigate} />)}
        </ul> : null}
      </section>;
    })}
    {!includesLogout ? null : <ul className="vp-navigation-page-list vp-navigation-root-pages">
      <NavigationAction label={i18n.text(message(namespace, "account.logout", "退出登录"))} onSelect={() => void onLogout()} />
    </ul>}
  </div>;
}

function NavigationLink({ id, label, href, active, onNavigate }: { id: string; label: string; href?: string; active: boolean; onNavigate(id: string): void }) {
  return <li><a className="vp-navigation-link" href={href} aria-current={active ? "page" : undefined} onClick={(event) => {
    event.preventDefault();
    onNavigate(id);
  }}>{label}</a></li>;
}

function NavigationAction({ label, onSelect }: { label: string; onSelect(): void }) {
  return <li><button type="button" className="vp-navigation-action" onClick={onSelect}>{label}</button></li>;
}

function readOpenGroups(key: string, activeID?: string): ReadonlySet<string> {
  const fallback = new Set(activeID === undefined ? [] : [activeID]);
  if (typeof window === "undefined") return fallback;
  try {
    const stored = JSON.parse(window.sessionStorage.getItem(key) ?? "[]") as unknown;
    if (!Array.isArray(stored)) return fallback;
    return new Set([...stored.filter((value): value is string => typeof value === "string"), ...fallback]);
  } catch {
    return fallback;
  }
}

function writeOpenGroups(key: string, values: ReadonlySet<string>) {
  if (typeof window === "undefined") return;
  try { window.sessionStorage.setItem(key, JSON.stringify([...values])); } catch { /* session storage is optional */ }
}

function returnToRail(event: KeyboardEvent<HTMLElement>, button: React.RefObject<HTMLButtonElement>) {
  if (event.key !== "ArrowLeft") return;
  event.preventDefault();
  button.current?.focus();
}

function moveRailFocus(event: KeyboardEvent<HTMLElement>) {
  if (!["ArrowUp", "ArrowDown", "Home", "End"].includes(event.key)) return;
  const buttons = [...event.currentTarget.querySelectorAll<HTMLButtonElement>(".vp-rail-button")];
  const current = buttons.indexOf(event.target as HTMLButtonElement);
  if (current < 0 || buttons.length === 0) return;
  event.preventDefault();
  const next = event.key === "Home" ? 0 : event.key === "End" ? buttons.length - 1 : (current + (event.key === "ArrowDown" ? 1 : -1) + buttons.length) % buttons.length;
  buttons[next]?.focus();
}

export function groups(composition: UIShellProps["composition"], zones: readonly NavigationZone[]): readonly PortalNavigationGroup[] {
  return zones.flatMap((zone) => composition.navigation[zone]);
}

/** Visual ordering for the standard side rail; the account remains the fixed bottom control. */
export function navigationRailGroups(composition: UIShellProps["composition"]): readonly PortalNavigationGroup[] {
  return [
    ...composition.navigation.primary,
    ...composition.navigation.secondary.filter((group) => group.id !== accountNavigationNodeID),
    ...composition.navigation.settings,
  ];
}

/** Returns the first routeable leaf in the same order as the second-level menu. */
export function firstNavigablePageID(group: PortalNavigationGroup): string | undefined {
  return group.pages[0]?.id ?? group.children.flatMap((child) => child.pages)[0]?.id;
}

function pagePath(composition: UIShellProps["composition"], navigationID: string): string | undefined {
  return composition.pages.find((candidate) => candidate.navigation?.id === navigationID)?.path;
}

function Brand({ name, shortName, logoURL, compact = false }: { name: string; shortName?: string; logoURL?: string; compact?: boolean }) {
  const label = shortName ?? name;
  return <div className={`vp-brand${compact ? " vp-brand-compact" : ""}`} title={name}>{logoURL === undefined ? <span className="vp-brand-mark">{label.slice(0, 1).toUpperCase()}</span> : <img src={logoURL} alt="" className="vp-brand-logo" />}{compact ? null : <strong>{label}</strong>}</div>;
}

function shellSlot(values: UIShellProps["composition"]["shellSlots"], id: ShellSlotID): ReactNode {
  return values[id]?.map((item) => createElement(item.component, { key: `${item.pluginID}/${item.id}` }));
}

function pageSlot(values: UIShellProps["composition"]["pageSlots"], id: PageSlotID): ReactNode {
  return values[id]?.map((item) => createElement(item.component, { key: item.id }));
}

export const standardShellCSS = `
.vp-shell-root{height:100vh;height:100dvh;display:flex;flex-direction:column;overflow:hidden;background:var(--vp-shell-canvas);color:var(--vp-shell-text)}
.vp-shell-header{height:var(--vp-shell-bar-height);flex:0 0 var(--vp-shell-bar-height);display:grid;grid-template-columns:minmax(180px,auto) 1fr minmax(180px,auto);align-items:center;gap:16px;padding:0 24px;background:var(--vp-shell-surface);border-bottom:1px solid var(--vp-shell-border);z-index:20}.vp-shell-header-side{display:flex;align-items:center;gap:12px}.vp-shell-header-center{display:flex;justify-content:center;min-width:0}.vp-shell-header-end{justify-content:flex-end}
.vp-shell-frame{display:flex;flex:1;min-height:0;min-width:0}.vp-desktop-navigation{display:flex;flex:0 0 auto;min-height:0}.vp-navigation-rail{width:var(--vp-shell-rail-width);flex:0 0 var(--vp-shell-rail-width);min-height:0;display:grid;grid-template-rows:auto minmax(0,1fr) auto;background:var(--vp-shell-surface);border-right:1px solid var(--vp-shell-border)}.vp-navigation-start,.vp-navigation-center,.vp-navigation-end{display:flex;flex-direction:column;align-items:center;gap:8px;padding:8px}.vp-navigation-center{overflow-y:auto;overscroll-behavior:contain;scrollbar-width:thin}.vp-navigation-start{box-sizing:border-box;height:var(--vp-shell-bar-height);min-height:var(--vp-shell-bar-height);justify-content:center;overflow:hidden;border-bottom:1px solid var(--vp-shell-border)}.vp-navigation-end{border-top:1px solid var(--vp-shell-border);max-height:40vh;overflow-y:auto}
.vp-rail-button{width:var(--vp-shell-touch-minimum);height:var(--vp-shell-touch-minimum);flex:0 0 var(--vp-shell-touch-minimum);display:grid;place-items:center;border:0;border-radius:10px;background:transparent;color:var(--vp-shell-muted);cursor:pointer}.vp-rail-button:hover{background:var(--vp-shell-hover);color:var(--vp-shell-primary)}.vp-rail-button[data-selected]{background:var(--vp-shell-selected);color:var(--vp-shell-primary)}.vp-rail-button:focus-visible,.vp-mobile-menu-button:focus-visible,.vp-navigation-child-trigger:focus-visible,.vp-navigation-link:focus-visible{outline:var(--vp-shell-focus-width) solid var(--vp-shell-focus);outline-offset:2px}
.vp-navigation-panel{width:var(--vp-shell-navigation-width);flex:0 0 var(--vp-shell-navigation-width);min-height:0;display:grid;grid-template-rows:auto minmax(0,1fr);background:var(--vp-shell-surface);border-right:1px solid var(--vp-shell-border)}.vp-navigation-panel-header{box-sizing:border-box;height:var(--vp-shell-bar-height);min-height:var(--vp-shell-bar-height);display:flex;align-items:center;gap:10px;padding:8px 16px;border-bottom:1px solid var(--vp-shell-border)}.vp-navigation-panel-icon{color:var(--vp-shell-primary)}.vp-navigation-panel-body{min-height:0;overflow-y:auto;overscroll-behavior:contain;padding:8px;scrollbar-width:thin}
.vp-navigation-tree{display:grid;gap:4px}.vp-navigation-empty{margin:8px;padding:12px;border-radius:8px;background:var(--vp-shell-hover);color:var(--vp-shell-muted);font-size:13px;line-height:1.5}.vp-navigation-page-list{list-style:none;margin:0;padding:0}.vp-navigation-root-pages{margin:0}.vp-navigation-link,.vp-navigation-child-trigger{min-height:var(--vp-shell-touch-minimum);box-sizing:border-box;border-radius:8px;color:var(--vp-shell-text);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vp-navigation-link{display:flex;align-items:center;padding:8px 12px;text-decoration:none}.vp-navigation-link:hover,.vp-navigation-child-trigger:hover{background:var(--vp-shell-hover);color:var(--vp-shell-primary)}.vp-navigation-link[aria-current=page]{background:var(--vp-shell-selected);color:var(--vp-shell-primary);font-weight:600}.vp-navigation-child{border-radius:8px}.vp-navigation-child-trigger{width:100%;display:flex;align-items:center;justify-content:space-between;gap:8px;padding:8px 12px;border:0;background:transparent;font:inherit;font-weight:400;text-align:left;cursor:pointer}.vp-navigation-chevron{color:var(--vp-shell-muted);transition:transform var(--vp-shell-motion-fast) ease}.vp-navigation-child-trigger[aria-expanded=true] .vp-navigation-chevron{transform:rotate(90deg)}.vp-navigation-child .vp-navigation-page-list{padding:2px 0 2px 12px}
.vp-shell-content{flex:1;min-width:0;min-height:0;display:flex;flex-direction:column}.vp-mobile-header{display:none}.vp-mobile-preferences{margin-left:auto;display:flex}.vp-page-header{box-sizing:border-box;height:var(--vp-shell-bar-height);min-height:var(--vp-shell-bar-height);flex:0 0 var(--vp-shell-bar-height);display:grid;grid-template-columns:minmax(0,1fr) auto minmax(0,1fr);align-items:center;gap:16px;padding:8px 24px;background:var(--vp-shell-surface);border-bottom:1px solid var(--vp-shell-border);z-index:10}.vp-page-header-side{display:flex;align-items:center;gap:12px;min-width:0}.vp-page-header-center{display:flex;justify-content:center;gap:12px;min-width:0}.vp-page-header-end{justify-content:flex-end}.vp-page-title-copy{min-width:0}.vp-page-title{font-size:22px;line-height:1.2;margin:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vp-page-description{font-size:14px;line-height:1.3;color:var(--vp-shell-muted);margin:2px 0 0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vp-page-scroller{flex:1;min-height:0;overflow:auto;overscroll-behavior:contain;background:var(--vp-shell-surface)}.vp-page{box-sizing:border-box;width:100%;margin:0 auto;padding:var(--vp-page-content-start) 24px 24px}.vp-page-body-row{display:flex;align-items:flex-start;gap:20px}.vp-page-body-main{flex:1;min-width:0}.vp-page-aside{width:320px;flex:0 0 320px;max-height:calc(100dvh - 120px);overflow:auto}.vp-shell-footer{flex:0 0 auto}.vp-brand{display:flex;align-items:center;gap:10px;min-height:40px;min-width:0}.vp-brand-compact{justify-content:center}.vp-brand-mark{width:32px;height:32px;flex:0 0 32px;border-radius:9px;display:grid;place-items:center;color:var(--vp-shell-surface);background:var(--vp-shell-primary)}.vp-brand-logo{width:32px;height:32px;object-fit:contain}.vp-mobile-menu-button{width:44px;height:44px;border:0;border-radius:8px;background:transparent;color:var(--vp-shell-text);display:grid;place-items:center}
[data-vastplan-navigation-icon][data-motion=pulse]{animation:vp-nav-pulse 1.4s ease-in-out infinite}[data-vastplan-navigation-icon][data-motion=spin]{display:inline-flex;animation:vp-nav-spin 1s linear infinite}[data-vastplan-navigation-icon][data-motion=draw]{animation:vp-nav-draw 1.2s ease-in-out infinite}@keyframes vp-nav-pulse{50%{opacity:.45;transform:scale(.9)}}@keyframes vp-nav-spin{to{transform:rotate(360deg)}}@keyframes vp-nav-draw{50%{opacity:.55}}
.vp-navigation-action{display:flex;align-items:center;width:100%;min-height:var(--vp-shell-touch-minimum);box-sizing:border-box;padding:8px 12px;border:0;border-radius:8px;background:transparent;color:var(--vp-shell-text);font:inherit;text-align:left;cursor:pointer;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vp-navigation-action:hover{background:var(--vp-shell-hover);color:var(--vp-shell-primary)}.vp-navigation-action:focus-visible{outline:var(--vp-shell-focus-width) solid var(--vp-shell-focus);outline-offset:2px}
@media (max-width:1199px){.vp-navigation-panel{width:var(--vp-shell-navigation-compact-width);flex-basis:var(--vp-shell-navigation-compact-width)}.vp-page{padding:var(--vp-page-content-start) 20px 20px}.vp-page-header{padding-left:20px;padding-right:20px}}
@media (max-width:767px){.vp-desktop-navigation{display:none}.vp-mobile-header{box-sizing:border-box;height:var(--vp-shell-bar-height);flex:0 0 var(--vp-shell-bar-height);display:flex;align-items:center;gap:8px;padding:0 12px;background:var(--vp-shell-surface);border-bottom:1px solid var(--vp-shell-border)}.vp-page-header{height:auto;min-height:var(--vp-shell-bar-height);flex:0 0 auto;grid-template-columns:minmax(0,1fr) auto;padding:8px 16px}.vp-page-header-center{grid-column:1/-1;justify-content:flex-start;overflow-x:auto}.vp-page-header-end{grid-column:2}.vp-page-title{font-size:20px}.vp-page-description{max-width:65vw}.vp-page{padding:var(--vp-page-content-start) 16px 16px}.vp-page-body-row{display:block}.vp-page-aside{width:auto;max-height:none;margin-top:16px;overflow:visible}}
@media (prefers-reduced-motion:reduce){.vp-shell-root *{scroll-behavior:auto!important;transition:none!important;animation:none!important}}
`;

const namespace = "cn.vastplan.foundation.frontend.structure.layout.standard";

function navigationLabel(group: PortalNavigationGroup, i18n: ReturnType<typeof usePortalI18n>): string {
  return group.labels?.[i18n.locale] ?? i18n.text(group.label);
}
export const shellLibrary = {
  id: "standard", shell: "ui.structure.shell", uiContract: uiContractVersion, Shell: StandardShell,
  localization: {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": { "page.notFound": "页面不存在", "page.pathMissing": "Portal 没有注册路径 {path}", "navigation.open": "打开主菜单", "navigation.mobile": "移动主菜单", "navigation.groups": "主菜单分组", "navigation.secondaryLabel": "{group}导航", "navigation.accountUnavailable": "个人中心尚未装配" },
      "en-US": { "page.notFound": "Page not found", "page.pathMissing": "Portal has no registered route for {path}", "navigation.open": "Open main menu", "navigation.mobile": "Mobile main menu", "navigation.groups": "Main menu groups", "navigation.secondaryLabel": "{group} navigation", "navigation.accountUnavailable": "Account center is not installed" },
    },
  },
};
export const localization = shellLibrary.localization;
export default shellLibrary;
