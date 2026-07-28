import { describe, expect, it } from "vitest";
import shell from "./index";
import { shellLibraryPluginVersions } from "./template-versions.generated";

describe("Shell Catalog", () => {
  it("owns semantics while locking visual libraries as independent modules", () => {
    expect(shell.id).toBe("ui.structure.shell");
    expect(shell.compose).toBeTypeOf("function");
    expect(shell).not.toHaveProperty("Shell");
    expect(shell.templates).toMatchObject([
      { id: "standard", module: { id: "cn.vastplan.foundation.frontend.structure.layout.standard", version: shellLibraryPluginVersions.standard, channel: "stable" } },
      { id: "top-navigation", module: { id: "cn.vastplan.foundation.frontend.structure.layout.top-navigation", version: shellLibraryPluginVersions.topNavigation, channel: "stable" } },
    ]);
    expect(shell.localization?.messages["en-US"]).toMatchObject({
      "template.standard": "Standard sidebar",
      "navigation.primary": "Primary",
      "navigation.settings": "System settings",
    });
  });
});
