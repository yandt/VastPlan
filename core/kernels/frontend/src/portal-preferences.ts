import type { CollectionPreference, PortalPreference, PortalPreferenceScope, PortalPreferenceValues } from "@vastplan/frontend-engine-contract";
import type { ModuleFetcher, PortalRuntimeSpec } from "./module-loader";

const idPattern = /^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$/;
const defaultEndpoint = "/v1/portal-preference";

export interface ResolvedPortalPreference {
  readonly rendererID?: string;
  readonly shellTemplateID?: string;
  readonly themeTemplateID?: string;
  readonly iconThemeID?: string;
}

interface PendingRendererPreference {
  readonly scope: PortalPreferenceScope;
  readonly expectedRevision: number;
  readonly rendererID: string;
}

export class PortalPreferenceSession {
  private remote?: PortalPreference;
  private cache?: PortalPreference;
  private previewValues?: PortalPreferenceValues;
  private pendingRenderer?: PendingRendererPreference;
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
    session.pendingRenderer = readPendingRenderer(portal, scope);
    await session.refresh(portal);
    return session;
  }

  public resolve(portal: PortalRuntimeSpec["portal"]): ResolvedPortalPreference {
    const scope = preferenceScope(portal);
    if (!sameScope(scope, this.scope)) return {};
    const values = this.previewValues ?? preferredValues(this.remote, this.cache);
    const rendererID = this.pendingRenderer?.rendererID ?? validRenderer(portal, values.rendererId);
    const selectedRenderer = rendererID ?? portal.renderAdapter.config.defaultRenderer;
    const rendererOption = values.rendererOptions?.[selectedRenderer];
    return Object.freeze({
      ...(rendererID === undefined ? {} : { rendererID }),
      ...(validShellTemplate(portal, values.shellTemplateId) === undefined ? {} : { shellTemplateID: values.shellTemplateId }),
      ...(validID(rendererOption?.themeTemplateId) === undefined ? {} : { themeTemplateID: rendererOption?.themeTemplateId }),
      ...(validID(rendererOption?.iconThemeId) === undefined ? {} : { iconThemeID: rendererOption?.iconThemeId }),
    });
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

  public preview(patch: Partial<PortalPreferenceValues>): void {
    this.previewValues = mergeValues(preferredValues(this.remote, this.cache), patch);
  }

  public clearPreview(): void { this.previewValues = undefined; }

  public previewRendererOption(rendererID: string, patch: { themeTemplateId?: string; iconThemeId?: string }): void {
    this.preview({ rendererOptions: mergeRendererOptions(preferredValues(this.remote, this.cache).rendererOptions, rendererID, patch) });
  }

  public commitRendererOption(rendererID: string, patch: { themeTemplateId?: string; iconThemeId?: string }, portal: PortalRuntimeSpec["portal"]): Promise<PortalPreference> {
    return this.commit({ rendererOptions: mergeRendererOptions(preferredValues(this.remote, this.cache).rendererOptions, rendererID, patch) }, portal);
  }

  public async commit(patch: Partial<PortalPreferenceValues>, portal: PortalRuntimeSpec["portal"]): Promise<PortalPreference> {
    const scope = preferenceScope(portal);
    if (!sameScope(scope, this.scope)) {
      this.scope = scope;
      this.remote = undefined;
      this.cache = readCachedPreference(portal, scope);
    }
    const values = mergeValues(preferredValues(this.remote, this.cache), patch);
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
    this.previewValues = undefined;
    writeCachedPreference(portal, saved);
    return saved;
  }

  public stageRenderer(rendererID: string, portal: PortalRuntimeSpec["portal"]): void {
    if (validRenderer(portal, rendererID) === undefined) return;
    const pending = Object.freeze({ scope: this.scope, expectedRevision: this.remote?.revision ?? 0, rendererID });
    writePendingRenderer(portal, pending);
    this.pendingRenderer = pending;
  }

  public hasPendingRenderer(): boolean { return this.pendingRenderer !== undefined; }

  public discardPendingRenderer(portal: PortalRuntimeSpec["portal"]): void {
    clearPendingRenderer(portal);
    this.pendingRenderer = undefined;
  }

  public async commitPendingRenderer(portal: PortalRuntimeSpec["portal"]): Promise<void> {
    const pending = this.pendingRenderer;
    if (pending === undefined || !sameScope(pending.scope, this.scope)) return;
    if ((this.remote?.revision ?? 0) !== pending.expectedRevision) throw new PortalPreferenceConflict();
    await this.commit({ rendererId: pending.rendererID }, portal);
    this.discardPendingRenderer(portal);
  }

  public async migrateCache(portal: PortalRuntimeSpec["portal"]): Promise<void> {
    if (this.remote?.revision !== 0 || this.cache === undefined || emptyValues(this.cache.values)) return;
    await this.commit(this.cache.values, portal);
  }

  public async refresh(portal: PortalRuntimeSpec["portal"]): Promise<void> {
    this.portal = portal;
    const scope = preferenceScope(portal);
    if (!sameScope(scope, this.scope)) {
      this.scope = scope;
      this.remote = undefined;
      this.cache = readCachedPreference(portal, scope);
      this.pendingRenderer = readPendingRenderer(portal, scope);
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
  return Object.freeze({
    portalId: portal.id,
    renderer: Object.freeze({ id: portal.renderAdapter.id, contractMajor: contractMajor(portal.renderAdapter.uiContract) }),
    shell: Object.freeze({ id: portal.shell.id, contractMajor: contractMajor(portal.shell.uiContract) }),
    workbench: Object.freeze({ id: portal.workbench.id, contractMajor: contractMajor(portal.workbench.uiContract) }),
  });
}

function preferredValues(remote: PortalPreference | undefined, cache: PortalPreference | undefined): PortalPreferenceValues {
  return remote !== undefined && remote.revision > 0 ? remote.values : cache?.values ?? {};
}

function mergeValues(current: PortalPreferenceValues, patch: Partial<PortalPreferenceValues>): PortalPreferenceValues {
  return Object.freeze({
    ...(current.rendererId === undefined ? {} : { rendererId: current.rendererId }),
    ...(current.rendererOptions === undefined ? {} : { rendererOptions: current.rendererOptions }),
    ...(current.shellTemplateId === undefined ? {} : { shellTemplateId: current.shellTemplateId }),
    ...(current.collections === undefined ? {} : { collections: current.collections }),
    ...patch,
  });
}

function mergeRendererOptions(current: PortalPreferenceValues["rendererOptions"], rendererID: string, patch: { themeTemplateId?: string; iconThemeId?: string }): NonNullable<PortalPreferenceValues["rendererOptions"]> {
  const previous = current?.[rendererID] ?? {};
  return Object.freeze({ ...current, [rendererID]: Object.freeze({ ...previous, ...patch }) });
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
  const rendererId = validID(record.rendererId);
  const shellTemplateId = validID(record.shellTemplateId);
  const rendererOptions: Record<string, { themeTemplateId?: string; iconThemeId?: string }> = {};
  if (typeof record.rendererOptions === "object" && record.rendererOptions !== null && !Array.isArray(record.rendererOptions)) {
    for (const [rendererID, raw] of Object.entries(record.rendererOptions as Readonly<Record<string, unknown>>).slice(0, 16)) {
      if (validID(rendererID) === undefined || typeof raw !== "object" || raw === null || Array.isArray(raw)) continue;
      const option = raw as Readonly<Record<string, unknown>>;
      const themeTemplateId = validID(option.themeTemplateId);
      const iconThemeId = validID(option.iconThemeId);
      rendererOptions[rendererID] = Object.freeze({ ...(themeTemplateId === undefined ? {} : { themeTemplateId }), ...(iconThemeId === undefined ? {} : { iconThemeId }) });
    }
  }
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
  return Object.freeze({ ...(rendererId === undefined ? {} : { rendererId }), ...(Object.keys(rendererOptions).length === 0 ? {} : { rendererOptions: Object.freeze(rendererOptions) }), ...(shellTemplateId === undefined ? {} : { shellTemplateId }), ...(Object.keys(collections).length === 0 ? {} : { collections: Object.freeze(collections) }) });
}

function sanitizeCollectionPreference(value: CollectionPreference): CollectionPreference {
  const sanitized = sanitizeValues({ collections: { collection: value } }).collections?.collection;
  if (sanitized === undefined) throw new PortalPreferenceUnavailable("CollectionPreference 无效");
  return sanitized;
}

function readCachedPreference(portal: PortalRuntimeSpec["portal"], scope: PortalPreferenceScope): PortalPreference | undefined { return readStorage(cacheKey(portal, scope), scope) as PortalPreference | undefined; }
function writeCachedPreference(portal: PortalRuntimeSpec["portal"], preference: PortalPreference): void { writeStorage(cacheKey(portal, preference.scope), preference); }
function readPendingRenderer(portal: PortalRuntimeSpec["portal"], scope: PortalPreferenceScope): PendingRendererPreference | undefined {
  const value = readStorage(pendingKey(portal, scope), scope) as PendingRendererPreference | undefined;
  return value !== undefined && Number.isSafeInteger(value.expectedRevision) && value.expectedRevision >= 0 && validRenderer(portal, value.rendererID) !== undefined ? value : undefined;
}
function writePendingRenderer(portal: PortalRuntimeSpec["portal"], value: PendingRendererPreference): void { writeStorage(pendingKey(portal, value.scope), value); }
function clearPendingRenderer(portal: PortalRuntimeSpec["portal"]): void { try { globalThis.localStorage?.removeItem(pendingKey(portal, preferenceScope(portal))); } catch { /* best effort */ } }

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
function pendingKey(portal: PortalRuntimeSpec["portal"], scope: PortalPreferenceScope): string { return `vastplan.portal-preference-pending.${portal.tenantId}.${scopeKey(scope)}`; }
function scopeKey(scope: PortalPreferenceScope): string { return [scope.portalId, scope.renderer.id, scope.renderer.contractMajor, scope.shell.id, scope.shell.contractMajor, scope.workbench.id, scope.workbench.contractMajor].join("."); }
function sameScope(left: unknown, right: PortalPreferenceScope): boolean { try { return JSON.stringify(left) === JSON.stringify(right); } catch { return false; } }
function contractMajor(value: string): number { const match = value.trim().match(/^(?:\^|~|>=?)?\s*([1-9][0-9]{0,4})(?:\.|$)/); if (match === null) throw new PortalPreferenceUnavailable("UI contract major 无效"); return Number(match[1]); }
function validID(value: unknown): string | undefined { return typeof value === "string" && idPattern.test(value) ? value : undefined; }
function validRenderer(portal: PortalRuntimeSpec["portal"], value: unknown): string | undefined { const id = validID(value); return id !== undefined && portal.renderAdapter.config.userSelectable && portal.renderAdapter.config.allowedRenderers.includes(id) ? id : undefined; }
function validShellTemplate(portal: PortalRuntimeSpec["portal"], value: unknown): string | undefined { const id = validID(value); return id !== undefined && portal.shell.config.userSelectable && portal.shell.config.allowedTemplates.includes(id) ? id : undefined; }
function emptyValues(values: PortalPreferenceValues): boolean { return values.rendererId === undefined && values.shellTemplateId === undefined && Object.keys(values.rendererOptions ?? {}).length === 0 && Object.keys(values.collections ?? {}).length === 0; }

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
