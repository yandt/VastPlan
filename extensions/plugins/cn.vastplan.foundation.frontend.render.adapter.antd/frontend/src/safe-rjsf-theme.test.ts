import type { ArrayFieldTemplateProps } from "@rjsf/utils";
import { describe, expect, it, vi } from "vitest";
import { moveScalarArrayItem } from "./safe-rjsf-theme";

describe("compact scalar array sorting", () => {
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

    expect(moveScalarArrayItem([item], 0, 1)).toBe(1);
    expect(moveDown).toHaveBeenCalledOnce();
  });
});
