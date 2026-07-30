import type { IncomingMessage, ServerResponse } from "node:http";
import type { PortalComposerOperation } from "../capabilities/portal-composer-operations.generated";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import { sendCapabilityResponse } from "./capability-response";
import { sendAPIError } from "./json-response";
import { requireJSONObject, withRequestJSON } from "./request-json";
import { encodeCapabilityPayload, parseLifecycleAction, parseRevisionID } from "./revision-route-contract";

const basePath = "/v1/portals";
const portalIDPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$/;

/** Host-owned HTTP projection of the single Portal governance aggregate. */
export class PortalRoutes {
  public constructor(private readonly composer: PortalComposerPort) {}

  public async handle(path: string, method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (path !== basePath && !path.startsWith(`${basePath}/`)) return false;
    if (path === basePath) return this.collection(method, principal, request, response, signal);
    const parts = path.slice(basePath.length + 1).split("/");
    const portalId = parts[0];
    if (!portalIDPattern.test(portalId ?? "")) {
      sendAPIError(response, 400, "invalid_portal_id", method === "HEAD");
      return true;
    }
    if (parts[1] === "versions") return this.versions(portalId, parts.slice(2), method, principal, request, response, signal);
    if (parts[1] === "releases") return this.releases(portalId, parts.slice(2), method, principal, request, response, signal);
    sendAPIError(response, 404, "not_found", method === "HEAD");
    return true;
  }

  private async collection(method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<true> {
    if (method === "GET" || method === "HEAD") {
      await this.call("portalGovernance", {}, principal, response, signal, method === "HEAD");
      return true;
    }
    if (method !== "POST") {
      sendAPIError(response, 405, "method_not_allowed");
      return true;
    }
    await withRequestJSON(request, response, async (body) => this.call("createPortal", requireJSONObject(body), principal, response, signal));
    return true;
  }

  private async versions(portalId: string, tail: string[], method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<true> {
    if (tail.length === 0) {
      if (method !== "POST") {
        sendAPIError(response, 405, "method_not_allowed");
        return true;
      }
      await withRequestJSON(request, response, async (body) => {
        const value = requireJSONObject(body);
        await this.call("createPortalVersion", { portalId, configuration: value.configuration ?? value }, principal, response, signal);
      });
      return true;
    }
    const versionId = parseRevisionID(tail[0]);
    if (versionId === undefined || tail.length > 2) {
      sendAPIError(response, 400, "invalid_portal_version", method === "HEAD");
      return true;
    }
    if (tail.length === 1 && method === "PUT") {
      await withRequestJSON(request, response, async (body) => {
        const value = requireJSONObject(body);
        await this.call("updatePortalVersion", { portalId, versionId, configuration: value.configuration ?? value }, principal, response, signal);
      });
      return true;
    }
    if (tail.length === 1 && method === "DELETE") {
      await this.call("deletePortalVersion", { portalId, versionId }, principal, response, signal);
      return true;
    }
    if (tail.length === 2 && tail[1] === "audit" && (method === "GET" || method === "HEAD")) {
      await this.call("audit", { portalId, revisionId: versionId }, principal, response, signal, method === "HEAD");
      return true;
    }
    const action = tail.length === 2 ? parseLifecycleAction(tail[1]) : undefined;
    if (action !== undefined && method === "POST") {
      const operation = `${action}PortalVersion` as PortalComposerOperation;
      await withRequestJSON(request, response, async (body) => {
        const value = requireJSONObject(body);
        const breakGlassReason = value.breakGlassReason;
        await this.call(operation, { portalId, versionId, ...(typeof breakGlassReason === "string" ? { breakGlassReason } : {}) }, principal, response, signal);
      });
      return true;
    }
    sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
    return true;
  }

  private async releases(portalId: string, tail: string[], method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<true> {
    if (tail.length === 0 && method === "POST") {
      await withRequestJSON(request, response, async (body) => this.call("releasePortalVersion", { portalId, release: requireJSONObject(body) }, principal, response, signal));
      return true;
    }
    const releaseId = parseRevisionID(tail[0]);
    if (releaseId !== undefined && tail.length === 2 && tail[1] === "rollback" && method === "POST") {
      await withRequestJSON(request, response, async (body) => {
        const value = requireJSONObject(body);
        await this.call("rollbackPortalRelease", {
          portalId,
          releaseId,
          expectedCurrentReleaseId: value.expectedCurrentReleaseId,
          reason: value.reason,
        }, principal, response, signal);
      });
      return true;
    }
    sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
    return true;
  }

  private async call(operation: PortalComposerOperation, payload: unknown, principal: Principal, response: ServerResponse, signal: AbortSignal, head = false): Promise<void> {
    await sendCapabilityResponse(this.composer, principal, operation, encodeCapabilityPayload(payload), response, signal, head);
  }
}
