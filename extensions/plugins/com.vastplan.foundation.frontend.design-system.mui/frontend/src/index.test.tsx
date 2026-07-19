import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { PortalUI } from "@vastplan/portal-ui";
import { muiDesignSystem, muiPortalUIComponents } from "./index";

describe("MUI portal UI adapter", () => {
  it("implements the full framework-neutral component surface", () => {
    const required: Array<keyof Omit<PortalUI, "notify" | "confirm" | "theme">> = [
      "PortalShell", "Page", "Panel", "Stack", "Grid", "GridItem", "Divider", "Button", "Menu", "Breadcrumb", "Tabs", "CommandPalette", "Popover", "Dialog", "Drawer", "FormRenderer", "FilterBar", "Table", "Pagination", "Descriptions", "Status", "Icon", "EmptyState", "ErrorState", "Skeleton", "Busy",
    ];
    expect(muiDesignSystem).toMatchObject({ id: "ui.design-system", framework: "mui", uiContract: "2.0.0" });
    expect(required.every((name) => typeof muiPortalUIComponents[name] === "function")).toBe(true);
  });

  it("renders semantic UI without exposing MUI types to consumers", () => {
    const Page = muiPortalUIComponents.Page;
    const Button = muiPortalUIComponents.Button;
    const markup = renderToStaticMarkup(<Page title="Portal"><Button kind="primary">保存</Button></Page>);
    expect(markup).toContain("Portal");
    expect(markup).toContain("保存");
  });

  it("keeps navigation destinations as real links", () => {
    const Menu = muiPortalUIComponents.Menu;
    const markup = renderToStaticMarkup(<Menu items={[{ id: "settings", label: "设置", href: "/settings" }]} />);
    expect(markup).toContain('href="/settings"');
  });
});
