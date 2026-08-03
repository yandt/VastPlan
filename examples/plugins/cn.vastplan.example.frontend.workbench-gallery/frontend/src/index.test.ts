import { describe, expect, it } from "vitest";
import { accountExtensionPage, masterDetailPage, recordDetailPage, treeDetailPage } from "./index";

describe("Workbench pattern gallery", () => {
  it("exposes all three governed patterns", () => {
    expect(recordDetailPage().pattern).toBe("record-detail");
    expect(masterDetailPage()).toMatchObject({ pattern: "master-detail", editor: { workflow: { surface: "page" } } });
    expect(treeDetailPage()).toMatchObject({ pattern: "tree-detail", tree: { defaultExpandedDepth: 2 } });
    expect(accountExtensionPage()).toMatchObject({ id: "example.account-extension", navigation: { parentMenuRef: { pluginID: "cn.vastplan.example.frontend.workbench-gallery", nodeID: "account-gallery" } } });
  });
});
