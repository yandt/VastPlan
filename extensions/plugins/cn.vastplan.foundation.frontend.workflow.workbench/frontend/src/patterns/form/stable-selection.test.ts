import { describe, expect, it } from "vitest";
import { stabilizeSelection } from "./stable-selection.js";

describe("stabilizeSelection", () => {
  it("keeps the previous container when selected records are unchanged", () => {
    const record = { id: "svc-auth" };
    const previous = [record] as const;
    expect(stabilizeSelection(previous, [record])).toBe(previous);
  });

  it("returns the new selection when order or record identity changes", () => {
    const first = { id: "svc-auth" };
    const second = { id: "svc-artifact" };
    const previous = [first, second] as const;
    const reordered = [second, first] as const;
    const replaced = [{ id: "svc-auth" }, second] as const;
    expect(stabilizeSelection(previous, reordered)).toBe(reordered);
    expect(stabilizeSelection(previous, replaced)).toBe(replaced);
  });
});
