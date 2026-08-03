import { createElement, useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { uiContractVersion } from "@vastplan/ui-contract";
import {
  accountNavigationNodeID,
  accountLogoutMenuItemID,
  accountMenuItems,
  message,
  NavigationIcon,
  PortalNavigationMenu,
  portalPageRhythm,
  PortalAccountControl,
  resolvePageBodyMaxWidth,
  usePortalI18n,
  usePortalUI,
  type MenuItem,
  type NavigationZone,
  type PageSlotID,
  type PortalI18n,
  type PortalNavigationGroup,
  type UIShellProps,
  type ShellSlotID,
} from "@vastplan/ui-primitives";

const shellHeaderSlots = ["shell.header.start", "shell.header.center", "shell.header.end"] as const;

export function TopNavigationShell(props: UIShellProps) {
  const { composition, branding, template, pathname, recoveryNotice, onNavigate } = props;
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const [mobileOpen, setMobileOpen] = useState(false);
  const [openRootID, setOpenRootID] = useState<string>();
  const centerRef = useRef<HTMLDivElement>(null);
  const centerWidth = useContainerWidth(centerRef, 1200);
  const accountRoot = composition.navigation.secondary.find((group) => group.id === accountNavigationNodeID);
  const mainRoots = useMemo(() => [...composition.navigation.primary, ...composition.navigation.secondary.filter((group) => group.id !== accountNavigationNodeID)], [composition]);
  const settingsRoots = composition.navigation.settings;
  const activeRootID = composition.activeNavigationPath?.rootGroupID;
  const capacity = topNavigationCapacity(centerWidth, ui.theme.tokens.touch.minimum);
  const { visible, overflow } = prioritizeRoots(mainRoots, capacity, activeRootID);
  const page = composition.activePage;
  const pageWidth = resolvePageBodyMaxWidth(page?.bodyLayout, template.options.pageBodyWidth === "contained");
  const shellTheme = {
    "--vp-top-canvas": ui.theme.tokens.color.canvas,
    "--vp-top-surface": ui.theme.tokens.color.surface,
    "--vp-top-text": ui.theme.tokens.color.text,
    "--vp-top-muted": ui.theme.tokens.color.mutedText,
    "--vp-top-border": ui.theme.tokens.color.border,
    "--vp-top-primary": ui.theme.tokens.color.primary,
    "--vp-top-hover": ui.theme.tokens.color.hover,
    "--vp-top-selected": ui.theme.tokens.color.selected,
    "--vp-top-focus": ui.theme.tokens.color.focusRing,
    "--vp-top-bar-height": `${ui.theme.tokens.shell.barHeight}px`,
    "--vp-top-focus-width": `${ui.theme.tokens.focus.width}px`,
    "--vp-top-touch-minimum": `${ui.theme.tokens.touch.minimum}px`,
    "--vp-page-content-start": `${portalPageRhythm.contentStart}px`,
  } as CSSProperties;

  const navigate = (navigationID: string) => {
    const target = composition.pages.find((candidate) => candidate.navigation?.id === navigationID);
    if (target === undefined) return;
    onNavigate(target.id);
    setOpenRootID(undefined);
    setMobileOpen(false);
  };

  const mobileItems: MenuItem[] = groups(composition, ["primary", "secondary", "settings"]).map((group) => ({
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

  const shellHeaderVisible = shellHeaderSlots.some((slot) => (composition.shellSlots[slot]?.length ?? 0) > 0);
  return <div className="vp-top-shell" style={shellTheme}>
    <style>{topNavigationShellCSS}</style>
    {shellHeaderVisible ? <header className="vp-top-shell-header">
      <div>{shellSlot(composition.shellSlots, "shell.header.start")}</div>
      <div className="vp-top-shell-header-center">{shellSlot(composition.shellSlots, "shell.header.center")}</div>
      <div className="vp-top-shell-header-end">{shellSlot(composition.shellSlots, "shell.header.end")}</div>
    </header> : null}
    <header className="vp-top-bar" onKeyDown={moveTopRootFocus}>
      <div className="vp-top-start"><Brand name={branding.name} shortName={branding.shortName} logoURL={branding.logoURL} />{shellSlot(composition.shellSlots, "shell.navigation.start")}</div>
      {page === undefined ? null : <span className="vp-top-logo-page-divider" aria-hidden="true" />}
      {page === undefined ? null : <PageHeader className="vp-top-inline-page-header" page={page} composition={composition} />}
      {page === undefined ? null : <span className="vp-top-page-navigation-divider" aria-hidden="true" />}
      <nav ref={centerRef} className="vp-top-center" aria-label={i18n.text(message(namespace, "navigation.main", "主导航"))}>
        {visible.map((group) => <RootPopover key={group.id} group={group} composition={composition} open={openRootID === group.id} active={activeRootID === group.id} onOpenChange={(open) => setOpenRootID(open ? group.id : undefined)} onNavigate={navigate} />)}
        {overflow.length === 0 ? null : <OverflowPopover groups={overflow} composition={composition} open={openRootID === "__more"} active={overflow.some((group) => group.id === activeRootID)} onOpenChange={(open) => setOpenRootID(open ? "__more" : undefined)} onNavigate={navigate} />}
        {shellSlot(composition.shellSlots, "shell.navigation.center")}
      </nav>
      <div className="vp-top-end">
        {settingsRoots.map((group) => <RootPopover key={group.id} group={group} composition={composition} open={openRootID === group.id} active={activeRootID === group.id} onOpenChange={(open) => setOpenRootID(open ? group.id : undefined)} onNavigate={navigate} />)}
        {shellSlot(composition.shellSlots, "shell.navigation.end")}
        {accountRoot === undefined ? null : <div className="vp-top-account"><AccountPopover group={accountRoot} account={props.account} composition={composition} open={openRootID === accountRoot.id} active={activeRootID === accountRoot.id} onOpenChange={(open) => setOpenRootID(open ? accountRoot.id : undefined)} onNavigate={navigate} onLogout={props.onLogout} /></div>}
      </div>
      <div className="vp-top-mobile-controls"><PortalAccountControl account={props.account} onSelect={() => setMobileOpen(true)} /><button type="button" className="vp-top-mobile-trigger" aria-label={i18n.text(message(namespace, "navigation.open", "打开主菜单"))} onClick={() => setMobileOpen(true)}><ui.Icon name="menu" /></button></div>
    </header>
    <div className="vp-top-content">
      {page === undefined ? null : <PageHeader className="vp-top-page-header" page={page} composition={composition} />}
      <div className="vp-top-page-scroller"><main className="vp-top-page" data-page-body-layout={page?.bodyLayout ?? "large"} style={{ maxWidth: pageWidth }}>
        {recoveryNotice}
        {page === undefined ? <ui.EmptyState title={i18n.text(message(namespace, "page.notFound", "页面不存在"))} description={i18n.text(message(namespace, "page.pathMissing", "Portal 没有注册路径 {path}", { path: pathname }))} /> : <>
          {pageSlot(composition.pageSlots, "page.body.before")}
          <div className="vp-top-page-body-row"><section className="vp-top-page-body-main">{pageSlot(composition.pageSlots, "page.body.main")}</section>{(composition.pageSlots["page.aside"]?.length ?? 0) === 0 ? null : <aside className="vp-top-page-aside">{pageSlot(composition.pageSlots, "page.aside")}</aside>}</div>
          {pageSlot(composition.pageSlots, "page.body.after")}
        </>}
      </main></div>
    </div>
    {(composition.shellSlots["shell.footer"]?.length ?? 0) === 0 ? null : <footer>{shellSlot(composition.shellSlots, "shell.footer")}</footer>}
    <ui.Drawer open={mobileOpen} title={branding.name} placement="left" width="sm" onClose={() => setMobileOpen(false)}><nav aria-label={i18n.text(message(namespace, "navigation.mobile", "移动主菜单"))}><ui.Menu items={mobileItems} activeID={page?.navigation?.id} onSelect={selectMobileMenu} /></nav></ui.Drawer>
  </div>;
}

/** Desktop puts page context in the primary bar; mobile keeps its own readable row. */
function PageHeader({ className, page, composition }: { className: string; page: NonNullable<UIShellProps["composition"]["activePage"]>; composition: UIShellProps["composition"] }) {
  const i18n = usePortalI18n();
  return <header className={className}>
    <div className="vp-top-page-header-side">{pageSlot(composition.pageSlots, "page.header.start")}<div className="vp-top-page-title-copy"><h1 className="vp-top-page-title" tabIndex={-1}>{i18n.text(page.title)}</h1>{page.description === undefined ? null : <p className="vp-top-page-description">{i18n.text(page.description)}</p>}</div></div>
    <div className="vp-top-page-header-center">{pageSlot(composition.pageSlots, "page.header.center")}</div>
    <div className="vp-top-page-header-side vp-top-page-header-end">{pageSlot(composition.pageSlots, "page.header.end")}</div>
  </header>;
}

function RootPopover({ group, composition, open, active, onOpenChange, onNavigate }: { group: PortalNavigationGroup; composition: UIShellProps["composition"]; open: boolean; active: boolean; onOpenChange(open: boolean): void; onNavigate(id: string): void }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const label = navigationLabel(group, i18n);
  return <ui.Popover open={open} placement="bottom-start" surface="compact" ariaLabel={label} initialFocus="current" onOpenChange={(next) => onOpenChange(next)} trigger={(props) => <button ref={(node) => props.ref(node)} type="button" className="vp-top-root-trigger" data-zone={group.zone} data-active={active || undefined} aria-label={label} title={label} aria-current={active ? "location" : undefined} aria-expanded={props["aria-expanded"]} aria-controls={props["aria-controls"]} onClick={props.onClick} onKeyDown={props.onKeyDown}><NavigationIcon icon={group.icon} state={active ? "active" : "normal"} size="md" /></button>}>
    <NavigationPopoverMenu groups={[group]} composition={composition} onNavigate={onNavigate} />
  </ui.Popover>;
}

/** The avatar is only a trigger; its contents use the same top navigation popover as every root group. */
function AccountPopover({ group, account, composition, open, active, onOpenChange, onNavigate, onLogout }: {
  group: PortalNavigationGroup;
  account: UIShellProps["account"];
  composition: UIShellProps["composition"];
  open: boolean;
  active: boolean;
  onOpenChange(open: boolean): void;
  onNavigate(id: string): void;
  onLogout?(): Promise<void>;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  return <ui.Popover open={open} placement="bottom-end" surface="compact" ariaLabel={navigationLabel(group, i18n)} initialFocus="current" onOpenChange={onOpenChange} trigger={(trigger) => <PortalAccountControl account={account} selected={active} trigger={trigger} />}>
    <NavigationPopoverMenu groups={[group]} composition={composition} onNavigate={onNavigate} onLogout={onLogout} />
  </ui.Popover>;
}

function OverflowPopover({ groups: overflow, composition, open, active, onOpenChange, onNavigate }: { groups: readonly PortalNavigationGroup[]; composition: UIShellProps["composition"]; open: boolean; active: boolean; onOpenChange(open: boolean): void; onNavigate(id: string): void }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const label = i18n.text(message(namespace, "navigation.more", "更多导航"));
  return <ui.Popover open={open} placement="bottom-end" surface="compact" ariaLabel={label} initialFocus="current" onOpenChange={(next) => onOpenChange(next)} trigger={(props) => <button ref={(node) => props.ref(node)} type="button" className="vp-top-root-trigger" data-active={active || undefined} aria-label={label} title={label} aria-expanded={props["aria-expanded"]} aria-controls={props["aria-controls"]} onClick={props.onClick} onKeyDown={props.onKeyDown}><ui.Icon name="menu" size="md" /></button>}>
    <NavigationPopoverMenu groups={overflow} composition={composition} onNavigate={onNavigate} />
  </ui.Popover>;
}

/** One compact menu surface is shared by primary, account and overflow navigation. */
function NavigationPopoverMenu({ groups: menuGroups, composition, onNavigate, onLogout }: { groups: readonly PortalNavigationGroup[]; composition: UIShellProps["composition"]; onNavigate(id: string): void; onLogout?(): Promise<void> }) {
  const i18n = usePortalI18n();
  return <PortalNavigationMenu
    groups={menuGroups}
    composition={composition}
    activeID={composition.activeNavigationPath?.pageID}
    onNavigate={onNavigate}
    onLogout={onLogout}
    empty={<div className="vp-top-navigation-menu-empty">{i18n.text(message(namespace, "navigation.accountUnavailable", "个人中心尚未装配"))}</div>}
  />;
}

function useContainerWidth(ref: React.RefObject<HTMLElement>, fallback: number): number {
  const [width, setWidth] = useState(fallback);
  useEffect(() => {
    const node = ref.current;
    if (node === null || typeof ResizeObserver === "undefined") return;
    const update = () => setWidth(node.clientWidth || fallback);
    update();
    const observer = new ResizeObserver(update);
    observer.observe(node);
    return () => observer.disconnect();
  }, [fallback, ref]);
  return width;
}

function moveTopRootFocus(event: React.KeyboardEvent<HTMLElement>) {
  if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
  const buttons = [...event.currentTarget.querySelectorAll<HTMLButtonElement>(".vp-top-root-trigger")];
  const current = buttons.indexOf(event.target as HTMLButtonElement);
  if (current < 0 || buttons.length === 0) return;
  event.preventDefault();
  const next = event.key === "Home" ? 0 : event.key === "End" ? buttons.length - 1 : (current + (event.key === "ArrowRight" ? 1 : -1) + buttons.length) % buttons.length;
  buttons[next]?.focus();
}

export function prioritizeRoots(groups: readonly PortalNavigationGroup[], capacity: number, activeID?: string): { visible: readonly PortalNavigationGroup[]; overflow: readonly PortalNavigationGroup[] } {
  if (groups.length <= capacity) return { visible: groups, overflow: [] };
  const slots = Math.max(1, capacity - 1);
  const visible = groups.slice(0, slots);
  const active = groups.find((group) => group.id === activeID);
  if (active !== undefined && !visible.some((group) => group.id === active.id)) visible[visible.length - 1] = active;
  const visibleIDs = new Set(visible.map((group) => group.id));
  return { visible, overflow: groups.filter((group) => !visibleIDs.has(group.id)) };
}

/** Root navigation is icon-only, so capacity follows its real touch target instead of a legacy text-menu budget. */
export function topNavigationCapacity(availableWidth: number, triggerWidth: number, gap = 6): number {
  return Math.max(1, Math.floor((availableWidth + gap) / (triggerWidth + gap)));
}

function groups(composition: UIShellProps["composition"], zones: readonly NavigationZone[]): readonly PortalNavigationGroup[] { return zones.flatMap((zone) => composition.navigation[zone]); }
function pagePath(composition: UIShellProps["composition"], navigationID: string): string | undefined { return composition.pages.find((candidate) => candidate.navigation?.id === navigationID)?.path; }
function Brand({ name, shortName, logoURL }: { name: string; shortName?: string; logoURL?: string }) { const label = shortName ?? name; return <div className="vp-top-brand" title={name}>{logoURL === undefined ? <span className="vp-top-brand-mark">{label.slice(0, 1).toUpperCase()}</span> : <img src={logoURL} alt="" className="vp-top-brand-logo" />}<strong>{label}</strong></div>; }
function shellSlot(values: UIShellProps["composition"]["shellSlots"], id: ShellSlotID): ReactNode { return values[id]?.map((item) => createElement(item.component, { key: `${item.pluginID}/${item.id}` })); }
function pageSlot(values: UIShellProps["composition"]["pageSlots"], id: PageSlotID): ReactNode { return values[id]?.map((item) => createElement(item.component, { key: item.id })); }

export const topNavigationShellCSS = `
.vp-top-shell{height:100vh;height:100dvh;display:flex;flex-direction:column;overflow:hidden;background:var(--vp-top-canvas);color:var(--vp-top-text)}
.vp-top-shell-header{height:var(--vp-top-bar-height);flex:0 0 var(--vp-top-bar-height);display:grid;grid-template-columns:minmax(180px,auto) 1fr minmax(180px,auto);align-items:center;gap:16px;padding:0 24px;background:var(--vp-top-surface);border-bottom:1px solid var(--vp-top-border)}.vp-top-shell-header-center{display:flex;justify-content:center}.vp-top-shell-header-end{display:flex;justify-content:flex-end}
.vp-top-bar{height:var(--vp-top-bar-height);flex:0 0 var(--vp-top-bar-height);display:flex;align-items:center;gap:0;padding:0 20px;background:var(--vp-top-surface);border-bottom:1px solid var(--vp-top-border);z-index:20}.vp-top-start,.vp-top-end,.vp-top-center{display:flex;align-items:center;gap:6px;min-width:0}.vp-top-start,.vp-top-end{flex:0 0 auto}.vp-top-inline-page-header{box-sizing:border-box;display:grid;grid-template-columns:minmax(0,1fr) auto minmax(0,1fr);align-items:center;gap:12px;flex:0 1 320px;min-width:160px}.vp-top-logo-page-divider,.vp-top-page-navigation-divider{align-self:center;width:1px;height:32px;flex:0 0 1px;margin:0 12px;background:var(--vp-top-border)}.vp-top-center{flex:1 1 0;justify-content:flex-start;overflow:hidden}.vp-top-end{justify-content:flex-end;gap:0}.vp-top-account{display:flex;align-items:center;margin-left:12px;padding-left:12px;border-left:1px solid var(--vp-top-border)}.vp-top-mobile-controls{display:none;align-items:center;gap:4px}.vp-top-brand{display:flex;align-items:center;gap:10px;min-width:0;white-space:nowrap}.vp-top-brand-mark,.vp-top-brand-logo{width:32px;height:32px;flex:0 0 32px}.vp-top-brand-mark{display:grid;place-items:center;border-radius:9px;background:var(--vp-top-primary);color:var(--vp-top-surface)}.vp-top-brand-logo{object-fit:contain}.vp-top-root-trigger,.vp-top-mobile-trigger{position:relative;height:var(--vp-top-touch-minimum);min-width:var(--vp-top-touch-minimum);display:flex;align-items:center;justify-content:center;gap:7px;padding:0 12px;border:0;border-radius:9px;background:transparent;color:var(--vp-top-muted);font:inherit;cursor:pointer;white-space:nowrap}.vp-top-root-trigger[data-zone=secondary]{margin-left:6px;border-left:1px solid var(--vp-top-border)}.vp-top-root-trigger:hover{background:var(--vp-top-hover);color:var(--vp-top-primary)}.vp-top-root-trigger[data-active]{background:var(--vp-top-selected);color:var(--vp-top-primary);font-weight:600}.vp-top-root-trigger[data-active]:after{content:"";position:absolute;left:12px;right:12px;bottom:0;height:2px;border-radius:2px;background:var(--vp-top-primary)}.vp-top-root-trigger:focus-visible,.vp-top-mobile-trigger:focus-visible{outline:var(--vp-top-focus-width) solid var(--vp-top-focus);outline-offset:2px}.vp-top-mobile-trigger{display:none}
.vp-top-navigation-menu-empty{min-width:220px;padding:8px;color:var(--vp-top-muted);font-size:13px;line-height:1.5}
.vp-top-content{flex:1;min-width:0;min-height:0;display:flex;flex-direction:column}.vp-top-page-header{display:none}.vp-top-page-header-side{display:flex;align-items:center;gap:12px;min-width:0}.vp-top-page-header-center{display:flex;justify-content:center;gap:12px}.vp-top-page-header-end{justify-content:flex-end}.vp-top-page-title-copy{min-width:0}.vp-top-page-title{font-size:22px;line-height:1.2;margin:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vp-top-page-description{font-size:14px;color:var(--vp-top-muted);margin:2px 0 0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vp-top-inline-page-header .vp-top-page-title{font-size:18px}.vp-top-inline-page-header .vp-top-page-description{font-size:12px}.vp-top-page-scroller{flex:1;min-height:0;overflow:auto;overscroll-behavior:contain;background:var(--vp-top-surface)}.vp-top-page{box-sizing:border-box;width:100%;margin:0 auto;padding:var(--vp-page-content-start) 24px 24px}.vp-top-page-body-row{display:flex;align-items:flex-start;gap:20px}.vp-top-page-body-main{flex:1;min-width:0}.vp-top-page-aside{width:320px;flex:0 0 320px;max-height:calc(100dvh - 144px);overflow:auto}
[data-vastplan-navigation-icon][data-motion=pulse]{animation:vp-top-nav-pulse 1.4s ease-in-out infinite}[data-vastplan-navigation-icon][data-motion=spin]{display:inline-flex;animation:vp-top-nav-spin 1s linear infinite}[data-vastplan-navigation-icon][data-motion=draw]{animation:vp-top-nav-draw 1.2s ease-in-out infinite}@keyframes vp-top-nav-pulse{50%{opacity:.45;transform:scale(.9)}}@keyframes vp-top-nav-spin{to{transform:rotate(360deg)}}@keyframes vp-top-nav-draw{50%{opacity:.55}}
@media (max-width:1199px){.vp-top-bar{padding:0 14px}.vp-top-inline-page-header{min-width:120px;gap:8px}.vp-top-root-trigger{padding:0 9px}.vp-top-page{padding:var(--vp-page-content-start) 20px 20px}}
@media (max-width:767px){.vp-top-bar{display:grid;grid-template-columns:minmax(0,1fr) auto;padding:0 12px}.vp-top-logo-page-divider,.vp-top-inline-page-header,.vp-top-page-navigation-divider,.vp-top-center,.vp-top-end{display:none}.vp-top-mobile-controls,.vp-top-mobile-trigger{display:flex}.vp-top-brand strong{overflow:hidden;text-overflow:ellipsis}.vp-top-page-header{height:auto;min-height:var(--vp-top-bar-height);flex:0 0 auto;box-sizing:border-box;display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:16px;padding:8px 16px;background:var(--vp-top-surface);border-bottom:1px solid var(--vp-top-border);z-index:10}.vp-top-page-header-center{grid-column:1/-1;justify-content:flex-start;overflow-x:auto}.vp-top-page-header-end{grid-column:2}.vp-top-page-title{font-size:20px}.vp-top-page{padding:var(--vp-page-content-start) 16px 16px}.vp-top-page-body-row{display:block}.vp-top-page-aside{width:auto;max-height:none;margin-top:16px;overflow:visible}}
@media (max-width:520px){.vp-top-child-grid{grid-template-columns:minmax(0,1fr)}}
@media (prefers-reduced-motion:reduce){.vp-top-shell *{scroll-behavior:auto!important;transition:none!important;animation:none!important}}
`;

const namespace = "cn.vastplan.foundation.frontend.structure.layout.top-navigation";

function navigationLabel(group: PortalNavigationGroup, i18n: Pick<PortalI18n, "text" | "locale">): string {
  return group.labels?.[i18n.locale] ?? i18n.text(group.label);
}
export const shellLibrary = {
  id: "top-navigation", shell: "ui.structure.shell", uiContract: uiContractVersion, Shell: TopNavigationShell,
  localization: { defaultLocale: "zh-CN", messages: {
    "zh-CN": { "page.notFound": "页面不存在", "page.pathMissing": "Portal 没有注册路径 {path}", "navigation.main": "主导航", "navigation.open": "打开主菜单", "navigation.mobile": "移动主菜单", "navigation.more": "更多", "navigation.accountUnavailable": "个人中心尚未装配" },
    "en-US": { "page.notFound": "Page not found", "page.pathMissing": "Portal has no registered route for {path}", "navigation.main": "Main navigation", "navigation.open": "Open main menu", "navigation.mobile": "Mobile main menu", "navigation.more": "More", "navigation.accountUnavailable": "Account center is not installed" },
  } },
};
export const localization = shellLibrary.localization;
export default shellLibrary;
