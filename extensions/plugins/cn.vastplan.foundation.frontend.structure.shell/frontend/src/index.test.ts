import { describe, expect, it } from "vitest";
import shell from "./index";

describe("Shell Catalog", () => {
  it("owns semantics while exact visual libraries come from the Contribution Index", () => {
    expect(shell.id).toBe("ui.structure.shell");
    expect(shell.compose).toBeTypeOf("function");
    expect(shell).not.toHaveProperty("Shell");
    expect(shell.templates).toEqual([]);
    expect(shell.localization?.messages["en-US"]).toMatchObject({
      "template.standard": "Standard sidebar",
      "navigation.primary": "Primary",
      "navigation.settings": "System settings",
    });
  });
});
