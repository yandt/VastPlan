import { describe, expect, it } from "vitest";
import { formFieldErrorPath, localizeFormFieldErrors } from "./field-errors.js";

describe("form field errors", () => {
  it("normalizes JSON Pointer errors to renderer field paths", () => {
    expect(formFieldErrorPath("/database/host")).toBe("database.host");
    expect(formFieldErrorPath("/a~1b/name~0suffix")).toBe("a/b.name~suffix");
    expect(localizeFormFieldErrors({ "/name": "Required", "$form": "Invalid" }, String)).toEqual({ name: "Required", $form: "Invalid" });
  });
});
