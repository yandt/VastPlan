import { describe, expect, it } from "vitest";
import { validateFrontendRuntimeEngine } from "@vastplan/frontend-engine-contract";
import { runtimeEngine } from "./index.js";

describe("React Runtime Engine", () => {
  it("exports the governed React engine identity", () => {
    expect(validateFrontendRuntimeEngine(runtimeEngine).family).toBe("react");
  });
});
