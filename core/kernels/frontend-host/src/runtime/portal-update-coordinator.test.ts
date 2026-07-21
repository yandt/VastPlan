import { describe, expect, it } from "vitest";
import { classifyPortalUpdate } from "./portal-update-coordinator";
import type { PortalSpec } from "./portal-runtime-contract";

describe("classifyPortalUpdate", () => {
  it("uses Host Epoch only when a host compatibility boundary changes", () => {
    const base = {
      revision: 1, id: "operations", tenantId: "tenant-a", route: "/operations",
      renderAdapter: { id: "adapter", version: "1.0.0", channel: "stable", uiContract: "^4.0.0", config: { defaultRenderer: "arco" } },
      shell: { uiContract: "^4.0.0" }, workbench: { uiContract: "^4.0.0" },
    } as unknown as PortalSpec;
    expect(classifyPortalUpdate(base, { ...base, revision: 2 })).toBe("generation");
    expect(classifyPortalUpdate(base, {
      ...base, revision: 2,
      renderAdapter: { ...(base.renderAdapter as object), config: { defaultRenderer: "mui" } },
    } as PortalSpec)).toBe("host-epoch");
  });
});
