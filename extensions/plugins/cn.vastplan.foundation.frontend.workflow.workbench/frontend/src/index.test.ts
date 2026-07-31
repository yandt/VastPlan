import { describe, expect, it } from "vitest";
import { uiContractVersion } from "@vastplan/ui-contract";
import { workbench } from "./index.js";

describe("UI Workbench", () => {
  it("contributes the unique collection workflow extension point", () => {
    expect(workbench).toMatchObject({ id: "ui.workflow.workbench", uiContract: uiContractVersion });
    expect(typeof workbench.CollectionPage).toBe("function");
    expect(typeof workbench.WorkspacePage).toBe("function");
    expect(typeof workbench.PageActionHost).toBe("function");
    expect(typeof workbench.FormPage).toBe("function");
    expect(typeof workbench.RecordPage).toBe("function");
    expect(typeof workbench.loadDashboardGrid).toBe("function");
  });

  it("loads the dashboard grid from its deferred module boundary", async () => {
    await expect(workbench.loadDashboardGrid?.()).resolves.toBeTypeOf("function");
  });
});
