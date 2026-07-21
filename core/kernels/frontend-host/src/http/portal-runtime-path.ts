const digestPattern = /^[0-9a-f]{64}$/;
const objectNamePattern = /^([0-9a-f]{64})\.(js|css|json|wasm|bin)$/;

export interface ModuleRequest { readonly activeRevision: number; readonly fallbackRevision?: number; readonly digest: string }

export function parsePortalModulePath(path: string): ModuleRequest | undefined {
  const regular = path.match(/^\/v1\/portal-modules\/([1-9][0-9]*)\/([^/]+)$/);
  if (regular !== null) {
    const digest = objectDigest(regular[2]!);
    const activeRevision = Number(regular[1]);
    return digest === undefined || !Number.isSafeInteger(activeRevision) ? undefined : { activeRevision, digest };
  }
  const recovery = path.match(/^\/v1\/portal-recovery-modules\/([1-9][0-9]*)\/([1-9][0-9]*)\/([^/]+)$/);
  if (recovery === null) return undefined;
  const activeRevision = Number(recovery[1]);
  const fallbackRevision = Number(recovery[2]);
  const digest = objectDigest(recovery[3]!);
  if (digest === undefined || !Number.isSafeInteger(activeRevision) || !Number.isSafeInteger(fallbackRevision) || activeRevision === fallbackRevision) return undefined;
  return { activeRevision, fallbackRevision, digest };
}

export function requestedPortalPath(url: string | undefined): string | undefined {
  const parsed = new URL(url ?? "/", "https://portal.invalid");
  if ([...parsed.searchParams.keys()].some((key) => key !== "path") || parsed.searchParams.getAll("path").length > 1) return undefined;
  const path = parsed.searchParams.get("path") ?? "/";
  return path.startsWith("/") ? path : undefined;
}

export function portalUpdateQuery(url: string | undefined): { path: string; revision: number } | undefined {
  const parsed = new URL(url ?? "/", "https://portal.invalid");
  if ([...parsed.searchParams.keys()].some((key) => key !== "path" && key !== "revision")
    || parsed.searchParams.getAll("path").length > 1 || parsed.searchParams.getAll("revision").length !== 1) return undefined;
  const path = parsed.searchParams.get("path") ?? "/";
  const rawRevision = parsed.searchParams.get("revision") ?? "";
  if (!path.startsWith("/") || !/^[1-9][0-9]*$/.test(rawRevision)) return undefined;
  const revision = Number(rawRevision);
  return Number.isSafeInteger(revision) ? { path, revision } : undefined;
}

function objectDigest(name: string): string | undefined {
  const match = name.match(objectNamePattern);
  return match !== null && digestPattern.test(match[1]!) ? match[1] : undefined;
}
