import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { authorizeDeployment, callDeployment, listDeployment, rejectDeploymentRoute } from "./platform-deployment-route-support";
import { resourceName } from "./platform-route-contract";
import { requireEmptyJSONObject, withRequestJSON } from "./request-json";
import { parseRevisionID } from "./revision-route-contract";

export class PlatformDeploymentTestRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    const method = request.method ?? "GET";
    if (parts[0] === "test-target-bindings") {
      if (parts.length === 1) return listDeployment(this.client, "listTestTargetBindings", principal, target, request, response, signal);
      const id = parts.length === 2 ? resourceName(parts[1], 128) : undefined;
      if (id === undefined) return rejectDeploymentRoute(response, parts.length === 2 ? 400 : 404, parts.length === 2 ? "invalid_resource_name" : "not_found", method);
      if (!authorizeDeployment(this.client, target, "putTestTargetBinding", true, principal, "platform.deployment.test-target", response)) return true;
      if (method !== "PUT") return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
      await withRequestJSON(request, response, async (binding) => callDeployment({
        client: this.client, principal, target, operation: "putTestTargetBinding", write: true,
        payload: { id, binding }, response, signal,
      }));
      return true;
    }
    if (parts[0] !== "test-releases") return false;
    if (parts.length === 1) {
      if (method === "GET" || method === "HEAD") return listDeployment(this.client, "listTestReleases", principal, target, request, response, signal);
      if (method === "POST") {
        if (!authorizeDeployment(this.client, target, "createTestRelease", true, principal, "platform.deployment.publish", response)) return true;
        await withRequestJSON(request, response, async (release) => callDeployment({
          client: this.client, principal, target, operation: "createTestRelease", write: true,
          payload: { release }, response, signal,
        }));
        return true;
      }
      return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
    }
    const releaseId = parts.length === 3 && parts[2] === "rollback" ? parseRevisionID(parts[1]) : undefined;
    if (releaseId === undefined) return rejectDeploymentRoute(response, parts.length === 3 ? 400 : 404, parts.length === 3 ? "invalid_revision_id" : "not_found", method);
    if (!authorizeDeployment(this.client, target, "rollbackTestRelease", true, principal, "platform.deployment.publish", response)) return true;
    if (method !== "POST") return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
    await withRequestJSON(request, response, async (body) => {
      requireEmptyJSONObject(body);
      await callDeployment({
        client: this.client, principal, target, operation: "rollbackTestRelease", write: true,
        payload: { releaseId }, response, signal,
      });
    });
    return true;
  }
}
