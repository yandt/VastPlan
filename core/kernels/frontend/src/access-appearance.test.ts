import { describe, expect, it } from "vitest";
import { accessAppearance } from "./access-appearance";

describe("Access visual adapter facade", () => {
  it("keeps one semantic layout while projecting Arco and MUI tokens", () => {
    const arco = accessAppearance("access-arco"), mui = accessAppearance("access-mui");
    expect(arco.primary.background).toBe("#165dff");
    expect(mui.primary.background).toBe("#1976d2");
    expect(arco.card.width).toBe(mui.card.width);
    expect(Number(arco.primary.minHeight)).toBeGreaterThanOrEqual(40);
    expect(Number(mui.primary.minHeight)).toBeGreaterThanOrEqual(40);
		expect(arco.card.minWidth).toBe(0);
		expect(arco.footer.flexWrap).toBe("wrap");
  });
  it("fails closed to the foundation facade for unknown templates", () => { expect(accessAppearance("third-party-injected").primary.background).toBe("#165dff"); });
});
