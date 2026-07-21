import type { IncomingMessage } from "node:http";
import type { FrontendServerRenderResult } from "@vastplan/frontend-engine-contract";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { IdentityProvider } from "../identity/identity-provider";
import { SessionRejectedError } from "../identity/identity-provider";
import { requestHostname } from "../http/platform-route-contract";
import type { ServerGenerationManager } from "../workers/server-generation-manager";
import { PortalActivationCatalog } from "./portal-activation-catalog";

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
    return this.generations.render(principal.tenantId, active.resolved, {
      generation: active.id,
      tenantId: principal.tenantId,
      portalId: active.portalId,
      path,
      locale: requestLocale(request),
      branding: branding(active.resolved.branding),
    });
  }
}

function requestLocale(request: IncomingMessage): string {
  const preferred = request.headers["accept-language"]?.split(",", 1)[0]?.split(";", 1)[0]?.trim();
  return preferred !== undefined && /^[A-Za-z]{2,3}(?:-[A-Za-z0-9]{2,8})*$/.test(preferred) ? preferred : "zh-CN";
}

function branding(value: unknown): Readonly<Record<string, unknown>> {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Readonly<Record<string, unknown>> : Object.freeze({});
}
