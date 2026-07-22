import { describe, expect, it } from "vitest";
import { workbench } from "./index.js";

describe("UI Workbench", () => {
  it("contributes the unique collection workflow extension point", () => {
    expect(workbench).toMatchObject({ id: "ui.workflow.workbench", uiContract: "4.0.0" });
    expect(typeof workbench.CollectionPage).toBe("function");
    expect(typeof workbench.CollectionPageActions).toBe("function");
    expect(typeof workbench.FormPage).toBe("function");
  });
});
