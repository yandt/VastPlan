import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { authorizeDeployment, callDeployment, rejectDeploymentRoute } from "./platform-deployment-route-support";
import { selfServiceInstallationRequest } from "./platform-deployment-installation-request";
import { RequestJSONError, requireEmptyJSONObject, requireJSONObject, withRequestJSON } from "./request-json";

const actions = Object.freeze({
  submit: { operation: "submitSelfServicePluginInstallationCandidate", role: "platform.deployment.plugin.request" },
  approve: { operation: "approveSelfServicePluginInstallationCandidate", role: "platform.deployment.plugin.approve" },
  activate: { operation: "activateSelfServicePluginInstallationCandidate", role: "platform.deployment.plugin.activate" },
  cancel: { operation: "cancelSelfServicePluginInstallationCandidate", role: "platform.deployment.plugin.request" },
  rollback: { operation: "rollbackSelfServicePluginInstallationCandidate", role: "platform.deployment.plugin.activate" },
});

export class PlatformDeploymentSelfServiceInstallationRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (parts[0] !== "service-plugin-installations") return false;
    const resource = target.service.resource;
    const method = request.method ?? "GET";
    if (resource === undefined || resource.kind !== "service-unit" || resource.kernel !== "backend") return rejectDeploymentRoute(response, 409, "managed_service_resource_required", method);
    const installationTarget = { kernel: resource.kernel, deployment: resource.deployment, unitId: resource.unitId } as const;
    if (parts.length === 1) {
      if (method === "GET" || method === "HEAD") {
        if (!authorizeDeployment(this.client, target, "listSelfServicePluginInstallationCandidates", false, principal, "platform.deployment.plugin.preview", response)) return true;
        await callDeployment({ client: this.client, principal, target, operation: "listSelfServicePluginInstallationCandidates", write: false, payload: { installationTarget }, response, signal, head: method === "HEAD", items: true });
        return true;
      }
      if (method === "POST") return this.mutate("createSelfServicePluginInstallationCandidate", "platform.deployment.plugin.request", principal, target, installationTarget, request, response, signal);
      return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
    }
    if (parts.length === 2 && parts[1] === "preview") {
      if (method !== "POST") return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
      return this.mutate("previewSelfServicePluginInstallation", "platform.deployment.plugin.preview", principal, target, installationTarget, request, response, signal, false);
    }
    const candidateId = candidateID(parts[1]);
    if (candidateId === undefined) return rejectDeploymentRoute(response, 400, "invalid_installation_candidate_id", method);
    if (parts.length === 2) {
      if (method !== "GET" && method !== "HEAD") return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
      if (!authorizeDeployment(this.client, target, "getSelfServicePluginInstallationCandidate", false, principal, "platform.deployment.plugin.preview", response)) return true;
      await callDeployment({ client: this.client, principal, target, operation: "getSelfServicePluginInstallationCandidate", write: false, payload: { candidateId, installationTarget }, response, signal, head: method === "HEAD" });
      return true;
    }
    if (parts.length !== 3) return rejectDeploymentRoute(response, 404, "not_found", method);
    const action = actions[parts[2] as keyof typeof actions];
    if (action === undefined) return rejectDeploymentRoute(response, 404, "not_found", method);
    if (method !== "POST") return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
    if (!authorizeDeployment(this.client, target, action.operation, true, principal, action.role, response)) return true;
    await withRequestJSON(request, response, async (body) => {
      const approvalEvidence = parts[2] === "approve" ? evidenceRequest(body) : (requireEmptyJSONObject(body), undefined);
      await callDeployment({ client: this.client, principal, target, operation: action.operation, write: true, payload: { candidateId, installationTarget, ...(approvalEvidence === undefined ? {} : { approvalEvidence }) }, response, signal });
    });
    return true;
  }

  private async mutate(operation: string, role: string, principal: Principal, target: PlatformManagementTarget, installationTarget: Readonly<{ kernel: "backend"; deployment: string; unitId: string }>, request: IncomingMessage, response: ServerResponse, signal: AbortSignal, write = true): Promise<true> {
    if (!authorizeDeployment(this.client, target, operation, write, principal, role, response)) return true;
    await withRequestJSON(request, response, async (body) => {
      await callDeployment({ client: this.client, principal, target, operation, write, payload: { installationPreview: selfServiceInstallationRequest(body, installationTarget, target.portalId) }, response, signal });
    });
    return true;
  }
}

function evidenceRequest(value: unknown): Readonly<Record<string, unknown>> {
  const request = requireJSONObject(value);
  if (Object.keys(request).some((key) => key !== "evidence")) throw new RequestJSONError("审批请求字段无效");
  if (request.evidence === undefined) return Object.freeze({});
  const evidence = requireJSONObject(request.evidence);
  if (Object.keys(evidence).length > 32 || Object.keys(evidence).some((key) => !/^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$/.test(key))) throw new RequestJSONError("审批证据字段无效");
  return evidence;
}

function candidateID(value: string | undefined): string | undefined {
  if (value === undefined) return undefined;
  try { const decoded = decodeURIComponent(value); return /^installation-[a-f0-9]{32}$/.test(decoded) ? decoded : undefined; }
  catch { return undefined; }
}
