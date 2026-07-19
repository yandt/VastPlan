import { createElement, type ReactNode } from "react";
import { usePortalUI, type NavigationZone, type ShellLayoutAdapter, type ShellLayoutProps, type ShellSlotID } from "@vastplan/portal-ui";
import { hasRegionContent } from "./region-visibility";

const shellHeaderSlots = ["shell.header.start", "shell.header.center", "shell.header.end"] as const;
const shellNavigationSlots = ["shell.navigation.before", "shell.navigation.after"] as const;

function StandardShell({ composition, branding, config, pathname, recoveryNotice, onNavigate }: ShellLayoutProps) {
  const ui = usePortalUI();
  const navigationMode = config.navigation === "top" ? "top" : "sidebar";
  const logoPlacement = config.logoPlacement === "header" ? "header" : "navigation";
  const settingsAtBottom = config.settingsPlacement !== "with-primary";
  const pageWidth = config.pageBodyWidth === "contained" ? 1280 : undefined;
  const brand = <Brand name={branding.name} shortName={branding.shortName} logoURL={branding.logoURL} />;
  const menu = (zones: readonly NavigationZone[]) => <Navigation zones={zones} composition={composition} onNavigate={onNavigate} />;
  const mainNavigationZones: readonly NavigationZone[] = settingsAtBottom ? ["primary", "secondary"] : ["primary", "secondary", "settings"];
  const shellHeaderVisible = hasRegionContent(composition, {
    intrinsic: logoPlacement === "header",
    navigationZones: navigationMode === "top" ? mainNavigationZones : [],
    slots: shellHeaderSlots,
  });
  const sidebarVisible = navigationMode === "sidebar" && hasRegionContent(composition, {
    intrinsic: logoPlacement === "navigation",
    navigationZones: ["primary", "secondary", "settings"],
    slots: shellNavigationSlots,
  });
  const settingsNavigationVisible = hasRegionContent(composition, { navigationZones: ["settings"] });
  const topSettingsVisible = navigationMode === "top" && settingsAtBottom && settingsNavigationVisible;
  const header = shellHeaderVisible ? <header style={styles.shellHeader}>
    <div style={styles.headerSide}>{slot(composition.slots, "shell.header.start")}{logoPlacement === "header" ? brand : null}</div>
    <div style={styles.headerCenter}>{slot(composition.slots, "shell.header.center")}{navigationMode === "top" ? menu(mainNavigationZones) : null}</div>
    <div style={styles.headerSide}>{slot(composition.slots, "shell.header.end")}</div>
  </header> : null;
  const page = composition.activePage;
  const body = <main style={{ ...styles.page, maxWidth: pageWidth }}>
    {recoveryNotice}
    {page === undefined ? <ui.EmptyState title="页面不存在" description={`Portal 没有注册路径 ${pathname}`} /> : <>
      <header style={styles.pageHeader}>
        <div style={styles.pageHeaderSide}>{slot(composition.slots, "page.header.start")}<div><h1 style={styles.title}>{page.title}</h1>{page.description === undefined ? null : <p style={styles.description}>{page.description}</p>}</div></div>
        <div style={styles.pageHeaderCenter}>{slot(composition.slots, "page.header.center")}</div>
        <div style={styles.pageHeaderSide}>{slot(composition.slots, "page.header.end")}</div>
      </header>
      {slot(composition.slots, "page.body.before")}
      <div style={styles.bodyRow}><section style={styles.bodyMain}>{slot(composition.slots, "page.body.main")}</section>{hasRegionContent(composition, { slots: ["page.aside"] }) ? <aside style={styles.aside}>{slot(composition.slots, "page.aside")}</aside> : null}</div>
      {slot(composition.slots, "page.body.after")}
    </>}
  </main>;
  return <div style={styles.root}>{header}<div style={{ ...styles.shellBody, minHeight: shellHeaderVisible ? "calc(100vh - 57px)" : "100vh" }}>
    {sidebarVisible ? <aside style={styles.navigation}>
      {logoPlacement === "navigation" ? brand : null}{slot(composition.slots, "shell.navigation.before")}
      <div>{menu(mainNavigationZones)}</div>
      {settingsAtBottom && settingsNavigationVisible ? <div style={styles.settings}>{menu(["settings"])}</div> : null}
      {slot(composition.slots, "shell.navigation.after")}
    </aside> : null}
    <div style={styles.content}>{topSettingsVisible ? <div style={styles.topSettings}>{menu(["settings"])}</div> : null}{body}</div>
  </div>{hasRegionContent(composition, { slots: ["shell.footer"] }) ? slot(composition.slots, "shell.footer") : null}</div>;
}

function Navigation({ zones, composition, onNavigate }: Pick<ShellLayoutProps, "composition" | "onNavigate"> & { zones: readonly NavigationZone[] }) {
  const ui = usePortalUI();
  const items = zones.flatMap((zone) => composition.navigation[zone]).map((item) => ({ id: item.id, label: item.label }));
  if (items.length === 0) return null;
  const activeID = composition.activePage?.navigation?.id;
  return <ui.Menu items={items} activeID={activeID} onSelect={(navigationID) => {
    const page = composition.pages.find((candidate) => candidate.navigation?.id === navigationID);
    if (page !== undefined) onNavigate(page.id);
  }} />;
}

function Brand({ name, shortName, logoURL }: { name: string; shortName?: string; logoURL?: string }) {
  return <div style={styles.brand}>{logoURL === undefined ? <span style={styles.brandMark}>{(shortName ?? name).slice(0, 1).toUpperCase()}</span> : <img src={logoURL} alt="" style={styles.logo} />}<strong>{shortName ?? name}</strong></div>;
}

function slot(values: ShellLayoutProps["composition"]["slots"], id: ShellSlotID): ReactNode {
  return values[id]?.map((item) => createElement(item.component, { key: item.id }));
}

const styles = {
  root: { minHeight: "100vh", background: "#f7f8fa", color: "#1d2129" },
  shellHeader: { height: 56, display: "grid", gridTemplateColumns: "minmax(180px, auto) 1fr minmax(180px, auto)", alignItems: "center", gap: 16, padding: "0 24px", background: "#fff", borderBottom: "1px solid #e5e6eb", position: "sticky", top: 0, zIndex: 20 },
  headerSide: { display: "flex", alignItems: "center", gap: 12 }, headerCenter: { display: "flex", justifyContent: "center", minWidth: 0 },
  shellBody: { display: "flex" }, navigation: { width: 240, padding: 16, background: "#fff", borderRight: "1px solid #e5e6eb", display: "flex", flexDirection: "column", gap: 16 },
  settings: { marginTop: "auto", paddingTop: 16, borderTop: "1px solid #e5e6eb" }, topSettings: { display: "flex", justifyContent: "flex-end", background: "#fff", padding: "4px 24px", borderBottom: "1px solid #e5e6eb" },
  content: { flex: 1, minWidth: 0 }, page: { margin: "0 auto", padding: 24 }, pageHeader: { minHeight: 56, display: "grid", gridTemplateColumns: "minmax(0, 1fr) auto minmax(0, 1fr)", alignItems: "center", gap: 16, marginBottom: 20 },
  pageHeaderSide: { display: "flex", alignItems: "center", gap: 12 }, pageHeaderCenter: { display: "flex", justifyContent: "center", gap: 12 }, title: { fontSize: 24, lineHeight: 1.3, margin: 0 }, description: { color: "#86909c", margin: "6px 0 0" },
  bodyRow: { display: "flex", alignItems: "flex-start", gap: 20 }, bodyMain: { flex: 1, minWidth: 0 }, aside: { width: 320, flex: "0 0 320px" },
  brand: { display: "flex", alignItems: "center", gap: 10, minHeight: 40 }, brandMark: { width: 32, height: 32, borderRadius: 9, display: "grid", placeItems: "center", color: "#fff", background: "#165dff" }, logo: { width: 32, height: 32, objectFit: "contain" },
} as const;

const adapter: ShellLayoutAdapter = { id: "ui.shell-layout", uiContract: "1.0.0", Shell: StandardShell };
export default adapter;
