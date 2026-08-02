import type { IncomingMessage, ServerResponse } from "node:http";
import type { PortalComposerOperation } from "../capabilities/portal-composer-operations.generated";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import { sendCapabilityResponse } from "./capability-response";
import { sendAPIError } from "./json-response";
import { requireJSONObject, withRequestJSON } from "./request-json";
import { encodeCapabilityPayload, parseRevisionID } from "./revision-route-contract";

const basePath = "/v1/portals";
const portalIDPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$/;
const versionIDPattern = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,159}$/;

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
    if (parts[1] === "working-copy") return this.workingCopy(portalId, parts.slice(2), method, principal, request, response, signal);
    if (parts[1] === "publications") return this.publications(portalId, parts.slice(2), method, principal, request, response, signal);
    if (parts[1] === "releases") return this.releases(portalId, parts.slice(2), method, principal, request, response, signal);
    if (parts[1] === "history") return this.history(portalId, parts.slice(2), method, principal, request, response, signal);
    if (parts[1] === "compare") return this.compare(portalId, parts.slice(2), method, principal, request, response, signal);
    // `/v1/portals/{id}` 也是平台管理等宿主协议的共享父前缀。
    // 只认领 Portal 治理拥有的子资源，未知子资源交回组合路由器继续分发。
    return false;
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

  private async workingCopy(portalId: string, tail: string[], method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<true> {
    if (tail.length !== 0 || (method !== "POST" && method !== "PUT")) {
      sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
      return true;
    }
    if (method === "POST") {
      await withRequestJSON(request, response, async (body) => {
        const value = requireJSONObject(body);
        await this.call("createPortalWorkingCopy", { portalId, configuration: value.configuration ?? value }, principal, response, signal);
      });
      return true;
    }
    await withRequestJSON(request, response, async (body) => this.call("savePortalWorkingCopy", {
      portalId, workingCopy: requireJSONObject(body),
    }, principal, response, signal));
    return true;
  }

  private async publications(portalId: string, tail: string[], method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<true> {
    if (tail.length === 0 && method === "POST") {
      await withRequestJSON(request, response, async (body) => this.call("submitPortalPublication", {
        portalId, publication: requireJSONObject(body),
      }, principal, response, signal));
      return true;
    }
    const publicationId = parseRevisionID(tail[0]);
    if (publicationId === undefined) {
      sendAPIError(response, 400, "invalid_publication", method === "HEAD");
      return true;
    }
    if (tail.length === 2 && tail[1] === "audit" && (method === "GET" || method === "HEAD")) {
      await this.call("audit", { portalId, revisionId: publicationId }, principal, response, signal, method === "HEAD");
      return true;
    }
    if (tail.length === 2 && (tail[1] === "approve" || tail[1] === "publish") && method === "POST") {
      const operation: PortalComposerOperation = tail[1] === "approve" ? "approvePortalPublication" : "publishPortalPublication";
			if (operation === "approvePortalPublication") {
				await withRequestJSON(request, response, async (body) => {
					const value = requireJSONObject(body);
					await this.call(operation, { portalId, publicationId, approval: { review: value.review ?? {} } }, principal, response, signal);
				});
			} else {
				await this.call(operation, { portalId, publicationId }, principal, response, signal);
			}
      return true;
    }
    sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
    return true;
  }

  private async releases(portalId: string, tail: string[], method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<true> {
    if (tail.length === 0 && method === "POST") {
      await withRequestJSON(request, response, async (body) => this.call("releasePortalPublication", { portalId, release: requireJSONObject(body) }, principal, response, signal));
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

  private async history(portalId: string, tail: string[], method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<true> {
    if (tail.length === 0 && (method === "GET" || method === "HEAD")) {
      await this.call("portalVersionHistory", { portalId }, principal, response, signal, method === "HEAD");
      return true;
    }
    const versionId = decodeVersionID(tail[0]);
    if (!versionIDPattern.test(versionId ?? "")) {
      sendAPIError(response, 400, "invalid_version_id", method === "HEAD");
      return true;
    }
    if (tail.length === 1 && (method === "GET" || method === "HEAD")) {
      await this.call("readPortalVersion", { portalId, versionId }, principal, response, signal, method === "HEAD");
      return true;
    }
    if (tail.length === 2 && tail[1] === "restore" && method === "POST") {
      await withRequestJSON(request, response, async (body) => this.call("restorePortalVersion", {
        portalId, restore: { ...requireJSONObject(body), versionId },
      }, principal, response, signal));
      return true;
    }
    sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
    return true;
  }

  private async compare(portalId: string, tail: string[], method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<true> {
    if (tail.length !== 0 || (method !== "GET" && method !== "HEAD")) {
      sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
      return true;
    }
    const url = new URL(request.url ?? "", "https://portal.invalid");
    const leftVersionId = url.searchParams.get("left") ?? "";
    const rightVersionId = url.searchParams.get("right") ?? "";
    if (!versionIDPattern.test(leftVersionId) || !versionIDPattern.test(rightVersionId)) {
      sendAPIError(response, 400, "invalid_version_id", method === "HEAD");
      return true;
    }
    await this.call("comparePortalVersions", { portalId, leftVersionId, rightVersionId }, principal, response, signal, method === "HEAD");
    return true;
  }

  private async call(operation: PortalComposerOperation, payload: unknown, principal: Principal, response: ServerResponse, signal: AbortSignal, head = false): Promise<void> {
    await sendCapabilityResponse(this.composer, principal, operation, encodeCapabilityPayload(payload), response, signal, head);
  }
}

function decodeVersionID(value: string | undefined): string {
  try { return decodeURIComponent(value ?? ""); }
  catch { return ""; }
}
