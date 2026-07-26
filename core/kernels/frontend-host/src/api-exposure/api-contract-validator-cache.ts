import Ajv2020, { type ValidateFunction } from "ajv/dist/2020.js";
import type { APIRouteContract } from "./api-exposure-contract";

export class APIContractValidatorCache {
  private readonly validators = new Map<string, { request: ValidateFunction; response: ValidateFunction }>();

  public resolve(contractDigest: string, route: APIRouteContract): { request: ValidateFunction; response: ValidateFunction } {
    const key = `${contractDigest}\0${route.id}`;
    const cached = this.validators.get(key);
    if (cached !== undefined) return cached;
    const ajv = new Ajv2020({ allErrors: false, strict: true });
    const compiled = { request: ajv.compile(route.requestSchema), response: ajv.compile(route.responseSchema) };
    if (this.validators.size >= 10_000) this.validators.clear();
    this.validators.set(key, compiled);
    return compiled;
  }
}
