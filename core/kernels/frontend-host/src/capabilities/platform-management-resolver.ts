import type { Principal } from "../identity/identity-provider";
import type { PortalComposerPort } from "./portal-composer-client";
import { managementBindingDigest, parseManagementBinding, type ManagedService } from "./management-binding";

export interface PlatformManagementTarget { portalId: string; service: ManagedService }

export class ManagementResolutionError extends Error {
  public constructor(public readonly code: string) { super(code); this.name = "ManagementResolutionError"; }
}

export class PlatformManagementResolver {
  private readonly emptyPayload = new TextEncoder().encode("{}");

  public constructor(private readonly composer: PortalComposerPort) {}

  public async resolve(principal: Principal, portalId: string, serviceId: string, requestHost: string, signal?: AbortSignal): Promise<PlatformManagementTarget> {
    const raw = await this.composer.call(principal, "listActivations", this.emptyPayload, signal);
    let value: unknown;
    try { value = JSON.parse(new TextDecoder().decode(raw)) as unknown; }
    catch { throw new ManagementResolutionError("portal_activation_invalid"); }
    if (!Array.isArray(value)) throw new ManagementResolutionError("portal_activation_invalid");
    const activation = value.find((candidate) => currentPortal(candidate, principal.tenantId, portalId, requestHost));
    if (activation === undefined) throw new ManagementResolutionError("portal_not_found");
    const resolved = activation as Record<string, unknown>;
    const spec = resolved.resolved as Record<string, unknown>;
    if (!audienceAllows(spec.audience, principal)) throw new ManagementResolutionError("portal_audience_forbidden");
    let binding;
    try { binding = parseManagementBinding(spec.management); }
    catch { throw new ManagementResolutionError("portal_management_binding_rejected"); }
    const resolution = typeof spec.resolution === "object" && spec.resolution !== null ? spec.resolution as Record<string, unknown> : {};
    if (binding.tenantId !== principal.tenantId || binding.portalId !== portalId || !sameRef(binding.platformProfile, resolution.platformProfile) || managementBindingDigest(binding) !== resolution.managementBindingDigest) {
      throw new ManagementResolutionError("portal_management_binding_rejected");
    }
    const service = binding.services.find((candidate) => candidate.id === serviceId);
    if (service === undefined) throw new ManagementResolutionError("managed_service_not_found");
    return Object.freeze({ portalId, service });
  }
}

function currentPortal(value: unknown, tenantId: string, portalId: string, host: string): boolean {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  const activation = value as Record<string, unknown>;
  if (activation.status !== "Current" || activation.tenantId !== tenantId || activation.portalId !== portalId) return false;
  if (typeof activation.resolved !== "object" || activation.resolved === null) return false;
  const spec = activation.resolved as Record<string, unknown>;
  return spec.id === portalId && spec.tenantId === tenantId && domainAllows(spec.domains, host);
}

function domainAllows(value: unknown, host: string): boolean {
  if (value === undefined || (Array.isArray(value) && value.length === 0)) return true;
  return Array.isArray(value) && value.some((domain) => typeof domain === "string" && domain.toLowerCase() === host.toLowerCase());
}

function audienceAllows(value: unknown, principal: Principal): boolean {
  if (principal.system === true || value === undefined || (Array.isArray(value) && value.length === 0)) return true;
  return Array.isArray(value) && value.some((role) => typeof role === "string" && principal.roles.includes(role));
}

function sameRef(left: { id: string; revision: number; digest: string }, value: unknown): boolean {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  const right = value as Record<string, unknown>;
  return left.id === right.id && left.revision === right.revision && left.digest === right.digest;
}
