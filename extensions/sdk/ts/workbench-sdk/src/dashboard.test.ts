import { describe, expect, it } from "vitest";
import { defineDashboardGrid } from "./dashboard.js";

const valid = {
  id: "portal-home", cards: ["summary", "activity"],
  layouts: { lg: [{ cardID: "summary", x: 0, y: 0, width: 4, height: 2 }, { cardID: "activity", x: 4, y: 0, width: 8, height: 2 }] },
} as const;

describe("defineDashboardGrid", () => {
  it("freezes a governed responsive card layout", () => {
    const result = defineDashboardGrid(valid);
    expect(Object.isFrozen(result)).toBe(true);
    expect(Object.isFrozen(result.layouts.lg)).toBe(true);
  });

  it("rejects unknown cards and out-of-grid placement", () => {
    expect(() => defineDashboardGrid({ ...valid, layouts: { lg: [{ cardID: "unknown", x: 0, y: 0, width: 4, height: 2 }] } })).toThrow("未知");
    expect(() => defineDashboardGrid({ ...valid, layouts: { lg: [{ cardID: "summary", x: 10, y: 0, width: 4, height: 2 }, { cardID: "activity", x: 0, y: 0, width: 4, height: 2 }] } })).toThrow("超出列边界");
  });

  it("rejects incomplete, overlapping and contradictory layouts", () => {
    expect(() => defineDashboardGrid({ ...valid, layouts: { lg: [valid.layouts.lg[0]] } })).toThrow("全部卡片");
    expect(() => defineDashboardGrid({ ...valid, layouts: { lg: [{ ...valid.layouts.lg[0], width: 8 }, { ...valid.layouts.lg[1], x: 4 }] } })).toThrow("重叠");
    expect(() => defineDashboardGrid({ ...valid, layouts: { lg: [{ ...valid.layouts.lg[0], minWidth: 5 }, valid.layouts.lg[1]] } })).toThrow("初始尺寸");
  });
});
