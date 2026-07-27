import { describe, expect, it } from "vitest";
import { portalPageRhythm } from "./rhythm.js";

describe("Portal page rhythm", () => {
  it("keeps header separation independent from Workbench density and component inset", () => {
    expect(portalPageRhythm.contentStart).toBe(16);
    expect(portalPageRhythm.sectionGap).toEqual({ compact: 8, standard: 16, comfortable: 24 });
    expect(portalPageRhythm.componentInset).toEqual({ flush: 0, compact: 8 });
  });
});
