import { describe, expect, it, vi } from "vitest";
import { defaultPortalAppearance } from "./appearance.js";
import { applyAppearanceChange } from "./portal-account-pages.js";

describe("applyAppearanceChange", () => {
  it("immediately sends a readable appearance change to the trusted host", () => {
    const onChange = vi.fn();
    const appearance = { ...defaultPortalAppearance, mode: "dark" as const };

    expect(applyAppearanceChange(appearance, onChange)).toBe(true);
    expect(onChange).toHaveBeenCalledExactlyOnceWith(appearance);
  });

  it("keeps an unreadable color draft out of the trusted host", () => {
    const onChange = vi.fn();
    const appearance = {
      ...defaultPortalAppearance,
      light: { templateID: "light", colors: { canvas: "#ffffff", text: "#ffffff" } },
    };

    expect(applyAppearanceChange(appearance, onChange)).toBe(false);
    expect(onChange).not.toHaveBeenCalled();
  });
});
