import type { IncomingMessage, ServerResponse } from "node:http";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import { PortalRoutes } from "./portal-routes";
import { PortalTestReleaseRoutes } from "./portal-test-release-routes";
import { PortalTestTargetRoutes } from "./portal-test-target-routes";

export class PortalControlRoutes {
  private readonly portals: PortalRoutes;
  private readonly testTargets: PortalTestTargetRoutes;
  private readonly testReleases: PortalTestReleaseRoutes;

  public constructor(composer: PortalComposerPort) {
    this.portals = new PortalRoutes(composer);
    this.testTargets = new PortalTestTargetRoutes(composer);
    this.testReleases = new PortalTestReleaseRoutes(composer);
  }

  public async handle(path: string, method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (await this.portals.handle(path, method, principal, request, response, signal)) return true;
    if (await this.testTargets.handle(path, method, principal, request, response, signal)) return true;
    return this.testReleases.handle(path, method, principal, request, response, signal);
  }
}
