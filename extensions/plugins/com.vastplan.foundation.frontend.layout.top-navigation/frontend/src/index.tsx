import { createElement, useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from "react";
import {
  message,
  usePortalI18n,
  usePortalUI,
  type MenuItem,
  type NavigationZone,
  type PageSlotID,
  type PortalNavigationGroup,
  type PortalPageNavigation,
  type ShellLayoutAdapter,
  type ShellLayoutProps,
  type ShellSlotID,
} from "@vastplan/portal-ui";

const shellHeaderSlots = ["shell.header.start", "shell.header.center", "shell.header.end"] as const;

function TopNavigationShell({ composition, branding, config, pathname, recoveryNotice, onNavigate }: ShellLayoutProps) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const [mobileOpen, setMobileOpen] = useState(false);
  const [openRootID, setOpenRootID] = useState<string>();
  const centerRef = useRef<HTMLDivElement>(null);
  const centerWidth = useContainerWidth(centerRef, 1200);
  const mainRoots = useMemo(() => [...composition.navigation.primary, ...composition.navigation.secondary], [composition]);
  const settingsRoots = composition.navigation.settings;
  const activeRootID = composition.activeNavigationPath?.rootGroupID;
  const capacity = Math.max(1, Math.floor(centerWidth / 120));
  const { visible, overflow } = prioritizeRoots(mainRoots, capacity, activeRootID);
  const page = composition.activePage;
  const pageWidth = config.pageBodyWidth === "contained" ? 1280 : undefined;
  const shellTheme = {
    "--vp-top-canvas": ui.theme.tokens.color.canvas,
    "--vp-top-surface": ui.theme.tokens.color.surface,
    "--vp-top-overlay": ui.theme.tokens.color.overlaySurface,
    "--vp-top-text": ui.theme.tokens.color.text,
    "--vp-top-muted": ui.theme.tokens.color.mutedText,
    "--vp-top-border": ui.theme.tokens.color.border,
    "--vp-top-primary": ui.theme.tokens.color.primary,
    "--vp-top-hover": ui.theme.tokens.color.hover,
    "--vp-top-selected": ui.theme.tokens.color.selected,
    "--vp-top-focus": ui.theme.tokens.color.focusRing,
    "--vp-top-bar-height": `${ui.theme.tokens.shell.barHeight}px`,
    "--vp-top-mega-min": `${ui.theme.tokens.overlay.navigationMinWidth}px`,
    "--vp-top-mega-max": `${ui.theme.tokens.overlay.navigationMaxWidth}px`,
    "--vp-top-overlay-shadow": ui.theme.tokens.elevation.overlay,
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
    label: i18n.text(group.label),
    icon: <ui.Icon name={group.icon} label={i18n.text(group.label)} />,
    children: [
      ...group.pages.map((item) => ({ id: item.id, label: i18n.text(item.label), href: pagePath(composition, item.id) })),
      ...group.children.map((child) => ({ id: `group:${child.id}`, label: i18n.text(child.label), children: child.pages.map((item) => ({ id: item.id, label: i18n.text(item.label), href: pagePath(composition, item.id) })) })),
    ],
  }));

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
      <nav ref={centerRef} className="vp-top-center" aria-label={i18n.text(message(namespace, "navigation.main", "主导航"))}>
        {visible.map((group) => <RootPopover key={group.id} group={group} composition={composition} open={openRootID === group.id} active={activeRootID === group.id} onOpenChange={(open) => setOpenRootID(open ? group.id : undefined)} onNavigate={navigate} />)}
        {overflow.length === 0 ? null : <OverflowPopover groups={overflow} composition={composition} open={openRootID === "__more"} active={overflow.some((group) => group.id === activeRootID)} onOpenChange={(open) => setOpenRootID(open ? "__more" : undefined)} onNavigate={navigate} />}
        {shellSlot(composition.shellSlots, "shell.navigation.center")}
      </nav>
      <div className="vp-top-end">
        {settingsRoots.map((group) => <RootPopover key={group.id} group={group} composition={composition} open={openRootID === group.id} active={activeRootID === group.id} onOpenChange={(open) => setOpenRootID(open ? group.id : undefined)} onNavigate={navigate} />)}
        {shellSlot(composition.shellSlots, "shell.navigation.end")}
      </div>
      <button type="button" className="vp-top-mobile-trigger" aria-label={i18n.text(message(namespace, "navigation.open", "打开主菜单"))} onClick={() => setMobileOpen(true)}><ui.Icon name="menu" /></button>
    </header>
    <div className="vp-top-content">
      {page === undefined ? null : <header className="vp-top-page-header">
        <div className="vp-top-page-header-side">{pageSlot(composition.pageSlots, "page.header.start")}<div className="vp-top-page-title-copy"><h1 className="vp-top-page-title" tabIndex={-1}>{i18n.text(page.title)}</h1>{page.description === undefined ? null : <p className="vp-top-page-description">{i18n.text(page.description)}</p>}</div></div>
        <div className="vp-top-page-header-center">{pageSlot(composition.pageSlots, "page.header.center")}</div>
        <div className="vp-top-page-header-side vp-top-page-header-end">{pageSlot(composition.pageSlots, "page.header.end")}</div>
      </header>}
      <div className="vp-top-page-scroller"><main className="vp-top-page" style={{ maxWidth: pageWidth }}>
        {recoveryNotice}
        {page === undefined ? <ui.EmptyState title={i18n.text(message(namespace, "page.notFound", "页面不存在"))} description={i18n.text(message(namespace, "page.pathMissing", "Portal 没有注册路径 {path}", { path: pathname }))} /> : <>
          {pageSlot(composition.pageSlots, "page.body.before")}
          <div className="vp-top-page-body-row"><section className="vp-top-page-body-main">{pageSlot(composition.pageSlots, "page.body.main")}</section>{(composition.pageSlots["page.aside"]?.length ?? 0) === 0 ? null : <aside className="vp-top-page-aside">{pageSlot(composition.pageSlots, "page.aside")}</aside>}</div>
          {pageSlot(composition.pageSlots, "page.body.after")}
        </>}
      </main></div>
    </div>
    {(composition.shellSlots["shell.footer"]?.length ?? 0) === 0 ? null : <footer>{shellSlot(composition.shellSlots, "shell.footer")}</footer>}
    <ui.Drawer open={mobileOpen} title={branding.name} placement="left" width="sm" onClose={() => setMobileOpen(false)}><nav aria-label={i18n.text(message(namespace, "navigation.mobile", "移动主菜单"))}><ui.Menu items={mobileItems} activeID={page?.navigation?.id} onSelect={navigate} /></nav></ui.Drawer>
  </div>;
}

function RootPopover({ group, composition, open, active, onOpenChange, onNavigate }: { group: PortalNavigationGroup; composition: ShellLayoutProps["composition"]; open: boolean; active: boolean; onOpenChange(open: boolean): void; onNavigate(id: string): void }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  return <ui.Popover open={open} placement="bottom-start" ariaLabel={i18n.text(group.label)} initialFocus="current" onOpenChange={(next) => onOpenChange(next)} trigger={(props) => <button ref={(node) => props.ref(node)} type="button" className="vp-top-root-trigger" data-active={active || undefined} aria-expanded={props["aria-expanded"]} aria-controls={props["aria-controls"]} onClick={props.onClick} onKeyDown={props.onKeyDown}><ui.Icon name={group.icon} /><span>{i18n.text(group.label)}</span></button>}>
    <MegaGroup group={group} composition={composition} onNavigate={onNavigate} />
  </ui.Popover>;
}

function OverflowPopover({ groups: overflow, composition, open, active, onOpenChange, onNavigate }: { groups: readonly PortalNavigationGroup[]; composition: ShellLayoutProps["composition"]; open: boolean; active: boolean; onOpenChange(open: boolean): void; onNavigate(id: string): void }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  return <ui.Popover open={open} placement="bottom-end" ariaLabel={i18n.text(message(namespace, "navigation.more", "更多导航"))} initialFocus="current" onOpenChange={(next) => onOpenChange(next)} trigger={(props) => <button ref={(node) => props.ref(node)} type="button" className="vp-top-root-trigger" data-active={active || undefined} aria-expanded={props["aria-expanded"]} aria-controls={props["aria-controls"]} onClick={props.onClick} onKeyDown={props.onKeyDown}><ui.Icon name="menu" /><span>{i18n.text(message(namespace, "navigation.more", "更多"))}</span></button>}>
    <div className="vp-top-overflow-mega">{overflow.map((group) => <section key={group.id} className="vp-top-overflow-section"><h2>{i18n.text(group.label)}</h2><MegaGroup group={group} composition={composition} onNavigate={onNavigate} /></section>)}</div>
  </ui.Popover>;
}

function MegaGroup({ group, composition, onNavigate }: { group: PortalNavigationGroup; composition: ShellLayoutProps["composition"]; onNavigate(id: string): void }) {
  const i18n = usePortalI18n();
  const activePageID = composition.activeNavigationPath?.pageID;
  return <div className="vp-top-mega">
    {group.pages.length === 0 ? null : <div className="vp-top-direct-pages">{group.pages.map((page) => <MegaLink key={page.id} page={page} href={pagePath(composition, page.id)} active={page.id === activePageID} onNavigate={onNavigate} />)}</div>}
    {group.children.length === 0 ? null : <div className="vp-top-child-grid">{group.children.map((child) => <section key={child.id} className="vp-top-child-group"><h3>{i18n.text(child.label)}</h3>{child.pages.map((page) => <MegaLink key={page.id} page={page} href={pagePath(composition, page.id)} active={page.id === activePageID} onNavigate={onNavigate} />)}</section>)}</div>}
  </div>;
}

function MegaLink({ page, href, active, onNavigate }: { page: PortalPageNavigation; href?: string; active: boolean; onNavigate(id: string): void }) {
  const i18n = usePortalI18n();
  return <a className="vp-top-mega-link" href={href} aria-current={active ? "page" : undefined} onClick={(event) => { event.preventDefault(); onNavigate(page.id); }}>{i18n.text(page.label)}</a>;
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

function groups(composition: ShellLayoutProps["composition"], zones: readonly NavigationZone[]): readonly PortalNavigationGroup[] { return zones.flatMap((zone) => composition.navigation[zone]); }
function pagePath(composition: ShellLayoutProps["composition"], navigationID: string): string | undefined { return composition.pages.find((candidate) => candidate.navigation?.id === navigationID)?.path; }
function Brand({ name, shortName, logoURL }: { name: string; shortName?: string; logoURL?: string }) { const label = shortName ?? name; return <div className="vp-top-brand" title={name}>{logoURL === undefined ? <span className="vp-top-brand-mark">{label.slice(0, 1).toUpperCase()}</span> : <img src={logoURL} alt="" className="vp-top-brand-logo" />}<strong>{label}</strong></div>; }
function shellSlot(values: ShellLayoutProps["composition"]["shellSlots"], id: ShellSlotID): ReactNode { return values[id]?.map((item) => createElement(item.component, { key: `${item.pluginID}/${item.id}` })); }
function pageSlot(values: ShellLayoutProps["composition"]["pageSlots"], id: PageSlotID): ReactNode { return values[id]?.map((item) => createElement(item.component, { key: item.id })); }

export const topNavigationShellCSS = `
.vp-top-shell{height:100vh;height:100dvh;display:flex;flex-direction:column;overflow:hidden;background:var(--vp-top-canvas);color:var(--vp-top-text)}
.vp-top-shell-header{height:var(--vp-top-bar-height);flex:0 0 var(--vp-top-bar-height);display:grid;grid-template-columns:minmax(180px,auto) 1fr minmax(180px,auto);align-items:center;gap:16px;padding:0 24px;background:var(--vp-top-surface);border-bottom:1px solid var(--vp-top-border)}.vp-top-shell-header-center{display:flex;justify-content:center}.vp-top-shell-header-end{display:flex;justify-content:flex-end}
.vp-top-bar{height:var(--vp-top-bar-height);flex:0 0 var(--vp-top-bar-height);display:grid;grid-template-columns:minmax(200px,auto) minmax(0,1fr) minmax(120px,auto);align-items:center;gap:16px;padding:0 20px;background:var(--vp-top-surface);border-bottom:1px solid var(--vp-top-border);z-index:20}.vp-top-start,.vp-top-end,.vp-top-center{display:flex;align-items:center;gap:6px;min-width:0}.vp-top-center{justify-content:center;overflow:hidden}.vp-top-end{justify-content:flex-end}.vp-top-brand{display:flex;align-items:center;gap:10px;min-width:0;white-space:nowrap}.vp-top-brand-mark,.vp-top-brand-logo{width:32px;height:32px;flex:0 0 32px}.vp-top-brand-mark{display:grid;place-items:center;border-radius:9px;background:var(--vp-top-primary);color:var(--vp-top-surface)}.vp-top-brand-logo{object-fit:contain}.vp-top-root-trigger,.vp-top-mobile-trigger{height:44px;min-width:44px;display:flex;align-items:center;justify-content:center;gap:7px;padding:0 12px;border:0;border-radius:9px;background:transparent;color:var(--vp-top-muted);font:inherit;cursor:pointer;white-space:nowrap}.vp-top-root-trigger:hover{background:var(--vp-top-hover);color:var(--vp-top-primary)}.vp-top-root-trigger[data-active]{background:var(--vp-top-selected);color:var(--vp-top-primary);font-weight:600}.vp-top-root-trigger:focus-visible,.vp-top-mobile-trigger:focus-visible,.vp-top-mega-link:focus-visible{outline:2px solid var(--vp-top-focus);outline-offset:2px}.vp-top-mobile-trigger{display:none}
.vp-top-mega,.vp-top-overflow-mega{box-sizing:border-box;width:clamp(var(--vp-top-mega-min),70vw,var(--vp-top-mega-max));max-height:min(70vh,720px);overflow:auto;padding:16px;background:var(--vp-top-overlay);box-shadow:var(--vp-top-overlay-shadow);color:var(--vp-top-text)}.vp-top-direct-pages{display:flex;flex-wrap:wrap;gap:8px;padding-bottom:12px;border-bottom:1px solid var(--vp-top-border);margin-bottom:12px}.vp-top-child-grid{display:grid;grid-template-columns:repeat(3,minmax(220px,1fr));gap:12px 16px}.vp-top-child-group{min-width:0}.vp-top-child-group h3,.vp-top-overflow-section h2{margin:0 0 8px;font-size:14px}.vp-top-mega-link{display:flex;align-items:center;min-height:44px;box-sizing:border-box;padding:7px 9px;border-radius:7px;color:var(--vp-top-text);text-decoration:none;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vp-top-mega-link:hover{background:var(--vp-top-hover);color:var(--vp-top-primary)}.vp-top-mega-link[aria-current=page]{background:var(--vp-top-selected);color:var(--vp-top-primary);font-weight:600}.vp-top-overflow-mega{display:grid;gap:20px}.vp-top-overflow-section+.vp-top-overflow-section{padding-top:16px;border-top:1px solid var(--vp-top-border)}.vp-top-overflow-section>.vp-top-mega{width:auto;max-height:none;overflow:visible;padding:0;box-shadow:none}
.vp-top-content{flex:1;min-width:0;min-height:0;display:flex;flex-direction:column}.vp-top-page-header{height:var(--vp-top-bar-height);min-height:var(--vp-top-bar-height);flex:0 0 var(--vp-top-bar-height);box-sizing:border-box;display:grid;grid-template-columns:minmax(0,1fr) auto minmax(0,1fr);align-items:center;gap:16px;padding:8px 24px;background:var(--vp-top-surface);border-bottom:1px solid var(--vp-top-border);z-index:10}.vp-top-page-header-side{display:flex;align-items:center;gap:12px;min-width:0}.vp-top-page-header-center{display:flex;justify-content:center;gap:12px}.vp-top-page-header-end{justify-content:flex-end}.vp-top-page-title-copy{min-width:0}.vp-top-page-title{font-size:22px;line-height:1.2;margin:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vp-top-page-description{font-size:14px;color:var(--vp-top-muted);margin:2px 0 0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vp-top-page-scroller{flex:1;min-height:0;overflow:auto;overscroll-behavior:contain}.vp-top-page{box-sizing:border-box;width:100%;margin:0 auto;padding:24px}.vp-top-page-body-row{display:flex;align-items:flex-start;gap:20px}.vp-top-page-body-main{flex:1;min-width:0}.vp-top-page-aside{width:320px;flex:0 0 320px;max-height:calc(100dvh - 144px);overflow:auto}
@media (max-width:1199px){.vp-top-bar{grid-template-columns:minmax(160px,auto) minmax(0,1fr) auto;padding:0 14px}.vp-top-root-trigger{padding:0 9px}.vp-top-child-grid{grid-template-columns:repeat(2,minmax(220px,1fr))}.vp-top-page{padding:20px}}
@media (max-width:767px){.vp-top-bar{grid-template-columns:minmax(0,1fr) auto;padding:0 12px}.vp-top-center,.vp-top-end{display:none}.vp-top-mobile-trigger{display:flex}.vp-top-brand strong{overflow:hidden;text-overflow:ellipsis}.vp-top-page-header{height:auto;min-height:var(--vp-top-bar-height);flex:0 0 auto;grid-template-columns:minmax(0,1fr) auto;padding:8px 16px}.vp-top-page-header-center{grid-column:1/-1;justify-content:flex-start;overflow-x:auto}.vp-top-page-header-end{grid-column:2}.vp-top-page-title{font-size:20px}.vp-top-page{padding:16px}.vp-top-page-body-row{display:block}.vp-top-page-aside{width:auto;max-height:none;margin-top:16px;overflow:visible}}
@media (max-width:520px){.vp-top-child-grid{grid-template-columns:minmax(0,1fr)}}
@media (prefers-reduced-motion:reduce){.vp-top-shell *{scroll-behavior:auto!important;transition:none!important}}
`;

const namespace = "com.vastplan.foundation.frontend.layout.top-navigation";
const adapter: ShellLayoutAdapter = {
  id: "ui.shell-layout", uiContract: "2.0.0", Shell: TopNavigationShell,
  localization: { defaultLocale: "zh-CN", messages: {
    "zh-CN": { "page.notFound": "页面不存在", "page.pathMissing": "Portal 没有注册路径 {path}", "navigation.main": "主导航", "navigation.open": "打开主菜单", "navigation.mobile": "移动主菜单", "navigation.more": "更多" },
    "en-US": { "page.notFound": "Page not found", "page.pathMissing": "Portal has no registered route for {path}", "navigation.main": "Main navigation", "navigation.open": "Open main menu", "navigation.mobile": "Mobile main menu", "navigation.more": "More" },
  } },
};
export default adapter;
