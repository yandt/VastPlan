import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import type { PortalSpec } from "./portal-runtime-contract";

export interface PortalActivation {
  readonly id: number;
  readonly tenantId: string;
  readonly portalId: string;
  readonly status: "Current" | "Superseded";
  readonly resolved: PortalSpec;
}

export class PortalActivationCatalog {
  private readonly emptyPayload = new TextEncoder().encode("{}");

  public constructor(private readonly composer: PortalComposerPort) {}

  public async list(principal: Principal, signal?: AbortSignal): Promise<readonly PortalActivation[]> {
    const raw = await this.composer.call(principal, "listActivations", this.emptyPayload, signal);
    let value: unknown;
    try { value = JSON.parse(new TextDecoder().decode(raw)) as unknown; }
    catch { throw new Error("Portal Activation 响应 JSON 无效"); }
    if (!Array.isArray(value)) throw new Error("Portal Activation 响应必须是数组");
    return Object.freeze(value.map(parseActivation).filter((activation): activation is PortalActivation => activation !== undefined));
  }

  public selectCurrent(activations: readonly PortalActivation[], principal: Principal, requestedPath: string, host: string): PortalActivation | undefined {
    if (!requestedPath.startsWith("/")) return undefined;
    return activations
      .filter((activation) => current(activation, principal.tenantId) && routeMatches(activation.resolved.route, requestedPath) && domainAllows(activation.resolved.domains, host))
      .sort((left, right) => right.resolved.route.length - left.resolved.route.length)[0];
  }

  public currentRevision(activations: readonly PortalActivation[], principal: Principal, revision: number): PortalActivation | undefined {
    return activations.find((activation) => activation.id === revision && current(activation, principal.tenantId));
  }

  public recovery(activations: readonly PortalActivation[], active: PortalActivation): PortalActivation | undefined {
    return activations
      .filter((activation) => activation.tenantId === active.tenantId && activation.portalId === active.portalId
        && activation.status === "Superseded" && activation.id > 0 && activation.resolved.revision === activation.id)
      .sort((left, right) => right.id - left.id)[0];
  }

  public audienceAllows(activation: PortalActivation, principal: Principal): boolean {
    const audience = activation.resolved.audience ?? [];
    return principal.system === true || audience.length === 0 || audience.some((role) => principal.roles.includes(role));
  }
}

function parseActivation(value: unknown): PortalActivation | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error("Portal Activation 条目无效");
  const activation = value as Readonly<Record<string, unknown>>;
  if (!positiveInteger(activation.id) || typeof activation.tenantId !== "string" || typeof activation.portalId !== "string" || typeof activation.status !== "string") {
    throw new Error("Portal Activation 条目无效");
  }
  if (activation.status !== "Current" && activation.status !== "Superseded") return undefined;
  if (typeof activation.resolved !== "object" || activation.resolved === null || Array.isArray(activation.resolved)) throw new Error("Portal Activation 解析锁无效");
  const resolved = activation.resolved as PortalSpec;
  if (!positiveInteger(resolved.revision) || !nonempty(resolved.id) || !nonempty(resolved.tenantId) || !nonempty(resolved.route) || !resolved.route.startsWith("/")
    || !optionalStringArray(resolved.domains) || !optionalStringArray(resolved.audience)
    || resolved.id !== activation.portalId || resolved.tenantId !== activation.tenantId) throw new Error("Portal Activation 解析锁无效");
  return Object.freeze({ id: activation.id, tenantId: activation.tenantId, portalId: activation.portalId, status: activation.status, resolved });
}

function current(activation: PortalActivation, tenantId: string): boolean {
  return activation.status === "Current" && activation.tenantId === tenantId && activation.id === activation.resolved.revision;
}

function routeMatches(root: string, requested: string): boolean {
  if (root === "/") return true;
  const normalized = root.endsWith("/") ? root.slice(0, -1) : root;
  return requested === normalized || requested.startsWith(`${normalized}/`);
}

function domainAllows(domains: readonly string[] | undefined, host: string): boolean {
  return domains === undefined || domains.length === 0 || domains.some((domain) => domain.toLowerCase() === host.toLowerCase());
}

function positiveInteger(value: unknown): value is number { return Number.isSafeInteger(value) && (value as number) > 0; }
function nonempty(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
function optionalStringArray(value: unknown): value is readonly string[] | undefined {
  return value === undefined || (Array.isArray(value) && value.every(nonempty));
}
