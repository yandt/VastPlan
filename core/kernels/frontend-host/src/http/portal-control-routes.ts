import type { IncomingMessage, ServerResponse } from "node:http";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import { PortalDraftRoutes } from "./portal-draft-routes";
import { PortalGovernanceRoutes } from "./portal-governance-routes";

export class PortalControlRoutes {
  private readonly drafts: PortalDraftRoutes;
  private readonly governance: PortalGovernanceRoutes;

  public constructor(composer: PortalComposerPort) {
    this.drafts = new PortalDraftRoutes(composer);
    this.governance = new PortalGovernanceRoutes(composer);
  }

  public async handle(path: string, method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (await this.drafts.handle(path, method, principal, request, response, signal)) return true;
    return this.governance.handle(path, method, principal, request, response, signal);
  }
}
