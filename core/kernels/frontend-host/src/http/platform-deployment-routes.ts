import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { PlatformDeploymentNodeRoutes } from "./platform-deployment-node-routes";
import { PlatformDeploymentServiceRoutes } from "./platform-deployment-service-routes";
import { PlatformDeploymentTestRoutes } from "./platform-deployment-test-routes";

export class PlatformDeploymentRoutes {
  private readonly nodes: PlatformDeploymentNodeRoutes;
  private readonly services: PlatformDeploymentServiceRoutes;
  private readonly tests: PlatformDeploymentTestRoutes;

  public constructor(client: PlatformCapabilityPort) {
    this.nodes = new PlatformDeploymentNodeRoutes(client);
    this.services = new PlatformDeploymentServiceRoutes(client);
    this.tests = new PlatformDeploymentTestRoutes(client);
  }

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (parts[0] !== "deployment") return false;
    const deploymentParts = parts.slice(1);
    if (await this.nodes.handle(deploymentParts, principal, target, request, response, signal)) return true;
    if (await this.services.handle(deploymentParts, principal, target, request, response, signal)) return true;
    if (await this.tests.handle(deploymentParts, principal, target, request, response, signal)) return true;
    sendAPIError(response, 404, "not_found", request.method === "HEAD");
    return true;
  }
}
