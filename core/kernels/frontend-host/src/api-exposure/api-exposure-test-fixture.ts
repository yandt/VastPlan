import type { APIContractContribution, APIExposureCatalog } from "./api-exposure-contract";
import { apiContractDigest } from "./api-exposure-schema";

export function exampleContract(): APIContractContribution {
  return {
    id: "management-api", service_role: "backend", contractId: "platform.demo.api", contractVersion: "1.0.0", protocol: "http-json",
    routes: [{
      id: "platform.demo.list", method: "GET", path: "/items/{itemId}",
      target: { capability: "platform.demo", operation: "listItems" },
      requestSchema: { type: "object", additionalProperties: false },
      responseSchema: { type: "object", additionalProperties: false, properties: { ok: { type: "boolean" } }, required: ["ok"] },
      successStatus: 200, errors: [{ code: "platform.demo.not_found", status: 404 }],
    }],
  };
}

export function exampleCatalog(contract = exampleContract()): APIExposureCatalog {
  return {
    schemaVersion: "v1", generation: 1,
    dataPlaneExposures: [], exposures: [{
      exposure: {
        schemaVersion: "v1", id: "exp_aaaaaaaaaaaaaaaaaaaa", revision: 1, routeKey: "aaaaaaaaaaaaaaaaaaaa",
        displayName: "演示 API", tenantId: "tenant-a", portalId: "operations", hosts: ["127.0.0.1"],
        contract: {
          pluginId: "cn.vastplan.platform.demo", artifactSha256: "a".repeat(64), contributionId: contract.id,
          contractId: contract.contractId, contractVersion: contract.contractVersion, contractDigest: apiContractDigest(contract),
        },
        authentication: { profileId: "auth.file", allowAnonymous: false }, requiredPermissions: ["platform.demo.read"],
        limits: { maxBodyBytes: 1024, maxResponseBytes: 4096, requestsPerMinute: 60, timeoutMs: 5000 },
        target: { logicalService: "backend.default", routingDomain: "platform.default" },
      },
      contract,
    }],
  };
}

export type MutableContract = {
  routes: Array<{ requestSchema: Record<string, unknown> }>;
} & Record<string, unknown>;

export type MutableCatalog = {
  exposures: Array<{
    exposure: { id: string; contract: { contractDigest: string } };
    contract: APIContractContribution;
  }>;
} & Record<string, unknown>;
