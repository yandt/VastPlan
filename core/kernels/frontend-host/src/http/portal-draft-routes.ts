import type { IncomingMessage, ServerResponse } from "node:http";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { sendComposerResponse } from "./composer-response";
import { readRequestJSON, RequestJSONError, requireJSONObject } from "./request-json";

const basePath = "/v1/portal-drafts";
const encoder = new TextEncoder();

export class PortalDraftRoutes {
  public constructor(private readonly composer: PortalComposerPort) {}

  public async handle(path: string, method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (path !== basePath && !path.startsWith(`${basePath}/`)) return false;
    if (path === basePath) return this.collection(method, principal, request, response, signal);
    const parts = path.slice(basePath.length + 1).split("/");
    if (parts.length < 1 || parts.length > 2 || !parts[0]) {
      sendAPIError(response, 404, "not_found", method === "HEAD");
      return true;
    }
    const revisionID = parseRevisionID(parts[0]);
    if (revisionID === undefined) {
      sendAPIError(response, 400, "invalid_revision", method === "HEAD");
      return true;
    }
    return this.revision(method, parts[1], revisionID, principal, request, response, signal);
  }

  private async collection(method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<true> {
    if (method === "GET" || method === "HEAD") {
      await sendComposerResponse(this.composer, principal, "list", encoder.encode("{}"), response, signal, method === "HEAD");
    } else if (method === "POST") {
      await this.withRequestJSON(request, response, async (body) => sendComposerResponse(this.composer, principal, "createDraft", encoder.encode(JSON.stringify(body)), response, signal));
    } else sendAPIError(response, 405, "method_not_allowed");
    return true;
  }

  private async revision(method: string, action: string | undefined, revisionID: number, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<true> {
    if (action === undefined && method === "PUT") {
      await this.withRequestJSON(request, response, async (composition) => sendComposerResponse(this.composer, principal, "updateDraft", encode({ revisionId: revisionID, composition }), response, signal));
      return true;
    }
    if (action === "audit" && (method === "GET" || method === "HEAD")) {
      await sendComposerResponse(this.composer, principal, "audit", encode({ revisionId: revisionID }), response, signal, method === "HEAD");
      return true;
    }
    if ((action === "submit" || action === "approve") && method === "POST") {
      await this.withRequestJSON(request, response, async () => sendComposerResponse(this.composer, principal, action, encode({ revisionId: revisionID }), response, signal));
      return true;
    }
    if (action === "publish" && method === "POST") {
      await this.withRequestJSON(request, response, async (body) => sendComposerResponse(this.composer, principal, "publish", encode({ ...requireJSONObject(body), revisionId: revisionID }), response, signal));
      return true;
    }
    sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
    return true;
  }

  private async withRequestJSON(request: IncomingMessage, response: ServerResponse, action: (value: unknown) => Promise<void>): Promise<void> {
    try { await action(await readRequestJSON(request)); }
    catch (error) {
      if (error instanceof RequestJSONError) sendAPIError(response, 400, "invalid_json");
      else throw error;
    }
  }
}

function parseRevisionID(value: string): number | undefined {
  if (!/^[0-9]+$/.test(value)) return undefined;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : undefined;
}

function encode(value: unknown): Uint8Array { return encoder.encode(JSON.stringify(value)); }
