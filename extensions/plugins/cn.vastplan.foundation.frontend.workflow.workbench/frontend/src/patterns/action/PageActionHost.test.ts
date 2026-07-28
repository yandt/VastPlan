import { describe, expect, it } from "vitest";
import type { PageActionSpec } from "@vastplan/ui-contract";
import { pageActionLayout } from "./PageActionHost.js";

const action = (id: string, overflow?: PageActionSpec["overflow"], order?: number): PageActionSpec => ({ id, label: id, icon: "info", ...(overflow === undefined ? {} : { overflow }), ...(order === undefined ? {} : { order }) });

describe("pageActionLayout", () => {
  it("keeps forced direct commands and bounds automatic commands", () => {
    const layout = pageActionLayout([action("e", "always"), action("d"), action("c"), action("b"), action("a", "never"), action("f")]);
    expect(layout.direct.map((item) => item.id)).toEqual(["a", "b", "c", "d"]);
    expect(layout.overflow.map((item) => item.id)).toEqual(["e", "f"]);
  });

  it("orders commands deterministically", () => {
    expect(pageActionLayout([action("later", undefined, 20), action("first", undefined, 10)]).direct.map((item) => item.id)).toEqual(["first", "later"]);
  });
});
