import { mkdtemp, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { describe, expect, it } from "vitest";
import { FileAPIExposureCatalog } from "./file-api-exposure-catalog";
import { exampleCatalog } from "./api-exposure-test-fixture";

describe("FileAPIExposureCatalog", () => {
  it("resolves normalized hosts and keeps the last good generation on invalid replacement", async () => {
    const root = await mkdtemp(join(tmpdir(), "vastplan-api-catalog-"));
    const path = join(root, "catalog.json");
    await writeFile(path, JSON.stringify(exampleCatalog()), { mode: 0o600 });
    let now = 1_000;
    const catalog = await FileAPIExposureCatalog.open(path, () => now);
    expect((await catalog.resolve("127.0.0.1:8443", "aaaaaaaaaaaaaaaaaaaa", 1))?.exposure.id).toBe("exp_aaaaaaaaaaaaaaaaaaaa");

    await writeFile(path, "{invalid", { mode: 0o600 });
    now += 2_000;
    expect((await catalog.resolve("127.0.0.1", "aaaaaaaaaaaaaaaaaaaa", 1))?.exposure.id).toBe("exp_aaaaaaaaaaaaaaaaaaaa");
  });

  it("fails startup on an invalid catalog", async () => {
    const root = await mkdtemp(join(tmpdir(), "vastplan-api-catalog-"));
    const path = join(root, "catalog.json");
    await writeFile(path, "{}", { mode: 0o600 });
    await expect(FileAPIExposureCatalog.open(path)).rejects.toThrow(/Schema/);
  });
});
