import { describe, expect, it } from "vitest";
import shell from "./index";

describe("Shell Catalog", () => {
  it("owns only the account anchor while every business menu comes from the Contribution Index", () => {
    expect(shell.id).toBe("ui.structure.shell");
    expect(shell.compose).toBeTypeOf("function");
    expect(shell).not.toHaveProperty("Shell");
    expect(shell.templates).toEqual([]);
    expect(shell.localization?.messages["en-US"]).toMatchObject({
      "template.standard": "Standard sidebar",
      "navigation.account": "Account",
    });
    expect(shell.localization?.messages["en-US"]).not.toHaveProperty("navigation.settings");
    expect(shell.localization?.messages["en-US"]).not.toHaveProperty("navigation.primary");
  });
});
