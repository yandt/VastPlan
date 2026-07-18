import { describe, expect, it } from "vitest";
import adapter from "./index";

describe("standard shell layout", () => {
  it("exports only the visual layout adapter contract", () => {
    expect(adapter.id).toBe("ui.shell-layout");
    expect(adapter.uiContract).toBe("1.0.0");
    expect(adapter.Shell).toBeTypeOf("function");
    expect(adapter).not.toHaveProperty("compose");
  });
});
