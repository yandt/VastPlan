import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import { ManagementResolutionError, type PlatformManagementResolver } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { PlatformCredentialsRoutes } from "./platform-credentials-routes";
import { PlatformArtifactRoutes } from "./platform-artifact-routes";
import { PlatformDeploymentRoutes } from "./platform-deployment-routes";
import { PlatformDatabaseRoutes } from "./platform-database-routes";
import { requestHostname, resourceName } from "./platform-route-contract";
import { PlatformSettingsRoutes } from "./platform-settings-routes";
import { PlatformAuthenticationProviderRoutes } from "./platform-authentication-provider-routes";
import { PlatformSeedHandoffRoutes } from "./platform-seed-handoff-routes";
import type { IdentityProvider } from "../identity/identity-provider";

const prefix = "/v1/portals/";

export class PlatformManagementRoutes {
  private readonly settings: PlatformSettingsRoutes;
  private readonly credentials: PlatformCredentialsRoutes;
  private readonly database: PlatformDatabaseRoutes;
  private readonly artifacts: PlatformArtifactRoutes;
  private readonly deployment: PlatformDeploymentRoutes;
  private readonly authenticationProviders: PlatformAuthenticationProviderRoutes;
  private readonly seedHandoff: PlatformSeedHandoffRoutes;

  public constructor(private readonly resolver: PlatformManagementResolver, client: PlatformCapabilityPort, identity: IdentityProvider) {
    this.settings = new PlatformSettingsRoutes(client);
    this.credentials = new PlatformCredentialsRoutes(client);
    this.database = new PlatformDatabaseRoutes(client);
    this.artifacts = new PlatformArtifactRoutes(client);
    this.deployment = new PlatformDeploymentRoutes(client);
    this.authenticationProviders = new PlatformAuthenticationProviderRoutes(client, identity);
    this.seedHandoff = new PlatformSeedHandoffRoutes(client, identity);
  }

  public async handle(path: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (!path.startsWith(prefix)) return false;
    const method = request.method ?? "GET";
    const parts = path.slice(prefix.length).split("/");
    if (parts.length < 5 || parts[1] !== "platform" || parts[2] !== "services") return reject(response, 404, "not_found", method);
    const portalId = resourceName(parts[0], 128);
    const serviceId = resourceName(parts[3], 160);
    if (portalId === undefined || serviceId === undefined) return reject(response, 400, "invalid_management_target", method);
    let target;
    try { target = await this.resolver.resolve(principal, portalId, serviceId, requestHostname(request), signal); }
    catch (error) {
      if (error instanceof ManagementResolutionError) return reject(response, managementStatus(error.code), error.code, method);
      return reject(response, 502, "platform_service_unavailable", method);
    }
    const resourceParts = parts.slice(4);
    if (await this.settings.handle(resourceParts, principal, target, request, response, signal)) return true;
    if (await this.credentials.handle(resourceParts, principal, target, request, response, signal)) return true;
    if (await this.database.handle(resourceParts, principal, target, request, response, signal)) return true;
    if (await this.artifacts.handle(resourceParts, principal, target, request, response, signal)) return true;
    if (await this.deployment.handle(resourceParts, principal, target, request, response, signal)) return true;
    if (await this.authenticationProviders.handle(resourceParts, principal, target, request, response, signal)) return true;
    if (await this.seedHandoff.handle(resourceParts, principal, target, request, response, signal)) return true;
    return reject(response, 404, "not_found", method);
  }
}

function managementStatus(code: string): number {
  if (code === "portal_audience_forbidden") return 403;
  if (code === "portal_management_binding_rejected") return 409;
  if (code === "portal_not_found" || code === "managed_service_not_found") return 404;
  return 502;
}
function reject(response: ServerResponse, status: number, code: string, method: string): true { sendAPIError(response, status, code, method === "HEAD"); return true; }
