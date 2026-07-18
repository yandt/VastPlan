import { describe, expect, it } from "vitest";
import { buildApplicationComposition, portalCompositionSchema } from "./index";

describe("Portal application composition", () => {
  it("never exposes or submits platform-managed fields", () => {
    const properties = portalCompositionSchema.schema.properties as Record<string, unknown>;
    expect(properties.designSystem).toBeUndefined();

    const composition = buildApplicationComposition({
      name: "operations",
      route: "/operations",
      designSystem: "com.vastplan.foundation.frontend.design-system.arco",
      plugins: [{ id: "com.example.application.dashboard", version: "1.2.3" }],
    });

    expect(composition).toEqual({
      version: 1,
      revision: 1,
      id: "operations",
      target: { kernel: "frontend" },
      route: "/operations",
      plugins: [{ id: "com.example.application.dashboard", version: "1.2.3" }],
      config: {},
    });
    expect(composition).not.toHaveProperty("designSystem");
  });
});
