import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { PlatformDeploymentNodeRoutes } from "./platform-deployment-node-routes";
import { PlatformDeploymentServiceRoutes } from "./platform-deployment-service-routes";
import { PlatformDeploymentTestRoutes } from "./platform-deployment-test-routes";
import { PlatformDeploymentInstallationRoutes } from "./platform-deployment-installation-routes";
import { PlatformDeploymentSelfServiceInstallationRoutes } from "./platform-deployment-self-service-installation-routes";

export class PlatformDeploymentRoutes {
  private readonly nodes: PlatformDeploymentNodeRoutes;
  private readonly services: PlatformDeploymentServiceRoutes;
  private readonly installations: PlatformDeploymentInstallationRoutes;
  private readonly tests: PlatformDeploymentTestRoutes;
  private readonly selfServiceInstallations: PlatformDeploymentSelfServiceInstallationRoutes;

  public constructor(client: PlatformCapabilityPort) {
    this.nodes = new PlatformDeploymentNodeRoutes(client);
    this.services = new PlatformDeploymentServiceRoutes(client);
    this.installations = new PlatformDeploymentInstallationRoutes(client);
    this.selfServiceInstallations = new PlatformDeploymentSelfServiceInstallationRoutes(client);
    this.tests = new PlatformDeploymentTestRoutes(client);
  }

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (parts[0] !== "deployment") return false;
    const deploymentParts = parts.slice(1);
    if (await this.nodes.handle(deploymentParts, principal, target, request, response, signal)) return true;
    if (await this.services.handle(deploymentParts, principal, target, request, response, signal)) return true;
    if (await this.installations.handle(deploymentParts, principal, target, request, response, signal)) return true;
    if (await this.selfServiceInstallations.handle(deploymentParts, principal, target, request, response, signal)) return true;
    if (await this.tests.handle(deploymentParts, principal, target, request, response, signal)) return true;
    sendAPIError(response, 404, "not_found", request.method === "HEAD");
    return true;
  }
}
