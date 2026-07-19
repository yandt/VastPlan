import { createElement, useEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent, type ReactNode } from "react";
import {
  usePortalUI,
  type MenuItem,
  type NavigationZone,
  type PageSlotID,
  type PortalNavigationGroup,
  type ShellLayoutAdapter,
  type ShellLayoutProps,
  type ShellSlotID,
} from "@vastplan/portal-ui";
import { hasRegionContent } from "./region-visibility";

const shellHeaderSlots = ["shell.header.start", "shell.header.center", "shell.header.end"] as const;
const shellNavigationSlots = ["shell.navigation.start", "shell.navigation.center", "shell.navigation.end"] as const;

function StandardShell({ composition, branding, config, pathname, recoveryNotice, onNavigate }: ShellLayoutProps) {
  const ui = usePortalUI();
  const [mobileOpen, setMobileOpen] = useState(false);
  const shellTheme = {
    "--vp-shell-canvas": ui.theme.tokens.color.canvas,
    "--vp-shell-surface": ui.theme.tokens.color.surface,
    "--vp-shell-text": ui.theme.tokens.color.text,
    "--vp-shell-muted": ui.theme.tokens.color.mutedText,
    "--vp-shell-border": ui.theme.tokens.color.border,
    "--vp-shell-primary": ui.theme.tokens.color.primary,
  } as CSSProperties;
  const pageWidth = config.pageBodyWidth === "contained" ? 1280 : undefined;
  const allGroups = useMemo(() => groups(composition, ["primary", "secondary", "settings"]), [composition]);
  const activeGroup = allGroups.find((group) => group.pages.some((item) => item.id === composition.activePage?.navigation?.id));
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
    <div className="vp-page-header-side">{pageSlot(composition.pageSlots, "page.header.start")}<div className="vp-page-title-copy"><h1 className="vp-page-title" tabIndex={-1}>{page.title}</h1>{page.description === undefined ? null : <p className="vp-page-description">{page.description}</p>}</div></div>
    <div className="vp-page-header-center">{pageSlot(composition.pageSlots, "page.header.center")}</div>
    <div className="vp-page-header-side vp-page-header-end">{pageSlot(composition.pageSlots, "page.header.end")}</div>
  </header>;
  const pageBody = <div className="vp-page-scroller"><main className="vp-page" style={{ maxWidth: pageWidth }}>
    {recoveryNotice}
    {page === undefined ? <ui.EmptyState title="页面不存在" description={`Portal 没有注册路径 ${pathname}`} /> : <>
      {pageSlot(composition.pageSlots, "page.body.before")}
      <div className="vp-page-body-row"><section className="vp-page-body-main">{pageSlot(composition.pageSlots, "page.body.main")}</section>{hasRegionContent(composition, { pageSlots: ["page.aside"] }) ? <aside className="vp-page-aside">{pageSlot(composition.pageSlots, "page.aside")}</aside> : null}</div>
      {pageSlot(composition.pageSlots, "page.body.after")}
    </>}
  </main></div>;

  const mobileItems: MenuItem[] = allGroups.map((group) => ({
    id: `group:${group.id}`,
    label: group.label,
    icon: <ui.Icon name={group.icon} label={group.label} />,
    children: group.pages.map((item) => ({ id: item.id, label: item.label, href: pagePath(composition, item.id) })),
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
        {navigationVisible ? <div className="vp-mobile-header"><button type="button" className="vp-mobile-menu-button" aria-label="打开主菜单" onClick={() => setMobileOpen(true)}><ui.Icon name="menu" /></button><Brand name={branding.name} shortName={branding.shortName} logoURL={branding.logoURL} /></div> : null}
        {pageHeader}
        {pageBody}
      </div>
    </div>
    {hasRegionContent(composition, { shellSlots: ["shell.footer"] }) ? <footer className="vp-shell-footer">{shellSlot(composition.shellSlots, "shell.footer")}</footer> : null}
    <ui.Drawer open={mobileOpen} title={branding.name} placement="left" width="sm" onClose={() => setMobileOpen(false)}>
      <nav aria-label="移动主菜单"><ui.Menu items={mobileItems} activeID={page?.navigation?.id} onSelect={navigate} /></nav>
    </ui.Drawer>
  </div>;
}

function DesktopNavigation({ branding, composition, mainGroups, settingsGroups, selectedGroup, onSelectGroup, onNavigate }: {
  branding: ReactNode;
  composition: ShellLayoutProps["composition"];
  mainGroups: readonly PortalNavigationGroup[];
  settingsGroups: readonly PortalNavigationGroup[];
  selectedGroup: PortalNavigationGroup | undefined;
  onSelectGroup(id: string): void;
  onNavigate(id: string): void;
}) {
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
    <aside className="vp-navigation-rail" aria-label="主菜单分组">
      <div className="vp-navigation-start">{branding}{shellSlot(composition.shellSlots, "shell.navigation.start")}</div>
      <div className="vp-navigation-center">{shellSlot(composition.shellSlots, "shell.navigation.center")}{mainGroups.map(groupButton)}</div>
      <div className="vp-navigation-end">{settingsGroups.map(groupButton)}{shellSlot(composition.shellSlots, "shell.navigation.end")}</div>
    </aside>
    {selectedGroup === undefined ? null : <aside id={panelID} ref={panelRef} className="vp-navigation-panel" aria-label={`${selectedGroup.label}二级导航`} onKeyDown={(event) => returnToRail(event, selectedButtonRef)}>
      <header className="vp-navigation-panel-header"><span className="vp-navigation-panel-icon"><IconForGroup group={selectedGroup} /></span><strong>{selectedGroup.label}</strong></header>
      <nav className="vp-navigation-panel-body" aria-label={selectedGroup.label}>
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
  return <button ref={buttonRef} type="button" className="vp-rail-button" data-selected={selected || undefined} aria-label={group.label} title={group.label} aria-pressed={selected} aria-controls={controls} onClick={onSelect} onKeyDown={(event) => {
    if (event.key === "ArrowRight" && selected) { event.preventDefault(); onOpen(); }
  }}><ui.Icon name={group.icon} size="lg" /></button>;
}

function IconForGroup({ group }: { group: PortalNavigationGroup }) {
  const ui = usePortalUI();
  return <ui.Icon name={group.icon} />;
}

function SecondLevelMenu({ group, composition, onNavigate }: { group: PortalNavigationGroup; composition: ShellLayoutProps["composition"]; onNavigate(id: string): void }) {
  const ui = usePortalUI();
  const items = group.pages.map((item) => ({ id: item.id, label: item.label, href: pagePath(composition, item.id) }));
  return <ui.Menu items={items} activeID={composition.activePage?.navigation?.id} onSelect={onNavigate} />;
}

function returnToRail(event: KeyboardEvent<HTMLElement>, button: React.RefObject<HTMLButtonElement>) {
  if (event.key !== "ArrowLeft") return;
  event.preventDefault();
  button.current?.focus();
}

export function groups(composition: ShellLayoutProps["composition"], zones: readonly NavigationZone[]): readonly PortalNavigationGroup[] {
  return zones.flatMap((zone) => composition.navigation[zone]);
}

function pagePath(composition: ShellLayoutProps["composition"], navigationID: string): string | undefined {
  return composition.pages.find((candidate) => candidate.navigation?.id === navigationID)?.path;
}

function Brand({ name, shortName, logoURL, compact = false }: { name: string; shortName?: string; logoURL?: string; compact?: boolean }) {
  const label = shortName ?? name;
  return <div className={`vp-brand${compact ? " vp-brand-compact" : ""}`} title={name}>{logoURL === undefined ? <span className="vp-brand-mark">{label.slice(0, 1).toUpperCase()}</span> : <img src={logoURL} alt="" className="vp-brand-logo" />}{compact ? null : <strong>{label}</strong>}</div>;
}

function shellSlot(values: ShellLayoutProps["composition"]["shellSlots"], id: ShellSlotID): ReactNode {
  return values[id]?.map((item) => createElement(item.component, { key: `${item.pluginID}/${item.id}` }));
}

function pageSlot(values: ShellLayoutProps["composition"]["pageSlots"], id: PageSlotID): ReactNode {
  return values[id]?.map((item) => createElement(item.component, { key: item.id }));
}

export const standardShellCSS = `
.vp-shell-root{--vp-shell-bar-height:64px;height:100vh;height:100dvh;display:flex;flex-direction:column;overflow:hidden;background:var(--vp-shell-canvas);color:var(--vp-shell-text)}
.vp-shell-header{height:56px;flex:0 0 56px;display:grid;grid-template-columns:minmax(180px,auto) 1fr minmax(180px,auto);align-items:center;gap:16px;padding:0 24px;background:var(--vp-shell-surface);border-bottom:1px solid var(--vp-shell-border);z-index:20}.vp-shell-header-side{display:flex;align-items:center;gap:12px}.vp-shell-header-center{display:flex;justify-content:center;min-width:0}.vp-shell-header-end{justify-content:flex-end}
.vp-shell-frame{display:flex;flex:1;min-height:0;min-width:0}.vp-desktop-navigation{display:flex;flex:0 0 auto;min-height:0}.vp-navigation-rail{width:64px;flex:0 0 64px;min-height:0;display:grid;grid-template-rows:auto minmax(0,1fr) auto;background:var(--vp-shell-surface);border-right:1px solid var(--vp-shell-border)}.vp-navigation-start,.vp-navigation-center,.vp-navigation-end{display:flex;flex-direction:column;align-items:center;gap:8px;padding:8px}.vp-navigation-center{overflow-y:auto;overscroll-behavior:contain;scrollbar-width:thin}.vp-navigation-start{box-sizing:border-box;height:var(--vp-shell-bar-height);min-height:var(--vp-shell-bar-height);overflow:hidden;border-bottom:1px solid var(--vp-shell-border)}.vp-navigation-end{border-top:1px solid var(--vp-shell-border);max-height:40vh;overflow-y:auto}
.vp-rail-button{width:44px;height:44px;flex:0 0 44px;display:grid;place-items:center;border:0;border-radius:10px;background:transparent;color:var(--vp-shell-muted);cursor:pointer}.vp-rail-button:hover{background:color-mix(in srgb,var(--vp-shell-primary) 8%,transparent);color:var(--vp-shell-primary)}.vp-rail-button[data-selected]{background:color-mix(in srgb,var(--vp-shell-primary) 12%,transparent);color:var(--vp-shell-primary)}.vp-rail-button:focus-visible,.vp-mobile-menu-button:focus-visible{outline:2px solid var(--vp-shell-primary);outline-offset:2px}
.vp-navigation-panel{width:240px;flex:0 0 240px;min-height:0;display:grid;grid-template-rows:auto minmax(0,1fr);background:var(--vp-shell-surface);border-right:1px solid var(--vp-shell-border)}.vp-navigation-panel-header{box-sizing:border-box;height:var(--vp-shell-bar-height);min-height:var(--vp-shell-bar-height);display:flex;align-items:center;gap:10px;padding:8px 16px;border-bottom:1px solid var(--vp-shell-border)}.vp-navigation-panel-icon{color:var(--vp-shell-primary)}.vp-navigation-panel-body{min-height:0;overflow-y:auto;overscroll-behavior:contain;padding:8px;scrollbar-width:thin}
.vp-shell-content{flex:1;min-width:0;min-height:0;display:flex;flex-direction:column}.vp-mobile-header{display:none}.vp-page-header{box-sizing:border-box;height:var(--vp-shell-bar-height);min-height:var(--vp-shell-bar-height);flex:0 0 var(--vp-shell-bar-height);display:grid;grid-template-columns:minmax(0,1fr) auto minmax(0,1fr);align-items:center;gap:16px;padding:8px 24px;background:var(--vp-shell-surface);border-bottom:1px solid var(--vp-shell-border);z-index:10}.vp-page-header-side{display:flex;align-items:center;gap:12px;min-width:0}.vp-page-header-center{display:flex;justify-content:center;gap:12px;min-width:0}.vp-page-header-end{justify-content:flex-end}.vp-page-title-copy{min-width:0}.vp-page-title{font-size:22px;line-height:1.2;margin:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vp-page-description{font-size:14px;line-height:1.3;color:var(--vp-shell-muted);margin:2px 0 0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vp-page-scroller{flex:1;min-height:0;overflow:auto;overscroll-behavior:contain;background:color-mix(in srgb,var(--vp-shell-surface) 97%,var(--vp-shell-text))}.vp-page{box-sizing:border-box;width:100%;margin:0 auto;padding:24px}.vp-page-body-row{display:flex;align-items:flex-start;gap:20px}.vp-page-body-main{flex:1;min-width:0}.vp-page-aside{width:320px;flex:0 0 320px;max-height:calc(100dvh - 120px);overflow:auto}.vp-shell-footer{flex:0 0 auto}.vp-brand{display:flex;align-items:center;gap:10px;min-height:40px;min-width:0}.vp-brand-compact{justify-content:center}.vp-brand-mark{width:32px;height:32px;flex:0 0 32px;border-radius:9px;display:grid;place-items:center;color:var(--vp-shell-surface);background:var(--vp-shell-primary)}.vp-brand-logo{width:32px;height:32px;object-fit:contain}.vp-mobile-menu-button{width:44px;height:44px;border:0;border-radius:8px;background:transparent;color:var(--vp-shell-text);display:grid;place-items:center}
@media (max-width:1199px){.vp-navigation-panel{width:220px;flex-basis:220px}.vp-page{padding:20px}.vp-page-header{padding-left:20px;padding-right:20px}}
@media (max-width:767px){.vp-desktop-navigation{display:none}.vp-mobile-header{box-sizing:border-box;height:56px;flex:0 0 56px;display:flex;align-items:center;gap:8px;padding:0 12px;background:var(--vp-shell-surface);border-bottom:1px solid var(--vp-shell-border)}.vp-page-header{height:auto;min-height:64px;flex:0 0 auto;grid-template-columns:minmax(0,1fr) auto;padding:8px 16px}.vp-page-header-center{grid-column:1/-1;justify-content:flex-start;overflow-x:auto}.vp-page-header-end{grid-column:2}.vp-page-title{font-size:20px}.vp-page-description{max-width:65vw}.vp-page{padding:16px}.vp-page-body-row{display:block}.vp-page-aside{width:auto;max-height:none;margin-top:16px;overflow:visible}}
@media (prefers-reduced-motion:reduce){.vp-shell-root *{scroll-behavior:auto!important;transition:none!important}}
`;

const adapter: ShellLayoutAdapter = { id: "ui.shell-layout", uiContract: "1.0.0", Shell: StandardShell };
export default adapter;
