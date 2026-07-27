import { describe, expect, it } from "vitest";
import { workbenchComponentRegionStyle, workbenchPageFlowStyle } from "./WorkbenchRhythm.js";

describe("Workbench rhythm", () => {
  it("keeps the page root flush while owning the section gap", () => {
    expect(workbenchPageFlowStyle("compact")).toMatchObject({ margin: 0, padding: 0, gap: 8 });
    expect(workbenchPageFlowStyle("standard")).toMatchObject({ margin: 0, padding: 0, gap: 16 });
  });

  it("separates component inset from outer page spacing", () => {
    expect(workbenchComponentRegionStyle("flush")).toMatchObject({ margin: 0, padding: 0 });
    expect(workbenchComponentRegionStyle("compact")).toMatchObject({ margin: 0, padding: 8 });
  });
});
