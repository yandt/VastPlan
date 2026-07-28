import { describe, expect, it } from "vitest";
import { semanticIconNames } from "@vastplan/ui-contract";
import { iconCatalogEntries, iconCatalogNames, loadIconGlyph } from "./index.js";
import { semanticIconGlyph } from "./semantic.js";

describe("Ant Design icon catalog", () => {
  it("keeps the complete locked catalog with stable unique names", () => {
    expect(iconCatalogEntries).toHaveLength(846);
    expect(iconCatalogNames).toHaveLength(846);
    expect(new Set(iconCatalogNames).size).toBe(846);
    expect(iconCatalogEntries.filter((entry) => entry.theme === "outlined")).toHaveLength(447);
    expect(iconCatalogEntries.filter((entry) => entry.theme === "filled")).toHaveLength(249);
    expect(iconCatalogEntries.filter((entry) => entry.theme === "twotone")).toHaveLength(150);
  });

  it("provides every stable semantic icon synchronously", () => {
    for (const name of semanticIconNames) {
      const glyph = semanticIconGlyph(name);
      expect(glyph.viewBox).toBe("64 64 896 896");
      expect(glyph.nodes.length).toBeGreaterThan(0);
    }
  });

  it("loads outlined, two-tone and filtered-source icons through delayed shards", async () => {
    const [outlined, twoTone, twitch] = await Promise.all([
      loadIconGlyph("plus-outlined"), loadIconGlyph("warning-two-tone"), loadIconGlyph("twitch-filled"),
    ]);
    expect(outlined.nodes.length).toBeGreaterThan(0);
    expect(flatten(twoTone.nodes).some((node) => node.tag === "path" && node.tone === "secondary")).toBe(true);
    expect(twitch.nodes.some((node) => node.tag === "g" && node.transform === "translate(9 9)")).toBe(true);
  });

  it("normalizes every locked upstream definition through the SVG whitelist", async () => {
    const glyphs = await Promise.all(iconCatalogNames.map((name) => loadIconGlyph(name)));
    expect(glyphs).toHaveLength(846);
    expect(glyphs.every((glyph) => glyph.nodes.length > 0)).toBe(true);
  });

  it("rejects unknown names even when JavaScript bypasses the type", async () => {
    await expect(loadIconGlyph("not-in-catalog" as never)).rejects.toThrow("未知图标目录名称");
  });
});

function flatten(nodes: readonly import("./types.js").IconGlyphNode[]): import("./types.js").IconGlyphNode[] {
  return nodes.flatMap((node) => node.tag === "g" ? [node, ...flatten(node.children)] : [node]);
}
