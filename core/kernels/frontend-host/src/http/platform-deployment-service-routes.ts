import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { authorizeDeployment, callDeployment, listDeployment, rejectDeploymentRoute } from "./platform-deployment-route-support";
import { requireEmptyJSONObject, requireJSONObject, withRequestJSON } from "./request-json";
import { parseRevisionID } from "./revision-route-contract";

const actions = Object.freeze({
  submit: { operation: "submitServiceDraft", role: "platform.deployment.compose" },
  approve: { operation: "approveServiceRevision", role: "platform.deployment.approve" },
  publish: { operation: "publishServiceRevision", role: "platform.deployment.publish" },
  rollback: { operation: "rollbackServiceRevision", role: "platform.deployment.publish" },
});

export class PlatformDeploymentServiceRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    const method = request.method ?? "GET";
    if (parts.length === 1 && parts[0] === "targets") return listDeployment(this.client, "listDeploymentTargets", principal, target, request, response, signal);
    if (parts[0] !== "service-revisions") return false;
    if (parts.length === 1) {
      if (method === "GET" || method === "HEAD") return listDeployment(this.client, "listServiceRevisions", principal, target, request, response, signal);
      if (method === "POST") {
        if (!authorizeDeployment(this.client, target, "createServiceDraft", true, principal, "platform.deployment.compose", response)) return true;
        await withRequestJSON(request, response, async (body) => callDeployment({
          client: this.client, principal, target, operation: "createServiceDraft", write: true,
          payload: body, response, signal,
        }));
        return true;
      }
      return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
    }
    const revisionId = parseRevisionID(parts[1]);
    if (revisionId === undefined) return rejectDeploymentRoute(response, 400, "invalid_revision_id", method);
    if (parts.length === 2) {
      if (!authorizeDeployment(this.client, target, "updateServiceDraft", true, principal, "platform.deployment.compose", response)) return true;
      if (method !== "PUT") return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
      await withRequestJSON(request, response, async (body) => callDeployment({
        client: this.client, principal, target, operation: "updateServiceDraft", write: true,
        payload: { ...requireJSONObject(body), revisionId }, response, signal,
      }));
      return true;
    }
    if (parts.length !== 3) return rejectDeploymentRoute(response, 404, "not_found", method);
    if (parts[2] === "audit") {
      if (!authorizeDeployment(this.client, target, "listServiceRevisionAudit", false, principal, "platform.deployment.read", response)) return true;
      if (method !== "GET" && method !== "HEAD") return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
      await callDeployment({
        client: this.client, principal, target, operation: "listServiceRevisionAudit", write: false,
        payload: { revisionId }, response, signal, head: method === "HEAD", items: true,
      });
      return true;
    }
    const action = actions[parts[2] as keyof typeof actions];
    if (action === undefined) return rejectDeploymentRoute(response, 404, "not_found", method);
    if (!authorizeDeployment(this.client, target, action.operation, true, principal, action.role, response)) return true;
    if (method !== "POST") return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
    await withRequestJSON(request, response, async (body) => {
      requireEmptyJSONObject(body);
      await callDeployment({
        client: this.client, principal, target, operation: action.operation, write: true,
        payload: { revisionId }, response, signal,
      });
    });
    return true;
  }
}
