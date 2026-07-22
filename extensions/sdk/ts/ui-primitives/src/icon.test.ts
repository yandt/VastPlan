import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { semanticIconNames, VastPlanIcon } from "./icon.js";

describe("VastPlanIcon", () => {
  it("renders every semantic name as an owned SVG glyph", () => {
    for (const name of semanticIconNames) {
      const markup = renderToStaticMarkup(createElement(VastPlanIcon, { name }));
      expect(markup).toContain(`<svg data-vastplan-icon="${name}"`);
      expect(markup).toContain('aria-hidden="true"');
    }
  });

  it("exposes an accessible name when the icon carries meaning", () => {
    const markup = renderToStaticMarkup(createElement(VastPlanIcon, { name: "publish", label: "发布", size: "lg" }));
    expect(markup).toContain('role="img"');
    expect(markup).toContain('aria-label="发布"');
    expect(markup).toContain('width="24"');
  });
});
