import type { IncomingMessage } from "node:http";
import type { FrontendServerRenderResult } from "@vastplan/frontend-engine-contract";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { IdentityProvider } from "../identity/identity-provider";
import { SessionRejectedError } from "../identity/identity-provider";
import { requestHostname } from "../http/platform-route-contract";
import type { ServerGenerationManager } from "../workers/server-generation-manager";
import { PortalActivationCatalog } from "./portal-activation-catalog";
import { portalRenderInput } from "./portal-render-input";

export interface PortalSSRPort {
  render(request: IncomingMessage, path: string): Promise<FrontendServerRenderResult | undefined>;
}

export class PortalSSRCoordinator implements PortalSSRPort {
  private readonly activations: PortalActivationCatalog;

  public constructor(composer: PortalComposerPort, private readonly identity: IdentityProvider, private readonly generations: ServerGenerationManager) {
    this.activations = new PortalActivationCatalog(composer);
  }

  public async render(request: IncomingMessage, path: string): Promise<FrontendServerRenderResult | undefined> {
    let principal;
    try { principal = await this.identity.authenticate(request); }
    catch (error) {
      if (error instanceof SessionRejectedError) return undefined;
      throw error;
    }
    const activations = await this.activations.list(principal);
    const active = this.activations.selectCurrent(activations, principal, path, requestHostname(request));
    if (active === undefined || !this.activations.audienceAllows(active, principal)) return undefined;
    return this.generations.renderActive(principal.tenantId, active.resolved, portalRenderInput(active, principal, path, request));
  }
}
