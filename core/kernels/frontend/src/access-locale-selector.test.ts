import { describe, expect, it } from "vitest";
import { accessLocaleOption } from "./access-locale-selector";

describe("Access locale selector", () => {
  it("uses compact language glyphs with human-readable menu labels", () => {
    expect(accessLocaleOption("zh-CN")).toEqual({ glyph: "中", label: "中文" });
    expect(accessLocaleOption("en-US")).toEqual({ glyph: "EN", label: "English" });
  });
});
