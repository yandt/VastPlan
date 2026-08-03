import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import type { RJSFSchema } from "@rjsf/utils";
import { cspJSONSchemaValidator } from "@vastplan/rjsf-csp-validator";
import { PortalI18nProvider, translate, type PortalUI } from "@vastplan/ui-primitives";
import { antdIconForTheme, antdPortalUIComponents, antdRenderer } from "./index";
import { localizeValidationErrors } from "./form-renderer";
import { namespace } from "./theme";

describe("Ant Design portal UI renderer", () => {
  it("implements the complete framework-neutral component surface", () => {
    const required: Array<keyof Omit<PortalUI, "notify" | "confirm" | "theme">> = [
      "PortalShell", "Page", "Panel", "BodySections", "Stack", "Grid", "GridItem", "Divider", "Button", "IconButton", "Select", "Menu", "Breadcrumb", "Tabs", "CommandPalette", "Popover", "Dialog", "Drawer", "FormRenderer", "FilterBar", "Table", "DataCard", "SplitView", "RecordNavigationList", "RecordTree", "Pagination", "Descriptions", "Status", "Icon", "EmptyState", "ErrorState", "Skeleton", "Busy",
    ];
    expect(antdRenderer).toMatchObject({ id: "antd", framework: "antd" });
    expect(required.every((name) => typeof antdPortalUIComponents[name] === "function")).toBe(true);
  });

  it("offers canonical and native Ant Design icon themes", () => {
    const NativeIcon = antdIconForTheme("renderer-native");
    const markup = renderToStaticMarkup(<><NativeIcon name="publish" label="Publish" /><NativeIcon name="remove" label="Delete" /><NativeIcon name="visibilityOff" label="Hidden" /><NativeIcon name="drag" label="Reorder" /></>);
    expect(markup).toContain('data-vastplan-icon="publish"');
    expect(markup).toContain('data-vastplan-icon="remove"');
    expect(markup).toContain("anticon-delete");
    expect(markup).toContain('data-vastplan-icon="visibilityOff"');
    expect(markup).toContain('data-vastplan-icon="drag"');
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

  it("renders governed body sections with separators only between regions", () => {
    const BodySections = antdPortalUIComponents.BodySections;
    const markup = renderToStaticMarkup(<BodySections sections={[
      { id: "theme", title: "Theme", description: "Choose colors", content: <span>Theme controls</span> },
      { id: "preferences", title: "Preferences", content: <span>Preference controls</span> },
    ]} />);
    expect(markup).toContain("data-vastplan-body-sections");
    expect(markup).toContain('data-body-section="theme"');
    expect(markup).toContain('data-body-section="preferences"');
    expect(markup.match(/border-bottom/g)).toHaveLength(1);
  });

  it("does not turn a one-row table into a vertical scroll container", () => {
    const Table = antdPortalUIComponents.Table;
    const markup = renderToStaticMarkup(<Table columns={[{ key: "name", title: "Name" }]} rows={[{ id: "one", name: "Only row" }]} />);
    expect(markup).toContain('data-table-scroll="horizontal"');
    expect(markup).toContain("overflow-x:auto;overflow-y:hidden");
  });

  it("maps governed table virtualization to the native Ant virtual table", () => {
    const Table = antdPortalUIComponents.Table;
    const rows = Array.from({ length: 100 }, (_, id) => ({ id, name: `Row ${id}` }));
    const serverLayoutWarning = vi.spyOn(console, "error").mockImplementation(() => undefined);
    try {
      const markup = renderToStaticMarkup(<Table columns={[{ key: "name", title: "Name", width: 200 }]} rows={rows} virtualization={{ enabled: true, viewportHeight: 480, rowHeight: 40, overscan: 4 }} />);
      expect(markup).toContain("ant-table-virtual");
    } finally {
      serverLayoutWarning.mockRestore();
    }
  });

  it("maps responsive grid columns to Ant breakpoint spans", () => {
    const Grid = antdPortalUIComponents.Grid;
    const GridItem = antdPortalUIComponents.GridItem;
    const markup = renderToStaticMarkup(<Grid columns={{ xs: 1, md: 2, xl: 3 }}><GridItem>筛选 A</GridItem><GridItem>筛选 B</GridItem><GridItem>筛选 C</GridItem></Grid>);
    expect(markup).toContain("ant-row");
    expect(markup).toContain("ant-col-md-12");
    expect(markup).toContain("ant-col-xl-8");
  });

  it("preserves responsive description columns and item spans", () => {
    const Descriptions = antdPortalUIComponents.Descriptions;
    const markup = renderToStaticMarkup(<Descriptions
      columns={{ xs: 1, md: 3 }}
      items={[
        { id: "identity", label: "Identity", value: "Portal A", span: { xs: 1, md: 2 } },
        { id: "status", label: "Status", value: "Ready" },
      ]}
    />);
    expect(markup).toContain("Identity");
    expect(markup).toContain("Portal A");
    expect(markup).toContain("Status");
  });

  it("keeps links and accessible record navigation semantics", () => {
    const Menu = antdPortalUIComponents.Menu;
    const List = antdPortalUIComponents.RecordNavigationList;
    const markup = renderToStaticMarkup(<><Menu items={[{ id: "settings", label: "设置", href: "/settings" }]} /><List ariaLabel="Services" items={[{ id: "one", title: "One" }]} selectedID="one" onSelect={() => undefined} /></>);
    expect(markup).toContain('href="/settings"');
    expect(markup).toContain('role="listbox"');
    expect(markup).toContain('aria-selected="true"');
  });

  it("renders a controlled inline navigation tree for persistent Shell panels", () => {
    const Menu = antdPortalUIComponents.Menu;
    const markup = renderToStaticMarkup(<Menu appearance="shell-navigation" presentation="inline" expandedIDs={["group:settings"]} items={[{ id: "group:settings", label: "设置", children: [{ id: "profile", label: "个人信息" }] }]} />);
    expect(markup).toContain("ant-menu-inline");
    expect(markup).toContain("vp-antd-shell-navigation-menu");
    expect(markup).toContain("height:44px");
    expect(markup).toMatch(/ant-menu-submenu[^>]*style="margin:0"/);
    expect(markup).toContain("个人信息");
  });

  it("removes the navigation divider from compact action menus", () => {
    const Menu = antdPortalUIComponents.Menu;
    const markup = renderToStaticMarkup(<Menu size="xs" variant="action" items={[{ id: "submit", label: "Submit" }]} />);
    expect(markup).toContain("border-inline-end:0");
    expect(markup).toContain("width:max-content");
    expect(markup).toContain("min-width:112px");
    expect(markup).toContain("max-width:280px");
    expect(markup).toContain("padding:4px");
    expect(markup).toContain("display:flex");
    expect(markup).toContain("width:100%");
    expect(markup).toContain("gap:6px");
    expect(markup).toContain("padding-inline:12px 6px");
    expect(markup).toContain("vp-antd-action-menu");
    expect(markup).toContain("margin-inline-start: 0 !important");
    expect(markup).toContain("height:28px");
  });

  it("maps the shared four-step component size scale", () => {
    const IconButton = antdPortalUIComponents.IconButton;
    const markup = renderToStaticMarkup(<><IconButton size="xs" icon="edit" label="Extra small" /><IconButton size="sm" icon="edit" label="Small" /><IconButton size="md" icon="edit" label="Medium" /><IconButton size="lg" icon="edit" label="Large" /></>);
    expect(markup).toContain("width:18px;height:18px");
    expect(markup).toContain("width:24px;height:24px");
    expect(markup).toContain("width:32px;height:32px");
    expect(markup).toContain("width:44px;height:44px");
  });

  it("renders JSON Schema through the Ant Design RJSF theme", () => {
    const Form = antdPortalUIComponents.FormRenderer;
    const markup = renderToStaticMarkup(<PortalI18nProvider policy={{ defaultLocale: "en-US", supportedLocales: ["en-US"] }} catalogs={{}} candidates={["en-US"]}><Form
      schema={{ id: "node", schema: { $schema: "http://json-schema.org/draft-07/schema#", type: "object", properties: { name: { type: "string", title: "Name" }, credential: { type: "string", title: "Credential", format: "vastplan-credential-ref", writeOnly: true } }, required: ["name"] }, uiSchema: { credential: { "ui:widget": "secretRef" } } }}
      value={{}}
      onChange={() => undefined}
      presentation={{ navigation: "sections", sections: [{ id: "identity", title: "Identity", columns: 2, columnWidths: [35, 65], fields: ["/name", "/credential"] }] }}
      errors={{ name: "Name is already used" }}
    /></PortalI18nProvider>);
    expect(markup).toContain("Identity");
    expect(markup).toContain("Name");
    expect(markup).toContain("credential://");
    expect(markup).toContain("minmax(0, 35fr) minmax(0, 65fr)");
    expect(markup).toContain("Name is already used");
    expect(markup).toContain("flex:0 0 112px");
    expect(markup).toContain('data-form-control-alignment="end"');
    expect(markup).toContain("vp-antd-form-controls-end");
    expect(markup.match(/Name is already used/g)).toHaveLength(1);
    expect(markup).not.toContain("Submit");
  });

  it("localizes JSON Schema validation errors without exposing validator internals", () => {
    const catalogs = { [namespace]: antdRenderer.localization! };
    const schema: RJSFSchema = { type: "object", properties: { reason: { type: "string", minLength: 4 } } };
    const errors = cspJSONSchemaValidator.validateFormData({ reason: "三个" }, schema).errors;
    const localize = (locale: string) => localizeValidationErrors(errors, (value) => translate(value, locale, catalogs));

    expect(errors[0]?.message).toContain("#/reason");
    expect(localize("zh-CN")[0]?.message).toBe("请至少输入 4 个字符");
    expect(localize("en-US")[0]?.message).toBe("Enter at least 4 characters");
    expect(localize("zh-CN")[0]?.stack).not.toContain("#/reason");
  });

  it("allows a form to opt out of the default end control alignment", () => {
    const Form = antdPortalUIComponents.FormRenderer;
    const markup = renderToStaticMarkup(<PortalI18nProvider policy={{ defaultLocale: "en-US", supportedLocales: ["en-US"] }} catalogs={{}} candidates={["en-US"]}><Form
      schema={{ id: "start-aligned", schema: { $schema: "http://json-schema.org/draft-07/schema#", type: "object", properties: { name: { type: "string", title: "Name" } } } }}
      value={{}}
      onChange={() => undefined}
      presentation={{ controlAlignment: "start" }}
    /></PortalI18nProvider>);
    expect(markup).toContain('data-form-control-alignment="start"');
    expect(markup).toContain("vp-antd-form-controls-start");
  });

  it("renders persistent collection labels inside the shared field width", () => {
    const Form = antdPortalUIComponents.FormRenderer;
    const markup = renderToStaticMarkup(<PortalI18nProvider policy={{ defaultLocale: "en-US", supportedLocales: ["en-US"] }} catalogs={{}} candidates={["en-US"]}><Form
      schema={{ id: "filter", schema: { $schema: "http://json-schema.org/draft-07/schema#", type: "object", title: "Filter root", properties: { status: { type: "string", title: "Status", enum: ["ready", "offline"] } } }, uiSchema: { status: { "ui:placeholder": "", "ui:options": { allowClear: true } } } }}
      value={{ status: "ready" }}
      onChange={() => undefined}
      presentation={{ layout: "compact", labelPlacement: "inside-inline" }}
    /></PortalI18nProvider>);
    expect(markup).toContain("vp-antd-inside-inline-field");
    expect(markup).toContain("ant-select-outlined");
    expect(markup).toContain(".vp-antd-form-controls-start&gt;.rjsf,.vp-antd-form-controls-end&gt;.rjsf{width:100%;min-width:0}");
    expect(markup).toContain(".vp-antd-form-object{width:100%;min-width:0}");
    expect(markup).toContain('class="vp-antd-form-object"');
    expect(markup).toContain(".vp-inside-inline-control&gt;.ant-select{width:100%;border:0!important");
    expect(markup).toContain('aria-label="Status"');
    expect(markup).toContain("ant-select-clear");
    expect(markup).toContain("white-space:nowrap");
    expect(markup).toContain("max-width:clamp(48px,18%,112px)");
    expect(markup).toContain("padding:0 6px");
    expect(markup).not.toContain("Filter root");
  });

  it("maps the shared shell and interaction baselines", () => {
    expect(antdPortalUIComponents.theme.tokens).toMatchObject({
      shell: { barHeight: 64, railWidth: 64, navigationWidth: 240, navigationCompactWidth: 220 },
      overlay: { navigationMinWidth: 480, navigationMaxWidth: 840 },
      focus: { width: 2 }, touch: { minimum: 44 }, motion: { fast: 120, normal: 180 },
    });
  });
});
