import type { IncomingMessage } from "node:http";
import type { FrontendServerRenderInput } from "@vastplan/frontend-engine-contract";
import type { Principal } from "../identity/identity-provider";
import type { PortalActivation } from "./portal-activation-catalog";

export function portalRenderInput(active: PortalActivation, principal: Principal, path: string, request: IncomingMessage): FrontendServerRenderInput {
  return Object.freeze({
    generation: active.id,
    tenantId: principal.tenantId,
    portalId: active.portalId,
    path,
    locale: requestLocale(request),
    branding: branding(active.resolved.branding),
  });
}

function requestLocale(request: IncomingMessage): string {
  const preferred = request.headers["accept-language"]?.split(",", 1)[0]?.split(";", 1)[0]?.trim();
  return preferred !== undefined && /^[A-Za-z]{2,3}(?:-[A-Za-z0-9]{2,8})*$/.test(preferred) ? preferred : "zh-CN";
}

function branding(value: unknown): Readonly<Record<string, unknown>> {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Readonly<Record<string, unknown>> : Object.freeze({});
}
