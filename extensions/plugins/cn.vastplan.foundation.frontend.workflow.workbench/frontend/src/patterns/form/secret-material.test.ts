import { describe, expect, it } from "vitest";
import { containsSecretMaterial, discardSecretMaterial, secretMaterialPointers } from "./secret-material.js";

describe("secret material lifecycle", () => {
  const pointers = secretMaterialPointers({ fields: [{ pointer: "/credential/value", widget: "secretMaterial" }] });

  it("detects and discards nested one-time material without mutating the source", () => {
    const source = { name: "database", credential: { value: "plain-text", keep: true } };
    expect(containsSecretMaterial(source, pointers)).toBe(true);
    expect(discardSecretMaterial(source, pointers)).toEqual({ name: "database", credential: { keep: true } });
    expect(source.credential.value).toBe("plain-text");
  });

  it("does not treat an empty input as retained material", () => {
    expect(containsSecretMaterial({ credential: { value: "" } }, pointers)).toBe(false);
  });
});
