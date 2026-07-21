import { createElement, useEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent, type ReactNode } from "react";
import {
  message,
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

export function StandardShell({ composition, branding, template, pathname, recoveryNotice, onNavigate }: UIShellProps) {
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
  } as CSSProperties;
  const pageWidth = template.options.pageBodyWidth === "contained" ? 1280 : undefined;
  const allGroups = useMemo(() => groups(composition, ["primary", "secondary", "settings"]), [composition]);
  const activeGroup = allGroups.find((group) => group.id === composition.activeNavigationPath?.rootGroupID);
  const groupKey = allGroups.map((group) => group.id).join("\u0000");
  const [selectedGroupID, setSelectedGroupID] = useState(activeGroup?.id ?? allGroups[0]?.id);
  useEffect(() => {
    setSelectedGroupID((selected) => activeGroup?.id ?? (allGroups.some((group) => group.id === selected) ? selected : allGroups[0]?.id));
  }, [activeGroup?.id, groupKey]);
  const selectedGroup = allGroups.find((group) => group.id === selectedGroupID) ?? allGroups[0];
  const shellHeaderVisible = hasRegionContent(composition, { shellSlots: shellHeaderSlots });
  const navigationVisible = hasRegionContent(composition, { intrinsic: branding.name !== "", navigationGroups: true, shellSlots: shellNavigationSlots });
  const settingsGroups = composition.navigation.settings;
  const mainGroups = [...composition.navigation.primary, ...composition.navigation.secondary];
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

  const page = composition.activePage;
  const pageHeader = page === undefined ? null : <header className="vp-page-header">
    <div className="vp-page-header-side">{pageSlot(composition.pageSlots, "page.header.start")}<div className="vp-page-title-copy"><h1 className="vp-page-title" tabIndex={-1}>{i18n.text(page.title)}</h1>{page.description === undefined ? null : <p className="vp-page-description">{i18n.text(page.description)}</p>}</div></div>
    <div className="vp-page-header-center">{pageSlot(composition.pageSlots, "page.header.center")}</div>
    <div className="vp-page-header-side vp-page-header-end">{pageSlot(composition.pageSlots, "page.header.end")}</div>
  </header>;
  const pageBody = <div className="vp-page-scroller"><main className="vp-page" style={{ maxWidth: pageWidth }}>
    {recoveryNotice}
    {page === undefined ? <ui.EmptyState title={i18n.text(message(namespace, "page.notFound", "页面不存在"))} description={i18n.text(message(namespace, "page.pathMissing", "Portal 没有注册路径 {path}", { path: pathname }))} /> : <>
      {pageSlot(composition.pageSlots, "page.body.before")}
      <div className="vp-page-body-row"><section className="vp-page-body-main">{pageSlot(composition.pageSlots, "page.body.main")}</section>{hasRegionContent(composition, { pageSlots: ["page.aside"] }) ? <aside className="vp-page-aside">{pageSlot(composition.pageSlots, "page.aside")}</aside> : null}</div>
      {pageSlot(composition.pageSlots, "page.body.after")}
    </>}
  </main></div>;

  const mobileItems: MenuItem[] = allGroups.map((group) => ({
    id: `group:${group.id}`,
    label: i18n.text(group.label),
    icon: <ui.Icon name={group.icon} label={i18n.text(group.label)} />,
    children: [
      ...group.pages.map((item) => ({ id: item.id, label: i18n.text(item.label), href: pagePath(composition, item.id) })),
      ...group.children.map((child) => ({ id: `group:${child.id}`, label: i18n.text(child.label), children: child.pages.map((item) => ({ id: item.id, label: i18n.text(item.label), href: pagePath(composition, item.id) })) })),
    ],
  }));

  return <div className="vp-shell-root" style={shellTheme}>
    <style>{standardShellCSS}</style>
    {header}
    <div className="vp-shell-frame">
      {navigationVisible ? <DesktopNavigation
        branding={brand}
        composition={composition}
        mainGroups={mainGroups}
        settingsGroups={settingsGroups}
        selectedGroup={selectedGroup}
        onSelectGroup={setSelectedGroupID}
        onNavigate={navigate}
      /> : null}
      <div className="vp-shell-content">
        {navigationVisible ? <div className="vp-mobile-header"><button type="button" className="vp-mobile-menu-button" aria-label={i18n.text(message(namespace, "navigation.open", "打开主菜单"))} onClick={() => setMobileOpen(true)}><ui.Icon name="menu" /></button><Brand name={branding.name} shortName={branding.shortName} logoURL={branding.logoURL} /></div> : null}
        {pageHeader}
        {pageBody}
      </div>
    </div>
    {hasRegionContent(composition, { shellSlots: ["shell.footer"] }) ? <footer className="vp-shell-footer">{shellSlot(composition.shellSlots, "shell.footer")}</footer> : null}
    <ui.Drawer open={mobileOpen} title={branding.name} placement="left" width="sm" onClose={() => setMobileOpen(false)}>
      <nav aria-label={i18n.text(message(namespace, "navigation.mobile", "移动主菜单"))}><ui.Menu items={mobileItems} activeID={page?.navigation?.id} onSelect={navigate} /></nav>
    </ui.Drawer>
  </div>;
}

function DesktopNavigation({ branding, composition, mainGroups, settingsGroups, selectedGroup, onSelectGroup, onNavigate }: {
  branding: ReactNode;
  composition: UIShellProps["composition"];
  mainGroups: readonly PortalNavigationGroup[];
  settingsGroups: readonly PortalNavigationGroup[];
  selectedGroup: PortalNavigationGroup | undefined;
  onSelectGroup(id: string): void;
  onNavigate(id: string): void;
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
      <div className="vp-navigation-end">{settingsGroups.map(groupButton)}{shellSlot(composition.shellSlots, "shell.navigation.end")}</div>
    </aside>
    {selectedGroup === undefined ? null : <aside id={panelID} ref={panelRef} className="vp-navigation-panel" aria-label={i18n.text(message(namespace, "navigation.secondaryLabel", "{group}二级导航", { group: i18n.text(selectedGroup.label) }))} onKeyDown={(event) => returnToRail(event, selectedButtonRef)}>
      <header className="vp-navigation-panel-header"><span className="vp-navigation-panel-icon"><IconForGroup group={selectedGroup} /></span><strong>{i18n.text(selectedGroup.label)}</strong></header>
      <nav className="vp-navigation-panel-body" aria-label={i18n.text(selectedGroup.label)}>
        <SecondLevelMenu group={selectedGroup} composition={composition} onNavigate={onNavigate} />
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
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const label = i18n.text(group.label);
  return <button ref={buttonRef} type="button" className="vp-rail-button" data-selected={selected || undefined} aria-label={label} title={label} aria-pressed={selected} aria-controls={controls} onClick={onSelect} onKeyDown={(event) => {
    if (event.key === "ArrowRight" && selected) { event.preventDefault(); onOpen(); }
  }}><ui.Icon name={group.icon} size="lg" /></button>;
}

function IconForGroup({ group }: { group: PortalNavigationGroup }) {
  const ui = usePortalUI();
  return <ui.Icon name={group.icon} />;
}

function SecondLevelMenu({ group, composition, onNavigate }: { group: PortalNavigationGroup; composition: UIShellProps["composition"]; onNavigate(id: string): void }) {
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
  </div>;
}

function NavigationLink({ id, label, href, active, onNavigate }: { id: string; label: string; href?: string; active: boolean; onNavigate(id: string): void }) {
  return <li><a className="vp-navigation-link" href={href} aria-current={active ? "page" : undefined} onClick={(event) => {
    event.preventDefault();
    onNavigate(id);
  }}>{label}</a></li>;
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
.vp-navigation-tree{display:grid;gap:4px}.vp-navigation-page-list{list-style:none;margin:0;padding:4px 0 8px}.vp-navigation-root-pages{border-bottom:1px solid var(--vp-shell-border);margin-bottom:4px}.vp-navigation-link{display:flex;align-items:center;min-height:var(--vp-shell-touch-minimum);box-sizing:border-box;padding:8px 12px;border-radius:8px;color:var(--vp-shell-text);text-decoration:none;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vp-navigation-link:hover{background:var(--vp-shell-hover);color:var(--vp-shell-primary)}.vp-navigation-link[aria-current=page]{background:var(--vp-shell-selected);color:var(--vp-shell-primary);font-weight:600}.vp-navigation-child{border-radius:8px}.vp-navigation-child[data-active]{background:color-mix(in srgb,var(--vp-shell-selected) 35%,transparent)}.vp-navigation-child-trigger{width:100%;min-height:var(--vp-shell-touch-minimum);display:flex;align-items:center;justify-content:space-between;gap:8px;padding:8px 10px;border:0;border-radius:8px;background:transparent;color:var(--vp-shell-text);font:inherit;font-weight:600;text-align:left;cursor:pointer}.vp-navigation-child-trigger:hover{background:var(--vp-shell-hover)}.vp-navigation-chevron{color:var(--vp-shell-muted);transition:transform var(--vp-shell-motion-fast) ease}.vp-navigation-child-trigger[aria-expanded=true] .vp-navigation-chevron{transform:rotate(90deg)}.vp-navigation-child .vp-navigation-page-list{padding-left:8px}
.vp-shell-content{flex:1;min-width:0;min-height:0;display:flex;flex-direction:column}.vp-mobile-header{display:none}.vp-page-header{box-sizing:border-box;height:var(--vp-shell-bar-height);min-height:var(--vp-shell-bar-height);flex:0 0 var(--vp-shell-bar-height);display:grid;grid-template-columns:minmax(0,1fr) auto minmax(0,1fr);align-items:center;gap:16px;padding:8px 24px;background:var(--vp-shell-surface);border-bottom:1px solid var(--vp-shell-border);z-index:10}.vp-page-header-side{display:flex;align-items:center;gap:12px;min-width:0}.vp-page-header-center{display:flex;justify-content:center;gap:12px;min-width:0}.vp-page-header-end{justify-content:flex-end}.vp-page-title-copy{min-width:0}.vp-page-title{font-size:22px;line-height:1.2;margin:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vp-page-description{font-size:14px;line-height:1.3;color:var(--vp-shell-muted);margin:2px 0 0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vp-page-scroller{flex:1;min-height:0;overflow:auto;overscroll-behavior:contain;background:color-mix(in srgb,var(--vp-shell-surface) 97%,var(--vp-shell-text))}.vp-page{box-sizing:border-box;width:100%;margin:0 auto;padding:24px}.vp-page-body-row{display:flex;align-items:flex-start;gap:20px}.vp-page-body-main{flex:1;min-width:0}.vp-page-aside{width:320px;flex:0 0 320px;max-height:calc(100dvh - 120px);overflow:auto}.vp-shell-footer{flex:0 0 auto}.vp-brand{display:flex;align-items:center;gap:10px;min-height:40px;min-width:0}.vp-brand-compact{justify-content:center}.vp-brand-mark{width:32px;height:32px;flex:0 0 32px;border-radius:9px;display:grid;place-items:center;color:var(--vp-shell-surface);background:var(--vp-shell-primary)}.vp-brand-logo{width:32px;height:32px;object-fit:contain}.vp-mobile-menu-button{width:44px;height:44px;border:0;border-radius:8px;background:transparent;color:var(--vp-shell-text);display:grid;place-items:center}
@media (max-width:1199px){.vp-navigation-panel{width:var(--vp-shell-navigation-compact-width);flex-basis:var(--vp-shell-navigation-compact-width)}.vp-page{padding:20px}.vp-page-header{padding-left:20px;padding-right:20px}}
@media (max-width:767px){.vp-desktop-navigation{display:none}.vp-mobile-header{box-sizing:border-box;height:var(--vp-shell-bar-height);flex:0 0 var(--vp-shell-bar-height);display:flex;align-items:center;gap:8px;padding:0 12px;background:var(--vp-shell-surface);border-bottom:1px solid var(--vp-shell-border)}.vp-page-header{height:auto;min-height:var(--vp-shell-bar-height);flex:0 0 auto;grid-template-columns:minmax(0,1fr) auto;padding:8px 16px}.vp-page-header-center{grid-column:1/-1;justify-content:flex-start;overflow-x:auto}.vp-page-header-end{grid-column:2}.vp-page-title{font-size:20px}.vp-page-description{max-width:65vw}.vp-page{padding:16px}.vp-page-body-row{display:block}.vp-page-aside{width:auto;max-height:none;margin-top:16px;overflow:visible}}
@media (prefers-reduced-motion:reduce){.vp-shell-root *{scroll-behavior:auto!important;transition:none!important}}
`;

const namespace = "cn.vastplan.foundation.frontend.structure.layout.standard";
export const shellLibrary = {
  id: "standard", shell: "ui.structure.shell", uiContract: "4.0.0", Shell: StandardShell,
  localization: {
    defaultLocale: "zh-CN",
    messages: {
      "zh-CN": { "page.notFound": "页面不存在", "page.pathMissing": "Portal 没有注册路径 {path}", "navigation.open": "打开主菜单", "navigation.mobile": "移动主菜单", "navigation.groups": "主菜单分组", "navigation.secondaryLabel": "{group}导航" },
      "en-US": { "page.notFound": "Page not found", "page.pathMissing": "Portal has no registered route for {path}", "navigation.open": "Open main menu", "navigation.mobile": "Mobile main menu", "navigation.groups": "Main menu groups", "navigation.secondaryLabel": "{group} navigation" },
    },
  },
};
export const localization = shellLibrary.localization;
export default shellLibrary;
