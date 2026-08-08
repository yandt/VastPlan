import { describe, expect, it, vi } from "vitest";
import plugin from "./index.js";

function context(contributions: readonly object[] = []) {
  return {
    portal: { id: "operations", revision: 1, tenantId: "tenant", route: "/", management: { services: [{ id: "workflow", logicalService: "platform.workflow", routingDomain: "platform", capabilities: [{ capability: "platform.workflow.orchestrator", read: [], write: [] }] }] } },
    extensions: { owns: () => true, contributes: () => false, list: () => contributions },
    i18n: { message: (_key: string, fallback: string) => ({ key: _key, fallback }) },
    addCollectionPage: vi.fn(),
  };
}

describe("workflow default UI Provider", () => {
  it("registers the governed default Workbench pages", () => {
    const value = context();
    plugin.register(value as never);
    expect(value.addCollectionPage).toHaveBeenCalledTimes(5);
  });

  it("defers to the single signed replacement", () => {
    const value = context([{ point: "cn.vastplan.platform.workflow.orchestrator.ui-provider", id: "cn.example.workflow.page", pluginId: "cn.example.workflow", contract: "^1.0.0", descriptor: { pageId: "replacement", groupId: "workflow-management" } }]);
    plugin.register(value as never);
    expect(value.addCollectionPage).not.toHaveBeenCalled();
  });
});
