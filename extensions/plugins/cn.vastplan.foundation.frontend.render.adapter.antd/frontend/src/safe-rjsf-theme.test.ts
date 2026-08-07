import type { ArrayFieldTemplateProps } from "@rjsf/utils";
import { describe, expect, it, vi } from "vitest";
import { arrayItemIndex, arrayItemSummary, moveArrayItem } from "./safe-rjsf-theme";

describe("compact array sorting", () => {
  it("uses the nested RJSF item button callbacks when moving an item", () => {
    const moveDown = vi.fn();
    const item = {
      props: {
        buttonsProps: {
          hasMoveDown: true,
          hasMoveUp: false,
          onMoveDownItem: moveDown,
        },
      },
    } as unknown as ArrayFieldTemplateProps["items"][number];

    expect(moveArrayItem([item], 0, 1)).toBe(1);
    expect(moveDown).toHaveBeenCalledOnce();
  });

  it("resolves drag source and target by stable RJSF item keys", () => {
    const items = ["domain-a", "domain-b"].map((itemKey) => ({ props: { itemKey } })) as unknown as ArrayFieldTemplateProps["items"];
    expect(arrayItemIndex(items, "domain-b")).toBe(1);
    expect(arrayItemIndex(items, "missing")).toBeUndefined();
  });

  it("builds a stable drag overlay summary for object items", () => {
    expect(arrayItemSummary({ id: "cn.example.dashboard", version: "1.2.3", enabled: true }, "Item 1")).toBe("cn.example.dashboard · 1.2.3");
    expect(arrayItemSummary({}, "Item 1")).toBe("Item 1");
  });
});
