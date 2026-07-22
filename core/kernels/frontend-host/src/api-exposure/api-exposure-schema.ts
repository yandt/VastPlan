import { createHash } from "node:crypto";
import Ajv2020, { type ValidateFunction } from "ajv/dist/2020.js";
import apiExposureSchema from "../../../../../contracts/schemas/api/v1/vastplan.api-exposure.schema.json";
import type { APIContractContribution, APIExposureCatalog, ResolvedAPIExposure } from "./api-exposure-contract";

const ajv = new Ajv2020({ allErrors: true, strict: true });
ajv.addFormat("uri", true);
ajv.addFormat("date-time", true);
ajv.addSchema(apiExposureSchema);
const validateCatalog = requiredValidator(`${apiExposureSchema.$id}#/$defs/exposureCatalog`);

export class APIExposureContractError extends Error {
  public constructor(message: string) {
    super(message);
    this.name = "APIExposureContractError";
  }
}

export function parseAPIExposureCatalog(raw: string): APIExposureCatalog {
  let value: unknown;
  try { value = JSON.parse(raw) as unknown; }
  catch { throw new APIExposureContractError("API Exposure Catalog 不是有效 JSON"); }
  if (!validateCatalog(value)) throw new APIExposureContractError(`API Exposure Catalog 不符合 Schema: ${ajv.errorsText(validateCatalog.errors)}`);
  const catalog = value as APIExposureCatalog;
  const ids = new Set<string>();
  const routeKeys = new Set<string>();
  for (const resolved of catalog.exposures) {
    validateResolvedExposure(resolved);
    if (ids.has(resolved.exposure.id)) throw new APIExposureContractError(`API Exposure id 重复: ${resolved.exposure.id}`);
    if (routeKeys.has(resolved.exposure.routeKey)) throw new APIExposureContractError(`API Exposure Route Key 冲突: ${resolved.exposure.routeKey}`);
    ids.add(resolved.exposure.id);
    routeKeys.add(resolved.exposure.routeKey);
  }
  for (const exposure of catalog.dataPlaneExposures) {
    if (ids.has(exposure.id)) throw new APIExposureContractError(`Exposure id 重复: ${exposure.id}`);
    if (routeKeys.has(exposure.routeKey)) throw new APIExposureContractError(`Exposure Route Key 冲突: ${exposure.routeKey}`);
    if (exposure.hosts.some((host) => host !== host.toLowerCase() || host.endsWith("."))) {
      throw new APIExposureContractError(`Data Plane Exposure ${exposure.id} 的 Host 未规范化`);
    }
    for (const origin of exposure.allowedEndpointOrigins) {
      const parsed = new URL(origin);
      if (parsed.protocol !== "https:" || parsed.username !== "" || parsed.password !== "" || parsed.pathname !== "/" || parsed.search !== "" || parsed.hash !== "" || origin !== origin.toLowerCase()) {
        throw new APIExposureContractError(`Data Plane Exposure ${exposure.id} 的 Endpoint Origin 未规范化`);
      }
    }
    const identityPrefix = new URL(exposure.tlsIdentityPrefix);
    if (identityPrefix.protocol !== "spiffe:" || identityPrefix.host === "" || !identityPrefix.pathname.endsWith("/") || identityPrefix.search !== "" || identityPrefix.hash !== "") {
      throw new APIExposureContractError(`Data Plane Exposure ${exposure.id} 的 TLS Identity Prefix 无效`);
    }
    ids.add(exposure.id);
    routeKeys.add(exposure.routeKey);
  }
  return deepFreeze(structuredClone(catalog));
}

export function apiContractDigest(contract: APIContractContribution): string {
  const normalized = structuredClone(contract) as unknown as APIContractMutable;
  normalized.routes.sort((left, right) => left.id.localeCompare(right.id));
  for (const route of normalized.routes) {
    if (route.errors !== undefined) route.errors.sort((left, right) => left.code.localeCompare(right.code));
  }
  return createHash("sha256").update(canonicalJSON(normalized)).digest("hex");
}

function validateResolvedExposure(resolved: ResolvedAPIExposure): void {
  const reference = resolved.exposure.contract;
  const contract = resolved.contract;
  if (reference.contributionId !== contract.id || reference.contractId !== contract.contractId || reference.contractVersion !== contract.contractVersion) {
    throw new APIExposureContractError(`API Exposure ${resolved.exposure.id} 的契约引用不一致`);
  }
  if (apiContractDigest(contract) !== reference.contractDigest) {
    throw new APIExposureContractError(`API Exposure ${resolved.exposure.id} 的契约摘要不一致`);
  }
  if (resolved.exposure.hosts.some((host) => host !== host.toLowerCase() || host.endsWith("."))) {
    throw new APIExposureContractError(`API Exposure ${resolved.exposure.id} 的 Host 未规范化`);
  }
  for (const route of contract.routes) {
    validateRouteTemplate(route.path);
    rejectExternalReference(route.requestSchema);
    rejectExternalReference(route.responseSchema);
    validateInlineSchema(route.requestSchema, `${route.id} requestSchema`);
    validateInlineSchema(route.responseSchema, `${route.id} responseSchema`);
  }
}

function validateRouteTemplate(path: string): void {
  const names = new Set<string>();
  for (const segment of path.split("/").slice(1)) {
    if (!segment.startsWith("{")) continue;
    const name = segment.slice(1, -1);
    if (names.has(name)) throw new APIExposureContractError(`API route path 参数重复: ${name}`);
    names.add(name);
  }
}

function rejectExternalReference(value: unknown): void {
  if (Array.isArray(value)) {
    for (const child of value) rejectExternalReference(child);
    return;
  }
  if (typeof value !== "object" || value === null) return;
  for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
    if (key === "$ref" && typeof child === "string" && !child.startsWith("#")) {
      throw new APIExposureContractError(`API Contract 不得引用外部 Schema: ${child}`);
    }
    rejectExternalReference(child);
  }
}

function validateInlineSchema(value: Readonly<Record<string, unknown>>, name: string): void {
  try { new Ajv2020({ allErrors: false, strict: true }).compile(value); }
  catch (error) {
    throw new APIExposureContractError(`API Contract ${name} 无效: ${error instanceof Error ? error.message : String(error)}`);
  }
}

function canonicalJSON(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonicalJSON).join(",")}]`;
  if (typeof value === "object" && value !== null) {
    const record = value as Readonly<Record<string, unknown>>;
    return `{${Object.keys(record).sort().map((key) => `${canonicalString(key)}:${canonicalJSON(record[key])}`).join(",")}}`;
  }
  return canonicalStringify(value);
}

function canonicalString(value: string): string {
  return canonicalStringify(value);
}

function canonicalStringify(value: unknown): string {
  const encoded = JSON.stringify(value);
  if (encoded === undefined) throw new APIExposureContractError("API Contract 包含不可序列化值");
  return encoded.replace(/\u2028/g, "\\u2028").replace(/\u2029/g, "\\u2029");
}

function requiredValidator(reference: string): ValidateFunction {
  const validator = ajv.getSchema(reference);
  if (validator === undefined) throw new Error(`缺少 API Exposure Schema: ${reference}`);
  return validator;
}

type APIRouteMutable = {
  id: string;
  errors?: { code: string; status: number }[];
};

type APIContractMutable = Omit<APIContractContribution, "routes"> & { routes: APIRouteMutable[] };

function deepFreeze<T>(value: T): T {
  if (typeof value !== "object" || value === null || Object.isFrozen(value)) return value;
  Object.freeze(value);
  for (const child of Object.values(value as Record<string, unknown>)) deepFreeze(child);
  return value;
}
