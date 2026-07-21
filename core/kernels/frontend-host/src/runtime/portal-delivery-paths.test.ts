import { describe, expect, it } from "vitest";
import { objectPath, snapshotPath } from "./portal-delivery-paths";

describe("Portal delivery paths", () => {
  it("matches the Go tenant/portal delivery key and immutable object layout", () => {
    expect(snapshotPath("/cache", "local", "operations", 1))
      .toBe("/cache/snapshots/48461ff2d4b9265bbf28dbd781590af4c59f1ae3ef0f7707b8f70b1372f137e0/1.json");
    expect(objectPath("/cache", "ab".repeat(32)))
      .toBe(`/cache/objects/ab/${"ab".repeat(32)}.blob`);
  });
});
