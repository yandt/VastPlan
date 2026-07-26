import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { PortalI18nProvider, type PortalUI } from "@vastplan/ui-primitives";
import { antdIconForTheme, antdPortalUIComponents, antdRenderer } from "./index";

describe("Ant Design portal UI renderer", () => {
  it("implements the complete framework-neutral component surface", () => {
    const required: Array<keyof Omit<PortalUI, "notify" | "confirm" | "theme">> = [
      "PortalShell", "Page", "Panel", "Stack", "Grid", "GridItem", "Divider", "Button", "IconButton", "Select", "Menu", "Breadcrumb", "Tabs", "CommandPalette", "Popover", "Dialog", "Drawer", "FormRenderer", "FilterBar", "Table", "DataCard", "SplitView", "RecordNavigationList", "RecordTree", "Pagination", "Descriptions", "Status", "Icon", "EmptyState", "ErrorState", "Skeleton", "Busy",
    ];
    expect(antdRenderer).toMatchObject({ id: "antd", framework: "antd" });
    expect(required.every((name) => typeof antdPortalUIComponents[name] === "function")).toBe(true);
  });

  it("offers canonical and native Ant Design icon themes", () => {
    const NativeIcon = antdIconForTheme("renderer-native");
    const markup = renderToStaticMarkup(<NativeIcon name="publish" label="Publish" />);
    expect(markup).toContain('data-vastplan-icon="publish"');
    expect(markup).toContain('data-vastplan-icon-source="renderer-native"');
    expect(antdRenderer.iconThemes.map((theme) => theme.id)).toEqual(["canonical", "renderer-native"]);
    expect(antdIconForTheme("missing")).toBe(antdPortalUIComponents.Icon);
  });

  it("renders native Ant Design semantic components", () => {
    const Page = antdPortalUIComponents.Page;
    const Button = antdPortalUIComponents.Button;
    const DataCard = antdPortalUIComponents.DataCard;
    const markup = renderToStaticMarkup(<Page title="Portal"><Button kind="primary">保存</Button><DataCard title="Node A" selectable selected selectionLabel="Select Node A">Ready</DataCard></Page>);
    expect(markup).toContain("Portal");
    expect(markup).toContain("ant-btn-primary");
    expect(markup).toContain("ant-card");
    expect(markup).toContain("Select Node A");
  });

  it("keeps links and accessible record navigation semantics", () => {
    const Menu = antdPortalUIComponents.Menu;
    const List = antdPortalUIComponents.RecordNavigationList;
    const markup = renderToStaticMarkup(<><Menu items={[{ id: "settings", label: "设置", href: "/settings" }]} /><List ariaLabel="Services" items={[{ id: "one", title: "One" }]} selectedID="one" onSelect={() => undefined} /></>);
    expect(markup).toContain('href="/settings"');
    expect(markup).toContain('role="listbox"');
    expect(markup).toContain('aria-selected="true"');
  });

  it("renders JSON Schema through the Ant Design RJSF theme", () => {
    const Form = antdPortalUIComponents.FormRenderer;
    const markup = renderToStaticMarkup(<PortalI18nProvider policy={{ defaultLocale: "en-US", supportedLocales: ["en-US"] }} catalogs={{}} candidates={["en-US"]}><Form
      schema={{ id: "node", schema: { $schema: "http://json-schema.org/draft-07/schema#", type: "object", properties: { name: { type: "string", title: "Name" }, credential: { type: "string", title: "Credential", format: "vastplan-credential-ref", writeOnly: true } }, required: ["name"] }, uiSchema: { credential: { "ui:widget": "secretRef" } } }}
      value={{}}
      onChange={() => undefined}
      presentation={{ navigation: "sections", sections: [{ id: "identity", title: "Identity", columns: 2, fields: ["/name", "/credential"] }] }}
    /></PortalI18nProvider>);
    expect(markup).toContain("Identity");
    expect(markup).toContain("Name");
    expect(markup).toContain("credential://");
    expect(markup).not.toContain("Submit");
  });

  it("maps the shared shell and interaction baselines", () => {
    expect(antdPortalUIComponents.theme.tokens).toMatchObject({
      shell: { barHeight: 64, railWidth: 64, navigationWidth: 240, navigationCompactWidth: 220 },
      overlay: { navigationMinWidth: 480, navigationMaxWidth: 840 },
      focus: { width: 2 }, touch: { minimum: 44 }, motion: { fast: 120, normal: 180 },
    });
  });
});
