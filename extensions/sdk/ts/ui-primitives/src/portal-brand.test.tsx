import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { PortalBrand } from "./portal-brand.js";

describe("PortalBrand", () => {
  it("uses the short identity inline and the complete Portal name for compact expansion", () => {
    const inline = renderToStaticMarkup(<PortalBrand name="Operations Portal" shortName="OPS" className="brand" markClassName="mark" logoClassName="logo" />);
    const expanded = renderToStaticMarkup(<PortalBrand name="Operations Portal" shortName="OPS" className="brand" markClassName="mark" logoClassName="logo" fullName focusable />);
    expect(inline).toContain("<strong>OPS</strong>");
    expect(expanded).toContain('tabindex="0"');
    expect(expanded).toContain("<strong>Operations Portal</strong>");
  });
});
