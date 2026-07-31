import type { CollectionPreference, PortalPreference, PortalPreferenceScope, PortalPreferenceValues } from "@vastplan/frontend-engine-contract";
import type { ModuleFetcher, PortalRuntimeSpec } from "./module-loader";

const idPattern = /^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$/;
const defaultEndpoint = "/v1/portal-preference";

export class PortalPreferenceSession {
  private remote?: PortalPreference;
  private cache?: PortalPreference;
  private collectionWriteQueue: Promise<void> = Promise.resolve();

  private constructor(
    private readonly fetcher: ModuleFetcher,
    private readonly endpoint: string,
    private readonly pathname: string,
    private scope: PortalPreferenceScope,
    private portal: PortalRuntimeSpec["portal"],
  ) {}

  public static async open(fetcher: ModuleFetcher, pathname: string, portal: PortalRuntimeSpec["portal"], endpoint = defaultEndpoint): Promise<PortalPreferenceSession> {
    const scope = preferenceScope(portal);
    const session = new PortalPreferenceSession(fetcher, endpoint, pathname, scope, portal);
    session.cache = readCachedPreference(portal, scope);
    await session.refresh(portal);
    return session;
  }

  public readCollection(collectionID: string): CollectionPreference | undefined {
    if (validID(collectionID) === undefined) return undefined;
    return preferredValues(this.remote, this.cache).collections?.[collectionID];
  }

  public writeCollection(collectionID: string, preference: CollectionPreference): Promise<CollectionPreference> {
    if (validID(collectionID) === undefined) return Promise.reject(new PortalPreferenceUnavailable("CollectionPreference ID 无效"));
    const run = this.collectionWriteQueue.then(() => this.writeCollectionNow(collectionID, preference));
    this.collectionWriteQueue = run.then(() => undefined, () => undefined);
    return run;
  }

  private async commit(values: PortalPreferenceValues, portal: PortalRuntimeSpec["portal"]): Promise<PortalPreference> {
    const scope = preferenceScope(portal);
    if (!sameScope(scope, this.scope)) {
      this.scope = scope;
      this.remote = undefined;
      this.cache = readCachedPreference(portal, scope);
    }
    const response = await this.fetcher(`${this.endpoint}?path=${encodeURIComponent(this.pathname)}`, {
      method: "PUT", credentials: "same-origin", cache: "no-store",
      headers: { "Content-Type": "application/json", "X-VastPlan-CSRF": await csrfToken(this.fetcher) },
      body: JSON.stringify({ expectedRevision: this.remote?.revision ?? 0, values }),
    });
    if (response.status === 409) throw new PortalPreferenceConflict();
    if (!response.ok) throw new PortalPreferenceUnavailable(`PortalPreference 保存失败 (${response.status})`);
    const saved = parsePreference(await response.json(), scope);
    this.remote = saved;
    this.cache = saved;
    writeCachedPreference(portal, saved);
    return saved;
  }

  public async refresh(portal: PortalRuntimeSpec["portal"]): Promise<void> {
    this.portal = portal;
    const scope = preferenceScope(portal);
    if (!sameScope(scope, this.scope)) {
      this.scope = scope;
      this.remote = undefined;
      this.cache = readCachedPreference(portal, scope);
    }
    try {
      const response = await this.fetcher(`${this.endpoint}?path=${encodeURIComponent(this.pathname)}`, { credentials: "same-origin", cache: "no-store" });
      if (!response.ok) return;
      const preference = parsePreference(await response.json(), scope);
      this.remote = preference;
      if (preference.revision > 0) {
        this.cache = preference;
        writeCachedPreference(portal, preference);
      }
    } catch { /* offline/unavailable: validated local cache remains a startup fallback */ }
  }

  private async writeCollectionNow(collectionID: string, preference: CollectionPreference): Promise<CollectionPreference> {
    const sanitized = sanitizeCollectionPreference(preference);
    for (let attempt = 0; attempt < 2; attempt += 1) {
      const current = preferredValues(this.remote, this.cache);
      try {
        const saved = await this.commit({ collections: Object.freeze({ ...current.collections, [collectionID]: sanitized }) }, this.portal);
        return saved.values.collections?.[collectionID] ?? sanitized;
      } catch (error) {
        if (!(error instanceof PortalPreferenceConflict) || attempt > 0) throw error;
        await this.refresh(this.portal);
      }
    }
    throw new PortalPreferenceConflict();
  }
}

export class PortalPreferenceConflict extends Error {
  public constructor() { super("PortalPreference 已在其他设备更新"); this.name = "PortalPreferenceConflict"; }
}
export class PortalPreferenceUnavailable extends Error {
  public constructor(message: string) { super(message); this.name = "PortalPreferenceUnavailable"; }
}

function preferenceScope(portal: PortalRuntimeSpec["portal"]): PortalPreferenceScope {
  const projected = (portal as PortalRuntimeSpec["portal"] & { preferenceScope?: unknown }).preferenceScope;
  if (projected !== undefined) return parseProjectedPreferenceScope(projected, portal.id);
  return Object.freeze({
    portalId: portal.id,
    workbench: Object.freeze({ id: portal.workbench.id, contractMajor: contractMajor(portal.workbench.uiContract) }),
  });
}

function parseProjectedPreferenceScope(value: unknown, portalID: string): PortalPreferenceScope {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new PortalPreferenceUnavailable("PortalPreference 投影 scope 无效");
  const record = value as Readonly<Record<string, unknown>>;
  if (Object.keys(record).some((key) => !["portalId", "workbench", "renderer", "shell"].includes(key)) || validID(record.portalId) !== portalID) {
    throw new PortalPreferenceUnavailable("PortalPreference 投影 portalId 无效");
  }
  // UI Contract 8.2 and earlier projected Renderer/Shell into this scope.
  // Validate those legacy fields when present, then deliberately discard them:
  // appearance is local-only in 8.3, while rolling Host/asset restarts remain safe.
  if (record.renderer !== undefined) parseProjectedCatalogScope(record.renderer);
  if (record.shell !== undefined) parseProjectedCatalogScope(record.shell);
  return Object.freeze({
    portalId: portalID,
    workbench: parseProjectedCatalogScope(record.workbench),
  });
}

function parseProjectedCatalogScope(value: unknown): PortalPreferenceScope["workbench"] {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new PortalPreferenceUnavailable("PortalPreference 投影 catalog scope 无效");
  const record = value as Readonly<Record<string, unknown>>;
  const id = validID(record.id);
  if (Object.keys(record).some((key) => key !== "id" && key !== "contractMajor") || id === undefined || !Number.isSafeInteger(record.contractMajor) || Number(record.contractMajor) < 1 || Number(record.contractMajor) > 65_535) {
    throw new PortalPreferenceUnavailable("PortalPreference 投影 catalog scope 无效");
  }
  return Object.freeze({ id, contractMajor: Number(record.contractMajor) });
}

function preferredValues(remote: PortalPreference | undefined, cache: PortalPreference | undefined): PortalPreferenceValues {
  return remote !== undefined && remote.revision > 0 ? remote.values : cache?.values ?? {};
}

function parsePreference(value: unknown, expectedScope: PortalPreferenceScope): PortalPreference {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new PortalPreferenceUnavailable("PortalPreference 响应无效");
  const record = value as Readonly<Record<string, unknown>>;
  if (!Number.isSafeInteger(record.revision) || Number(record.revision) < 0 || !sameScope(record.scope, expectedScope)) throw new PortalPreferenceUnavailable("PortalPreference scope 或 revision 无效");
  const values = sanitizeValues(record.values);
  return Object.freeze({ revision: Number(record.revision), scope: expectedScope, values, ...(typeof record.updatedAt === "string" ? { updatedAt: record.updatedAt } : {}) });
}

function sanitizeValues(value: unknown): PortalPreferenceValues {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return {};
  const record = value as Readonly<Record<string, unknown>>;
  const collections: Record<string, { columns?: readonly string[]; hiddenColumns?: readonly string[]; density?: "compact" | "standard" | "comfortable"; pageSize?: number }> = {};
  if (typeof record.collections === "object" && record.collections !== null && !Array.isArray(record.collections)) {
    for (const [collectionID, raw] of Object.entries(record.collections as Readonly<Record<string, unknown>>).slice(0, 128)) {
      if (validID(collectionID) === undefined || typeof raw !== "object" || raw === null || Array.isArray(raw)) continue;
      const item = raw as Readonly<Record<string, unknown>>;
      const columns = Array.isArray(item.columns) ? [...new Set(item.columns.map(validID).filter((entry): entry is string => entry !== undefined))].slice(0, 128) : undefined;
      const hiddenColumns = Array.isArray(item.hiddenColumns) ? [...new Set(item.hiddenColumns.map(validID).filter((entry): entry is string => entry !== undefined))].slice(0, 128) : undefined;
      const density = item.density === "compact" || item.density === "standard" || item.density === "comfortable" ? item.density : undefined;
      const pageSize = Number.isSafeInteger(item.pageSize) && Number(item.pageSize) > 0 && Number(item.pageSize) <= 1000 ? Number(item.pageSize) : undefined;
      collections[collectionID] = Object.freeze({ ...(columns === undefined ? {} : { columns: Object.freeze(columns) }), ...(hiddenColumns === undefined ? {} : { hiddenColumns: Object.freeze(hiddenColumns) }), ...(density === undefined ? {} : { density }), ...(pageSize === undefined ? {} : { pageSize }) });
    }
  }
  return Object.freeze({ ...(Object.keys(collections).length === 0 ? {} : { collections: Object.freeze(collections) }) });
}

function sanitizeCollectionPreference(value: CollectionPreference): CollectionPreference {
  const sanitized = sanitizeValues({ collections: { collection: value } }).collections?.collection;
  if (sanitized === undefined) throw new PortalPreferenceUnavailable("CollectionPreference 无效");
  return sanitized;
}

function readCachedPreference(portal: PortalRuntimeSpec["portal"], scope: PortalPreferenceScope): PortalPreference | undefined { return readStorage(cacheKey(portal, scope), scope) as PortalPreference | undefined; }
function writeCachedPreference(portal: PortalRuntimeSpec["portal"], preference: PortalPreference): void { writeStorage(cacheKey(portal, preference.scope), preference); }
function readStorage(key: string, scope: PortalPreferenceScope): unknown {
  try {
    const raw = globalThis.localStorage?.getItem(key);
    if (raw === null || raw === undefined || new TextEncoder().encode(raw).byteLength > 256 << 10) return undefined;
    const value = JSON.parse(raw) as unknown;
    if (typeof value !== "object" || value === null || Array.isArray(value) || !sameScope((value as { scope?: unknown }).scope, scope)) return undefined;
    if ("values" in value) return parsePreference(value, scope);
    return value;
  } catch { return undefined; }
}
function writeStorage(key: string, value: unknown): void { try { globalThis.localStorage?.setItem(key, JSON.stringify(value)); } catch { /* privacy mode */ } }
function cacheKey(portal: PortalRuntimeSpec["portal"], scope: PortalPreferenceScope): string { return `vastplan.portal-preference.${portal.tenantId}.${scopeKey(scope)}`; }
function scopeKey(scope: PortalPreferenceScope): string { return [scope.portalId, scope.workbench.id, scope.workbench.contractMajor].join("."); }
function sameScope(left: unknown, right: PortalPreferenceScope): boolean {
  try { return JSON.stringify(parseProjectedPreferenceScope(left, right.portalId)) === JSON.stringify(right); }
  catch { return false; }
}
function contractMajor(value: string): number { const match = value.trim().match(/^(?:\^|~|>=?)?\s*([1-9][0-9]{0,4})(?:\.|$)/); if (match === null) throw new PortalPreferenceUnavailable("UI contract major 无效"); return Number(match[1]); }
function validID(value: unknown): string | undefined { return typeof value === "string" && idPattern.test(value) ? value : undefined; }

let csrfPromise: Promise<string> | undefined;
async function csrfToken(fetcher: ModuleFetcher): Promise<string> {
  csrfPromise ??= fetcher("/v1/csrf", { credentials: "same-origin", cache: "no-store" }).then(async (response) => {
    if (!response.ok) throw new PortalPreferenceUnavailable(`CSRF 获取失败 (${response.status})`);
    const value = await response.json() as { token?: unknown };
    if (typeof value.token !== "string" || value.token.length < 32) throw new PortalPreferenceUnavailable("CSRF 响应无效");
    return value.token;
  }).catch((error) => { csrfPromise = undefined; throw error; });
  return csrfPromise;
}
