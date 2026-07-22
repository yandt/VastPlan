import { describe, expect, it } from "vitest";
import { apiContractDigest, parseAPIExposureCatalog } from "./api-exposure-schema";
import type { APIContractContribution } from "./api-exposure-contract";
import { exampleCatalog, exampleContract, type MutableCatalog, type MutableContract } from "./api-exposure-test-fixture";

describe("API Exposure Contract", () => {
  it("normalizes contract declaration order and parses a self-contained catalog", () => {
    const contract = exampleContract();
    expect(apiContractDigest(contract)).toBe("f4a83624f391ff26825cddcffb48bfa970687749a3aa3dd2a4f8b38c00dfdc3f");
    const reordered = structuredClone(contract) as unknown as MutableContract;
    reordered.routes[0].requestSchema = { additionalProperties: false, type: "object" };
    expect(apiContractDigest(reordered as unknown as APIContractContribution)).toBe(apiContractDigest(contract));
    const parsed = parseAPIExposureCatalog(JSON.stringify(exampleCatalog(contract)));
    expect(parsed.exposures[0].contract.contractId).toBe("platform.demo.api");
    expect(Object.isFrozen(parsed.exposures[0].contract)).toBe(true);
  });

  it("rejects digest mismatch, external references, and route key collisions", () => {
    const contract = exampleContract();
    const mismatched = exampleCatalog(contract) as unknown as MutableCatalog;
    mismatched.exposures[0].exposure.contract.contractDigest = "b".repeat(64);
    expect(() => parseAPIExposureCatalog(JSON.stringify(mismatched))).toThrow(/摘要/);

    const external = exampleContract() as unknown as MutableContract;
    external.routes[0].requestSchema = { $ref: "https://attacker.example/schema.json" };
    expect(() => parseAPIExposureCatalog(JSON.stringify(exampleCatalog(external as unknown as APIContractContribution)))).toThrow(/外部 Schema/);

    const collision = exampleCatalog(contract) as unknown as MutableCatalog;
    collision.exposures.push(structuredClone(collision.exposures[0]));
    collision.exposures[1].exposure.id = "exp_bbbbbbbbbbbbbbbbbbbb";
    expect(() => parseAPIExposureCatalog(JSON.stringify(collision))).toThrow(/Route Key 冲突/);
  });
});
