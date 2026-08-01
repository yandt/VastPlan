import type { IncomingMessage, ServerResponse } from "node:http";
import { APIContractValidatorCache } from "../api-exposure/api-contract-validator-cache";
import type { APIContractCatalogPort, GatewayInvocation } from "../api-exposure/api-exposure-contract";
import { APIBodyTooLargeError, APIUnsupportedMediaTypeError, parseAPIQuery, readAPIJSONBody } from "../api-exposure/api-invocation";
import { matchAPIRoute } from "../api-exposure/api-route-matcher";
import { CapabilityApplicationError } from "../capabilities/capability-invoker";
import { ManagementAuthorizationError, type PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError, sendJSON } from "./json-response";
import { resourceName } from "./platform-route-contract";
import { reportCapabilityFailure } from "../observability/capability-failure";

const maximumBodyBytes = 1 << 20;
const maximumResponseBytes = 8 << 20;
const timeoutMilliseconds = 30_000;

export class PlatformManagementAPIRoutes {
  private readonly validators = new APIContractValidatorCache();

  public constructor(private readonly catalog: APIContractCatalogPort, private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (parts[0] !== "api") return false;
    const method = request.method ?? "GET";
    const apiID = resourceName(parts[1], 160);
    if (apiID === undefined) return reject(response, 400, "invalid_management_api", method);
    const reference = target.service.apis?.find((candidate) => candidate.id === apiID);
    if (reference === undefined) return reject(response, 404, "management_api_not_found", method);
    let contract;
    try { contract = await this.catalog.resolveContract(reference); }
    catch { return reject(response, 502, "management_contract_unavailable", method); }
    if (contract === undefined) return reject(response, 409, "management_contract_unavailable", method);
    const contractPath = `/${parts.slice(2).join("/")}`;
    const matched = matchAPIRoute(contract, method === "HEAD" ? "GET" : method, contractPath);
    if (matched === "method-not-allowed") return reject(response, 405, "method_not_allowed", method);
    if (matched === undefined) return reject(response, 404, "not_found", method);
    const write = matched.route.method !== "GET";
    try { this.client.authorize(target, matched.route.target.capability, matched.route.target.operation, write); }
    catch { return reject(response, 403, "management_binding_forbidden", method); }

    let body: unknown, query: Readonly<Record<string, readonly string[]>>;
    try {
      body = await readAPIJSONBody(request, maximumBodyBytes, method);
      query = parseAPIQuery(request.url ?? contractPath);
    } catch (error) {
      const status = error instanceof APIUnsupportedMediaTypeError ? 415 : error instanceof APIBodyTooLargeError ? 413 : 400;
      const code = error instanceof APIUnsupportedMediaTypeError ? "unsupported_media_type" : error instanceof APIBodyTooLargeError ? "body_too_large" : "invalid_request";
      return reject(response, status, code, method);
    }
    const validators = this.validators.resolve(reference.contractDigest, matched.route);
    if (!validators.request(body)) return reject(response, 422, "request_schema_rejected", method);
    const invocation: GatewayInvocation = {
      schemaVersion: "v1", routeId: matched.route.id, method: matched.route.method,
      pathParams: matched.pathParams, query, body,
    };
    const boundedSignal = AbortSignal.any([signal, AbortSignal.timeout(timeoutMilliseconds)]);
    try {
      const raw = await this.client.call(principal, target, matched.route.target.capability, matched.route.target.operation, write, Buffer.from(JSON.stringify(invocation)), boundedSignal);
      if (raw.byteLength > maximumResponseBytes) return reject(response, 502, "upstream_invalid_response", method);
      let value: unknown;
      try { value = JSON.parse(new TextDecoder().decode(raw)) as unknown; }
      catch { return reject(response, 502, "upstream_invalid_response", method); }
      if (!validators.response(value)) return reject(response, 502, "upstream_invalid_response", method);
      if (matched.route.successStatus === 204) { response.statusCode = 204; response.end(); return true; }
      sendJSON(response, matched.route.successStatus, value, method === "HEAD");
      return true;
    } catch (error) {
      reportCapabilityFailure({ operation: matched.route.target.operation, capability: matched.route.target.capability, logicalService: target.service.logicalService }, error);
      if (error instanceof ManagementAuthorizationError) return reject(response, 403, "management_binding_forbidden", method);
      if (error instanceof CapabilityApplicationError) {
        if (error.code === "permission.denied") return reject(response, 403, "forbidden", method);
        const mapping = matched.route.errors?.find((candidate) => candidate.code === error.code);
        return reject(response, mapping?.status ?? 502, mapping?.code ?? "upstream_rejected", method);
      }
      return reject(response, boundedSignal.aborted ? 504 : 502, boundedSignal.aborted ? "upstream_timeout" : "platform_service_unavailable", method);
    }
  }
}

function reject(response: ServerResponse, status: number, code: string, method: string): true {
  sendAPIError(response, status, code, method === "HEAD");
  return true;
}
