import { describe, expect, it } from "vitest";
import { validateFrontendRuntimeEngine } from "./index.js";

describe("frontend runtime engine contract", () => {
  it("accepts the minimum governed engine", () => {
    expect(validateFrontendRuntimeEngine({ id: "ui.runtime.engine", family: "react", engineContract: "1.0.0", capabilities: ["csr", "generation"] }).family).toBe("react");
  });

  it("rejects engines without the lifecycle baseline", () => {
    expect(() => validateFrontendRuntimeEngine({ id: "ui.runtime.engine", family: "react", engineContract: "1.0.0", capabilities: ["csr"] })).toThrow();
  });
});
