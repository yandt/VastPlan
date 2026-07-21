import type { IncomingMessage, ServerResponse } from "node:http";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import { sendComposerResponse } from "./composer-response";
import { sendAPIError } from "./json-response";
import { PortalActivationRoutes } from "./portal-activation-routes";
import { PortalBindingRoutes } from "./portal-binding-routes";
import { PortalProfileRoutes } from "./portal-profile-routes";
import { encodeCapabilityPayload } from "./revision-route-contract";

const basePath = "/v1/portal-governance";

export class PortalGovernanceRoutes {
  private readonly profiles: PortalProfileRoutes;
  private readonly bindings: PortalBindingRoutes;
  private readonly activations: PortalActivationRoutes;

  public constructor(private readonly composer: PortalComposerPort) {
    this.profiles = new PortalProfileRoutes(composer);
    this.bindings = new PortalBindingRoutes(composer);
    this.activations = new PortalActivationRoutes(composer);
  }

  public async handle(path: string, method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (path !== basePath && !path.startsWith(`${basePath}/`)) return false;
    if (path === basePath) {
      if (method !== "GET" && method !== "HEAD") sendAPIError(response, 405, "method_not_allowed");
      else await sendComposerResponse(this.composer, principal, "governance", encodeCapabilityPayload({}), response, signal, method === "HEAD");
      return true;
    }
    if (await this.profiles.handle(path, method, principal, request, response, signal)) return true;
    if (await this.bindings.handle(path, method, principal, request, response, signal)) return true;
    if (await this.activations.handle(path, method, principal, request, response, signal)) return true;
    sendAPIError(response, 404, "not_found", method === "HEAD");
    return true;
  }
}
