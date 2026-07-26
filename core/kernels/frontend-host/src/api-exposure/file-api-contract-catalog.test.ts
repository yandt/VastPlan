import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { apiContractDigest } from "./api-exposure-schema";
import { exampleContract, exampleContractCatalog } from "./api-exposure-test-fixture";
import { FileAPIContractCatalog } from "./file-api-contract-catalog";

describe("FileAPIContractCatalog", () => {
  it("resolves only the exact immutable contract reference", async () => {
    const root = await mkdtemp(join(tmpdir(), "vastplan-api-contracts-"));
    const path = join(root, "catalog.json"), contract = exampleContract();
    await writeFile(path, JSON.stringify(exampleContractCatalog(contract)), { mode: 0o600 });
    const catalog = await FileAPIContractCatalog.open(path);
    const reference = { contractId: contract.contractId, contractVersion: contract.contractVersion, contractDigest: apiContractDigest(contract) };
    expect((await catalog.resolveContract(reference))?.id).toBe("management-api");
    expect(await catalog.resolveContract({ ...reference, contractDigest: "b".repeat(64) })).toBeUndefined();
  });

  it("fails startup on an invalid contract catalog", async () => {
    const root = await mkdtemp(join(tmpdir(), "vastplan-api-contracts-"));
    const path = join(root, "catalog.json");
    await writeFile(path, "{}", { mode: 0o600 });
    await expect(FileAPIContractCatalog.open(path)).rejects.toThrow(/Schema/);
  });
});
