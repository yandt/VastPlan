import { describe, expect, it } from "vitest";
import type { DashboardGridSpec } from "@vastplan/ui-contract";
import { fromReactGridLayouts, toReactGridLayouts } from "./layout-model.js";

describe("dashboard layout model", () => {
  it("round-trips the serializable Workbench layout without leaking react-grid-layout names", () => {
    const spec: DashboardGridSpec = { id: "home", cards: ["summary"], layouts: { lg: [{ cardID: "summary", x: 1, y: 2, width: 4, height: 3, minWidth: 2 }] } };
    const native = toReactGridLayouts(spec);
    expect(native.lg?.[0]).toMatchObject({ i: "summary", x: 1, y: 2, w: 4, h: 3, minW: 2 });
    expect(fromReactGridLayouts(native).lg?.[0]).toEqual(spec.layouts.lg?.[0]);
  });
});
