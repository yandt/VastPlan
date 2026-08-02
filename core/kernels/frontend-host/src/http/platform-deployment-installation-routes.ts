import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { authorizeDeployment, callDeployment, rejectDeploymentRoute } from "./platform-deployment-route-support";
import { RequestJSONError, requireEmptyJSONObject, requireJSONObject, withRequestJSON } from "./request-json";

const candidateActions = Object.freeze({
  submit: { operation: "submitPluginInstallationCandidate", role: "platform.deployment.plugin.request" },
  approve: { operation: "approvePluginInstallationCandidate", role: "platform.deployment.plugin.approve" },
  activate: { operation: "activatePluginInstallationCandidate", role: "platform.deployment.plugin.activate" },
  cancel: { operation: "cancelPluginInstallationCandidate", role: "platform.deployment.plugin.request" },
  rollback: { operation: "rollbackPluginInstallationCandidate", role: "platform.deployment.plugin.activate" },
});

/**
 * Fixed Portal BFF routes for the controller installation source. The browser
 * can describe a logical target, but it cannot select a capability endpoint,
 * logical service or source policy; those remain owned by ManagementTarget.
 */
export class PlatformDeploymentInstallationRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (parts[0] !== "plugin-installations") return false;
    const method = request.method ?? "GET";
    if (parts.length === 1) {
      if (method === "GET" || method === "HEAD") {
        if (!authorizeDeployment(this.client, target, "listPluginInstallationCandidates", false, principal, "platform.deployment.plugin.preview", response)) return true;
        await callDeployment({ client: this.client, principal, target, operation: "listPluginInstallationCandidates", write: false, payload: {}, response, signal, head: method === "HEAD", items: true });
        return true;
      }
      if (method === "POST") return this.previewMutation("createPluginInstallationCandidate", "platform.deployment.plugin.request", principal, target, request, response, signal);
      return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
    }
    if (parts.length === 2 && parts[1] === "preview") {
      if (method !== "POST") return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
      return this.previewMutation("previewPluginInstallation", "platform.deployment.plugin.preview", principal, target, request, response, signal, false);
    }
    if (parts.length === 2 && parts[1] === "targets") {
      if (!authorizeDeployment(this.client, target, "listPluginInstallationTargets", false, principal, "platform.deployment.plugin.preview", response)) return true;
      if (method !== "GET" && method !== "HEAD") return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
      await callDeployment({ client: this.client, principal, target, operation: "listPluginInstallationTargets", write: false, payload: {}, response, signal, head: method === "HEAD", items: true });
      return true;
    }
    const candidateId = installationCandidateID(parts[1]);
    if (candidateId === undefined) return rejectDeploymentRoute(response, 400, "invalid_installation_candidate_id", method);
    if (parts.length === 2) {
      if (!authorizeDeployment(this.client, target, "getPluginInstallationCandidate", false, principal, "platform.deployment.plugin.preview", response)) return true;
      if (method !== "GET" && method !== "HEAD") return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
      await callDeployment({ client: this.client, principal, target, operation: "getPluginInstallationCandidate", write: false, payload: { candidateId }, response, signal, head: method === "HEAD" });
      return true;
    }
    if (parts.length !== 3) return rejectDeploymentRoute(response, 404, "not_found", method);
    const action = candidateActions[parts[2] as keyof typeof candidateActions];
    if (action === undefined) return rejectDeploymentRoute(response, 404, "not_found", method);
    if (!authorizeDeployment(this.client, target, action.operation, true, principal, action.role, response)) return true;
    if (method !== "POST") return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
    await withRequestJSON(request, response, async (body) => {
      requireEmptyJSONObject(body);
      await callDeployment({ client: this.client, principal, target, operation: action.operation, write: true, payload: { candidateId }, response, signal });
    });
    return true;
  }

  private async previewMutation(operation: string, role: string, principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal, write = true): Promise<true> {
    if (!authorizeDeployment(this.client, target, operation, write, principal, role, response)) return true;
    await withRequestJSON(request, response, async (body) => {
      await callDeployment({ client: this.client, principal, target, operation, write, payload: { installationPreview: installationPreviewRequest(body) }, response, signal });
    });
    return true;
  }
}

function installationCandidateID(value: string | undefined): string | undefined {
  if (value === undefined) return undefined;
  try {
    const decoded = decodeURIComponent(value);
    return /^installation-[a-f0-9]{32}$/.test(decoded) ? decoded : undefined;
  } catch { return undefined; }
}

function installationPreviewRequest(value: unknown): Readonly<Record<string, unknown>> {
  const request = exactObject(value, ["version", "target", "change"], ["expectedActiveRevision"]);
  if (request.version !== 1) throw new RequestJSONError("插件安装协议版本无效");
  if (request.expectedActiveRevision !== undefined && (!Number.isSafeInteger(request.expectedActiveRevision) || Number(request.expectedActiveRevision) < 1)) throw new RequestJSONError("活动修订无效");
  const target = exactObject(request.target, ["kernel", "deployment", "unitId"]);
  if (target.kernel !== "backend" || !boundedName(target.deployment, 128) || !boundedName(target.unitId, 128)) throw new RequestJSONError("插件安装目标无效");
  const change = exactObject(request.change, ["action", "pluginId"], ["requirement"]);
  if (!(["install", "upgrade", "remove"] as unknown[]).includes(change.action) || !pluginID(change.pluginId)) throw new RequestJSONError("插件安装变更无效");
  if (change.action === "remove") {
    if (change.requirement !== undefined) throw new RequestJSONError("卸载请求不能包含版本要求");
  } else {
    const requirement = exactObject(change.requirement, ["pluginId", "constraint"], ["channel", "features"]);
    if (requirement.pluginId !== change.pluginId || typeof requirement.constraint !== "string" || requirement.constraint.length < 1 || requirement.constraint.length > 128) throw new RequestJSONError("插件版本要求无效");
    if (requirement.channel !== undefined && !boundedName(requirement.channel, 32)) throw new RequestJSONError("插件通道无效");
    if (requirement.features !== undefined && (!Array.isArray(requirement.features) || requirement.features.length > 64 || requirement.features.some((item) => !boundedName(item, 128)))) throw new RequestJSONError("插件 Feature 无效");
  }
  return request;
}

function exactObject(value: unknown, required: readonly string[], optional: readonly string[] = []): Readonly<Record<string, unknown>> {
  const object = requireJSONObject(value);
  const allowed = new Set([...required, ...optional]);
  if (required.some((key) => !Object.hasOwn(object, key)) || Object.keys(object).some((key) => !allowed.has(key))) throw new RequestJSONError("插件安装请求字段无效");
  return object;
}

function boundedName(value: unknown, maximum: number): value is string {
  return typeof value === "string" && value.trim() === value && value.length > 0 && value.length <= maximum && !value.includes("/") && !value.includes("\\") && !value.includes("\0");
}

function pluginID(value: unknown): value is string {
  return typeof value === "string" && value.length <= 255 && /^[a-z0-9]+(?:[.-][a-z0-9]+)+$/.test(value);
}
