import { createHash } from "node:crypto";

const managementName = /^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$/;

export interface ManagementRef { id: string; revision: number; digest: string }
export interface CapabilityGrant { capability: string; read: readonly string[]; write: readonly string[] }
export interface ManagedService {
  id: string;
  label?: string;
  logicalService: string;
  routingDomain: string;
  capabilities: readonly CapabilityGrant[];
}
export interface ManagementBinding {
  tenantId: string;
  portalId: string;
  platformProfile: ManagementRef;
  services: readonly ManagedService[];
}

export function parseManagementBinding(value: unknown): ManagementBinding {
  const binding = record(value, "Management Binding");
  const tenantId = requiredString(binding.tenantId, "tenantId");
  const portalId = named(binding.portalId, "portalId");
  const profile = record(binding.platformProfile, "platformProfile");
  const platformProfile = Object.freeze({
    id: requiredString(profile.id, "platformProfile.id"),
    revision: positiveInteger(profile.revision, "platformProfile.revision"),
    digest: digest(profile.digest, "platformProfile.digest"),
  });
  if (!Array.isArray(binding.services) || binding.services.length === 0) throw new Error("Management Binding 至少包含一个服务");
  const serviceIDs = new Set<string>();
  const serviceTargets = new Set<string>();
  const services = binding.services.map((value) => {
    const service = record(value, "managed service");
    const id = named(service.id, "service.id");
    const logicalService = named(service.logicalService, "service.logicalService");
    const routingDomain = named(service.routingDomain, "service.routingDomain");
    if (serviceIDs.has(id) || serviceTargets.has(`${logicalService}\0${routingDomain}`)) throw new Error("Management Binding 服务身份或路由重复");
    serviceIDs.add(id); serviceTargets.add(`${logicalService}\0${routingDomain}`);
    if (!Array.isArray(service.capabilities) || service.capabilities.length === 0) throw new Error(`受管服务 ${id} 没有 capability grant`);
    const seenCapabilities = new Set<string>();
    const capabilities = service.capabilities.map((value) => parseGrant(value, seenCapabilities));
    const label = optionalString(service.label);
    return Object.freeze({ id, ...(label === undefined ? {} : { label }), logicalService, routingDomain, capabilities: Object.freeze(capabilities) });
  });
  return Object.freeze({ tenantId, portalId, platformProfile, services: Object.freeze(services) });
}

export function managementBindingDigest(binding: ManagementBinding): string {
  const canonical = {
    tenantId: binding.tenantId,
    portalId: binding.portalId,
    platformProfile: { id: binding.platformProfile.id, revision: binding.platformProfile.revision, digest: binding.platformProfile.digest },
    services: binding.services.map((service) => ({
      id: service.id,
      ...(service.label === undefined ? {} : { label: service.label }),
      logicalService: service.logicalService,
      routingDomain: service.routingDomain,
      capabilities: service.capabilities.map((grant) => ({
        capability: grant.capability,
        ...(grant.read.length === 0 ? {} : { read: [...grant.read] }),
        ...(grant.write.length === 0 ? {} : { write: [...grant.write] }),
      })),
    })),
  };
  return createHash("sha256").update(JSON.stringify(canonical)).digest("hex");
}

export function managementAllows(service: ManagedService, capability: string, operation: string, write: boolean): boolean {
  const grant = service.capabilities.find((candidate) => candidate.capability === capability);
  return grant !== undefined && (write ? grant.write : grant.read).includes(operation);
}

function parseGrant(value: unknown, seen: Set<string>): CapabilityGrant {
  const grant = record(value, "capability grant");
  const capability = named(grant.capability, "grant.capability");
  if (seen.has(capability)) throw new Error(`capability grant 重复: ${capability}`);
  seen.add(capability);
  const read = stringList(grant.read, "grant.read");
  const write = stringList(grant.write, "grant.write");
  const operations = [...read, ...write];
  if (operations.length === 0 || new Set(operations).size !== operations.length || operations.some((operation) => !managementName.test(operation))) throw new Error(`capability grant operation 无效: ${capability}`);
  return Object.freeze({ capability, read: Object.freeze(read), write: Object.freeze(write) });
}

function record(value: unknown, name: string): Record<string, unknown> { if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error(`${name} 不是对象`); return value as Record<string, unknown>; }
function requiredString(value: unknown, name: string): string { if (typeof value !== "string" || value === "") throw new Error(`${name} 无效`); return value; }
function named(value: unknown, name: string): string { const result = requiredString(value, name); if (!managementName.test(result)) throw new Error(`${name} 格式无效`); return result; }
function positiveInteger(value: unknown, name: string): number { if (!Number.isSafeInteger(value) || Number(value) <= 0) throw new Error(`${name} 无效`); return Number(value); }
function digest(value: unknown, name: string): string { const result = requiredString(value, name); if (!/^[a-f0-9]{64}$/.test(result)) throw new Error(`${name} 无效`); return result; }
function optionalString(value: unknown): string | undefined { return typeof value === "string" && value !== "" ? value : undefined; }
function stringList(value: unknown, name: string): string[] { if (value === undefined) return []; if (!Array.isArray(value) || value.some((item) => typeof item !== "string" || item === "")) throw new Error(`${name} 无效`); return [...value] as string[]; }
