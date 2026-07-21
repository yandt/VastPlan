import type { IncomingMessage, ServerResponse } from "node:http";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import { PortalActivationCatalog, type PortalActivation } from "../runtime/portal-activation-catalog";
import type { PortalDeliveryStore } from "../runtime/portal-delivery-store";
import { recoveryRuntime, type PortalRuntimeSpec } from "../runtime/portal-runtime-contract";
import { classifyPortalUpdate, PortalUpdateCoordinator, type PortalUpdate } from "../runtime/portal-update-coordinator";
import { sendAPIError, sendJSON } from "./json-response";
import { sendPortalModule } from "./portal-module-response";
import { parsePortalModulePath, portalUpdateQuery, requestedPortalPath } from "./portal-runtime-path";
import { requestHostname } from "./platform-route-contract";

const runtimePaths = new Set(["/v1/portal-runtime", "/v1/portal-recovery", "/v1/portal-updates"]);

export class PortalRuntimeRoutes {
  private readonly activations: PortalActivationCatalog;
  private readonly updates: PortalUpdateCoordinator;

  public constructor(composer: PortalComposerPort, private readonly delivery: PortalDeliveryStore) {
    this.activations = new PortalActivationCatalog(composer);
    this.updates = new PortalUpdateCoordinator(this.activations, delivery);
  }

  public async handle(path: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    const moduleRequest = parsePortalModulePath(path);
    if (!runtimePaths.has(path) && moduleRequest === undefined) return false;
    const method = request.method ?? "GET";
    if ((path === "/v1/portal-updates" && method !== "GET") || (path !== "/v1/portal-updates" && method !== "GET" && method !== "HEAD")) {
      sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
      return true;
    }
    let activations: readonly PortalActivation[];
    try { activations = await this.activations.list(principal, signal); }
    catch { sendAPIError(response, 502, "portal_service_unavailable", method === "HEAD"); return true; }
    if (moduleRequest !== undefined) return this.module(moduleRequest, activations, principal, request, response);
    if (path === "/v1/portal-updates") return this.updateStream(activations, principal, request, response);
    const requested = requestedPortalPath(request.url);
    if (requested === undefined) {
      sendAPIError(response, 400, "invalid_portal_path", method === "HEAD");
      return true;
    }
    const active = this.activations.selectCurrent(activations, principal, requested, requestHostname(request));
    if (active === undefined) {
      sendAPIError(response, 404, path === "/v1/portal-runtime" ? "portal_not_found" : "portal_recovery_not_found", method === "HEAD");
      return true;
    }
    if (!this.activations.audienceAllows(active, principal)) {
      sendAPIError(response, 403, "portal_audience_forbidden", method === "HEAD");
      return true;
    }
    return path === "/v1/portal-runtime"
      ? this.runtime(active, principal, response, method === "HEAD")
      : this.recovery(active, activations, principal, response, method === "HEAD");
  }

  private async updateStream(activations: readonly PortalActivation[], principal: Principal, request: IncomingMessage, response: ServerResponse): Promise<true> {
    const query = portalUpdateQuery(request.url);
    if (query === undefined) { sendAPIError(response, 400, "portal_update_revision_invalid"); return true; }
    const active = this.activations.selectCurrent(activations, principal, query.path, requestHostname(request));
    if (active === undefined) { sendAPIError(response, 404, "portal_not_found"); return true; }
    if (!this.activations.audienceAllows(active, principal)) { sendAPIError(response, 403, "portal_audience_forbidden"); return true; }
    if (updateMode(active.resolved) === "refresh") { sendAPIError(response, 404, "not_found"); return true; }
    if (query.revision > active.id) { sendAPIError(response, 400, "portal_update_revision_invalid"); return true; }
    const previous = activations.find((activation) => activation.tenantId === active.tenantId && activation.portalId === active.portalId && activation.id === query.revision);
    const mode = query.revision === active.id ? "current" : previous === undefined ? "host-epoch" : classifyPortalUpdate(previous.resolved, active.resolved);
    response.setHeader("Content-Type", "text/event-stream");
    response.setHeader("Cache-Control", "no-store");
    response.setHeader("X-Accel-Buffering", "no");
    response.statusCode = 200;
    writeUpdate(response, { portalId: active.portalId, activationId: active.id, mode });
    const unsubscribe = this.updates.subscribe(principal, active, (update) => writeUpdate(response, update));
    const heartbeat = setInterval(() => response.write(": heartbeat\n\n"), 15_000);
    heartbeat.unref();
    await new Promise<void>((resolve) => response.once("close", resolve));
    clearInterval(heartbeat);
    unsubscribe();
    return true;
  }

  private async runtime(active: PortalActivation, principal: Principal, response: ServerResponse, head: boolean): Promise<true> {
    try {
      const runtime = await this.delivery.runtime(principal.tenantId, active.resolved);
      addPreloads(response, runtime);
      sendJSON(response, 200, runtime, head);
    } catch { sendAPIError(response, 409, "portal_runtime_rejected", head); }
    return true;
  }

  private async recovery(active: PortalActivation, activations: readonly PortalActivation[], principal: Principal, response: ServerResponse, head: boolean): Promise<true> {
    const fallback = this.activations.recovery(activations, active);
    if (fallback === undefined) {
      sendAPIError(response, 404, "portal_recovery_not_found", head);
      return true;
    }
    try {
      const runtime = recoveryRuntime(await this.delivery.runtime(principal.tenantId, fallback.resolved), active.id, fallback.id);
      response.setHeader("X-VastPlan-Recovery-From", String(active.id));
      response.setHeader("X-VastPlan-Recovery-Revision", String(fallback.id));
      addPreloads(response, runtime);
      sendJSON(response, 200, runtime, head);
    } catch { sendAPIError(response, 409, "portal_recovery_rejected", head); }
    return true;
  }

  private async module(requested: { activeRevision: number; fallbackRevision?: number; digest: string }, activations: readonly PortalActivation[], principal: Principal, request: IncomingMessage, response: ServerResponse): Promise<true> {
    const active = this.activations.currentRevision(activations, principal, requested.activeRevision);
    if (active === undefined) { sendAPIError(response, 404, "portal_revision_not_found", request.method === "HEAD"); return true; }
    if (!this.activations.audienceAllows(active, principal)) { sendAPIError(response, 403, "portal_audience_forbidden", request.method === "HEAD"); return true; }
    let source = active;
    if (requested.fallbackRevision !== undefined) {
      const fallback = this.activations.recovery(activations, active);
      if (fallback === undefined || fallback.id !== requested.fallbackRevision) {
        sendAPIError(response, 404, "portal_recovery_not_found", request.method === "HEAD");
        return true;
      }
      source = fallback;
    }
    try { sendPortalModule(request, response, await this.delivery.object(principal.tenantId, source.resolved, requested.digest)); }
    catch { sendAPIError(response, 404, "portal_module_not_found", request.method === "HEAD"); }
    return true;
  }
}

function writeUpdate(response: ServerResponse, update: PortalUpdate | { portalId: string; activationId: number; mode: string }): void {
  response.write(`event: activation\ndata: ${JSON.stringify(update)}\n\n`);
}

function updateMode(spec: Readonly<Record<string, unknown>>): string {
  return typeof spec.updates === "object" && spec.updates !== null && !Array.isArray(spec.updates)
    ? String((spec.updates as Readonly<Record<string, unknown>>).mode ?? "refresh") : "refresh";
}


function addPreloads(response: ServerResponse, runtime: PortalRuntimeSpec): void {
  const urls = (runtime.modules ?? []).filter((module) => module.deferred !== true).map((module) => module.url);
  for (const graph of runtime.moduleGraphs ?? []) {
    if (graph.deferred === true) continue;
    const entry = graph.nodes.find((node) => node.path === graph.entry);
    if (entry !== undefined) urls.push(entry.url);
  }
  if (urls.length > 0) response.setHeader("Link", urls.map((url) => `<${url}>; rel=preload; as=fetch; crossorigin=use-credentials`));
}
