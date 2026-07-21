import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { authorizeDeployment, callDeployment, listDeployment, rejectDeploymentRoute } from "./platform-deployment-route-support";
import { resourceName } from "./platform-route-contract";
import { requireEmptyJSONObject, requireJSONObject, withRequestJSON } from "./request-json";

export class PlatformDeploymentNodeRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    const method = request.method ?? "GET";
    if (parts.length === 1 && (parts[0] === "nodes" || parts[0] === "bootstrap-jobs")) {
      const operation = parts[0] === "nodes" ? "listNodes" : "listBootstrapJobs";
      return listDeployment(this.client, operation, principal, target, request, response, signal);
    }
    if (parts.length === 2 && parts[0] === "nodes") {
      const id = resourceName(parts[1], 128);
      if (id === undefined) return rejectDeploymentRoute(response, 400, "invalid_resource_name", method);
      if (!authorizeDeployment(this.client, target, "putNode", true, principal, "platform.deployment.write", response)) return true;
      if (method !== "PUT") return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
      await withRequestJSON(request, response, async (body) => callDeployment({
        client: this.client, principal, target, operation: "putNode", write: true,
        payload: { ...requireJSONObject(body), id }, response, signal,
      }));
      return true;
    }
    if (parts.length === 3 && ((parts[0] === "nodes" && parts[2] === "bootstrap") || (parts[0] === "bootstrap-jobs" && parts[2] === "approve"))) {
      const id = resourceName(parts[1], 128);
      if (id === undefined) return rejectDeploymentRoute(response, 400, "invalid_resource_name", method);
      const create = parts[0] === "nodes";
      const operation = create ? "createBootstrap" : "approveBootstrap";
      const role = create ? "platform.deployment.bootstrap" : "platform.deployment.approve";
      if (!authorizeDeployment(this.client, target, operation, true, principal, role, response)) return true;
      if (method !== "POST") return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
      await withRequestJSON(request, response, async (body) => {
        requireEmptyJSONObject(body);
        await callDeployment({
          client: this.client, principal, target, operation, write: true,
          payload: create ? { nodeId: id } : { jobId: id }, response, signal,
        });
      });
      return true;
    }
    return false;
  }
}
