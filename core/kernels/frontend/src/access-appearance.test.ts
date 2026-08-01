import { describe, expect, it } from "vitest";
import { accessAppearance } from "./access-appearance";

describe("Access visual adapter facade", () => {
  it("projects the public access facade from Ant Design tokens", () => {
    const appearance = accessAppearance("access");
    expect(appearance.primary.background).toBe("#1677ff");
    expect(Number(appearance.primary.minHeight)).toBeGreaterThanOrEqual(40);
		expect(appearance.card.minWidth).toBe(0);
		expect(appearance.footer.flexWrap).toBe("wrap");
		expect(appearance.localePicker.position).toBe("absolute");
		expect(appearance.localeMenu.minWidth).toBe(128);
  });
  it("fails closed to the Ant facade for unknown templates", () => { expect(accessAppearance("third-party-injected").primary.background).toBe("#1677ff"); });
});
