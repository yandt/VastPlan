import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { PortalI18nProvider, type PortalUI } from "@vastplan/ui-primitives";
import { muiIconForTheme, muiRenderAdapter, muiPortalUIComponents } from "./index";

describe("MUI portal UI adapter", () => {
  it("implements the full framework-neutral component surface", () => {
    const required: Array<keyof Omit<PortalUI, "notify" | "confirm" | "theme">> = [
      "PortalShell", "Page", "Panel", "Stack", "Grid", "GridItem", "Divider", "Button", "IconButton", "Select", "Menu", "Breadcrumb", "Tabs", "CommandPalette", "Popover", "Dialog", "Drawer", "FormRenderer", "FilterBar", "Table", "DataCard", "SplitView", "RecordNavigationList", "RecordTree", "Pagination", "Descriptions", "Status", "Icon", "EmptyState", "ErrorState", "Skeleton", "Busy",
    ];
    expect(muiRenderAdapter).toMatchObject({ id: "mui", framework: "mui" });
    expect(required.every((name) => typeof muiPortalUIComponents[name] === "function")).toBe(true);
  });

  it("uses the same VastPlan-owned SVG icon geometry", () => {
    const Icon = muiPortalUIComponents.Icon;
    const markup = renderToStaticMarkup(<Icon name="publish" label="Publish" />);
    expect(markup).toContain('data-vastplan-icon="publish"');
    expect(markup).toContain('aria-label="Publish"');
  });

  it("maps the shared three-step component size scale", () => {
    const IconButton = muiPortalUIComponents.IconButton;
    const markup = renderToStaticMarkup(<><IconButton size="sm" icon="edit" label="Small" /><IconButton size="md" icon="edit" label="Medium" /><IconButton size="lg" icon="edit" label="Large" /></>);
    expect(markup).toContain("width:18px;height:18px");
    expect(markup).toContain("width:32px;height:32px");
    expect(markup).toContain("width:44px;height:44px");
  });

  it("maps the complete content-sized action menu recipe", () => {
    const Menu = muiPortalUIComponents.Menu;
    const markup = renderToStaticMarkup(<Menu size="sm" variant="action" items={[{ id: "submit", label: "Submit" }]} />);
    expect(markup).toContain("width:max-content");
    expect(markup).toContain("min-width:112px");
    expect(markup).toContain("max-width:280px");
    expect(markup).toContain("padding:4px");
    expect(markup).toContain("overflow:hidden");
    expect(markup).toContain("display:flex");
    expect(markup).toContain("width:100%");
    expect(markup).toContain("gap:6px");
    expect(markup).toContain("padding-inline:12px 6px");
  });

  it("offers a Material-native icon theme behind the same semantic name", () => {
    const NativeIcon = muiIconForTheme("renderer-native");
    const markup = renderToStaticMarkup(<><NativeIcon name="publish" label="Publish" /><NativeIcon name="visibilityOff" label="Hidden" /><NativeIcon name="drag" label="Reorder" /></>);
    expect(markup).toContain('data-vastplan-icon="publish"');
    expect(markup).toContain('data-vastplan-icon="visibilityOff"');
    expect(markup).toContain('data-vastplan-icon="drag"');
    expect(markup).toContain('data-vastplan-icon-source="renderer-native"');
    expect(muiRenderAdapter.iconThemes.map((theme) => theme.id)).toEqual(["canonical", "renderer-native"]);
    expect(muiIconForTheme("missing")).toBe(muiPortalUIComponents.Icon);
  });

  it("renders semantic UI without exposing MUI types to consumers", () => {
    const Page = muiPortalUIComponents.Page;
    const Button = muiPortalUIComponents.Button;
    const markup = renderToStaticMarkup(<Page title="Portal"><Button kind="primary">保存</Button></Page>);
    expect(markup).toContain("Portal");
    expect(markup).toContain("保存");
  });

  it("renders accessible record navigation and tree semantics", () => {
    const List = muiPortalUIComponents.RecordNavigationList;
    const Tree = muiPortalUIComponents.RecordTree;
    const markup = renderToStaticMarkup(<><List ariaLabel="Services" items={[{ id: "one", title: "One" }]} selectedID="one" onSelect={() => undefined} /><Tree ariaLabel="Tree" items={[{ id: "root", title: "Root", children: [{ id: "leaf", title: "Leaf" }] }]} expandedIDs={["root"]} onSelect={() => undefined} onExpandedChange={() => undefined} /></>);
    expect(markup).toContain('role="listbox"');
    expect(markup).toContain('role="tree"');
    expect(markup).toContain('aria-expanded="true"');
  });

  it("maps the same shell, overlay, focus and touch baselines as other adapters", () => {
    expect(muiPortalUIComponents.theme.tokens).toMatchObject({
      shell: { barHeight: 64, railWidth: 64, navigationWidth: 240, navigationCompactWidth: 220 },
      overlay: { navigationMinWidth: 480, navigationMaxWidth: 840 },
      focus: { width: 2 }, touch: { minimum: 44 }, motion: { fast: 120, normal: 180 },
    });
  });

  it("keeps navigation destinations as real links", () => {
    const Menu = muiPortalUIComponents.Menu;
    const markup = renderToStaticMarkup(<Menu items={[{ id: "settings", label: "设置", href: "/settings" }]} />);
    expect(markup).toContain('href="/settings"');
  });

  it("maps the semantic data card to native selectable MUI markup", () => {
    const DataCard = muiPortalUIComponents.DataCard;
    const markup = renderToStaticMarkup(<DataCard title="Node A" subtitle="linux" status="Ready" selectable selected selectionLabel="Select Node A">4 cores</DataCard>);
    expect(markup).toContain("Node A");
    expect(markup).toContain("Select Node A");
    expect(markup).toContain("MuiCard-root");
    expect(markup).toContain("Mui-checked");
  });

  it("renders the same governed form section semantics", () => {
    const Form = muiPortalUIComponents.FormRenderer;
    const markup = renderToStaticMarkup(<PortalI18nProvider policy={{ defaultLocale: "en-US", supportedLocales: ["en-US"] }} catalogs={{}} candidates={["en-US"]}><Form
      schema={{ id: "node", schema: { $schema: "http://json-schema.org/draft-07/schema#", type: "object", properties: { name: { type: "string", title: "Name" }, region: { type: "string", title: "Region" } } } }}
      value={{}}
      onChange={() => undefined}
      presentation={{ navigation: "sections", sections: [{ id: "identity", title: "Identity", columns: 2, columnWidths: [35, 65], fields: ["/name", "/region"] }] }}
      errors={{ name: "Name is already used" }}
    /></PortalI18nProvider>);
    expect(markup).toContain("Identity");
    expect(markup).toContain("Name");
    expect(markup).toContain("Region");
    expect(markup).toContain("minmax(0, 35fr) minmax(0, 65fr)");
    expect(markup).toContain("Name is already used");
    expect(markup.match(/Name is already used/g)).toHaveLength(1);
  });

  it("defaults labels to inline on leaf fields without collapsing the root object grid", () => {
    const Form = muiPortalUIComponents.FormRenderer;
    const markup = renderToStaticMarkup(<PortalI18nProvider policy={{ defaultLocale: "en-US", supportedLocales: ["en-US"] }} catalogs={{}} candidates={["en-US"]}><Form
      schema={{ id: "filter", schema: { $schema: "http://json-schema.org/draft-07/schema#", type: "object", properties: { profile: { type: "string", title: "Profile" } } } }}
      value={{}}
      onChange={() => undefined}
    /></PortalI18nProvider>);
    expect(markup).toContain("112px minmax(0,1fr)");
    expect(markup).toContain('aria-label="Profile"');
  });

  it("keeps compact filter labels persistent, single-line and inside the shared width", () => {
    const Form = muiPortalUIComponents.FormRenderer;
    const markup = renderToStaticMarkup(<PortalI18nProvider policy={{ defaultLocale: "en-US", supportedLocales: ["en-US"] }} catalogs={{}} candidates={["en-US"]}><Form
      schema={{ id: "filter", schema: { $schema: "http://json-schema.org/draft-07/schema#", type: "object", title: "Filter root", properties: { status: { type: "string", title: "Status", enum: ["ready", "offline"] } } }, uiSchema: { status: { "ui:placeholder": "", "ui:options": { allowClear: true } } } }}
      value={{ status: "ready" }}
      onChange={() => undefined}
      presentation={{ layout: "compact", labelPlacement: "inside-inline" }}
    /></PortalI18nProvider>);
    expect(markup).toContain("vp-mui-inside-inline-field");
    expect(markup).toContain('aria-label="Status"');
    expect(markup).toContain('aria-label="Clear selection"');
    expect(markup).toContain("white-space:nowrap");
    expect(markup).not.toContain("Filter root");
    expect(markup).toContain("margin-bottom:0");
  });
});
