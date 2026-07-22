import type { PortalSpec } from "./portal-runtime-contract";
import type {
  CollectionPreference,
  PortalPreference,
  PortalPreferenceScope,
  PreferenceCatalogScope,
  PortalPreferenceValues,
  RendererPreference,
} from "@vastplan/frontend-engine-contract";

export type { PortalPreference, PortalPreferenceScope, PortalPreferenceValues } from "@vastplan/frontend-engine-contract";

const idPattern = /^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$/;

export function preferenceScopeForPortal(portal: PortalSpec): PortalPreferenceScope {
  const renderAdapter = objectField(portal, "renderAdapter");
  const shell = objectField(portal, "shell");
  const workbench = objectField(portal, "workbench");
  return Object.freeze({
    portalId: requiredID(portal.id, "PortalPreference portalId"),
    renderer: catalogScope(renderAdapter, "Renderer"),
    shell: catalogScope(shell, "Shell"),
    workbench: catalogScope(workbench, "Workbench"),
  });
}

export function parsePortalPreference(value: unknown, expectedScope: PortalPreferenceScope): PortalPreference {
  const record = object(value, "PortalPreference 响应必须是对象");
  if (!nonnegativeInteger(record.revision)) throw new Error("PortalPreference revision 无效");
  const scope = parseScope(record.scope);
  if (JSON.stringify(scope) !== JSON.stringify(expectedScope)) throw new Error("PortalPreference scope 与活动 Portal 不一致");
  const values = parseValues(record.values);
  if (record.updatedAt !== undefined && (typeof record.updatedAt !== "string" || record.updatedAt.length > 64)) throw new Error("PortalPreference updatedAt 无效");
  return Object.freeze({ revision: record.revision, scope, values, ...(record.updatedAt === undefined ? {} : { updatedAt: record.updatedAt }) });
}

export function parsePreferencePutBody(value: unknown): { readonly expectedRevision: number; readonly values: PortalPreferenceValues } {
  const record = exactObject(value, ["expectedRevision", "values"], "PortalPreference PUT 必须是对象");
  if (!nonnegativeInteger(record.expectedRevision)) throw new Error("PortalPreference expectedRevision 无效");
  return Object.freeze({ expectedRevision: record.expectedRevision, values: parseValues(record.values) });
}

function parseScope(value: unknown): PortalPreferenceScope {
  const record = exactObject(value, ["portalId", "renderer", "shell", "workbench"], "PortalPreference scope 无效");
  return Object.freeze({
    portalId: requiredID(record.portalId, "PortalPreference portalId"),
    renderer: parseCatalogScope(record.renderer), shell: parseCatalogScope(record.shell), workbench: parseCatalogScope(record.workbench),
  });
}

function parseCatalogScope(value: unknown): PreferenceCatalogScope {
  const record = exactObject(value, ["id", "contractMajor"], "PortalPreference catalog scope 无效");
  if (!positiveInteger(record.contractMajor) || record.contractMajor > 65535) throw new Error("PortalPreference contractMajor 无效");
  return Object.freeze({ id: requiredID(record.id, "PortalPreference catalog ID"), contractMajor: record.contractMajor });
}

function parseValues(value: unknown): PortalPreferenceValues {
  const record = exactObject(value, ["rendererId", "rendererOptions", "shellTemplateId", "collections"], "PortalPreference values 无效");
  const rendererId = optionalID(record.rendererId, "rendererId");
  const shellTemplateId = optionalID(record.shellTemplateId, "shellTemplateId");
  const rendererOptions = record.rendererOptions === undefined ? undefined : parseRendererOptions(record.rendererOptions);
  const collections = record.collections === undefined ? undefined : parseCollections(record.collections);
  return Object.freeze({ ...(rendererId === undefined ? {} : { rendererId }), ...(rendererOptions === undefined ? {} : { rendererOptions }), ...(shellTemplateId === undefined ? {} : { shellTemplateId }), ...(collections === undefined ? {} : { collections }) });
}

function parseRendererOptions(value: unknown): Readonly<Record<string, RendererPreference>> {
  const record = object(value, "PortalPreference rendererOptions 无效");
  if (Object.keys(record).length > 16) throw new Error("PortalPreference rendererOptions 超过上限");
  const out: Record<string, RendererPreference> = {};
  for (const [rendererID, raw] of Object.entries(record)) {
    requiredID(rendererID, "PortalPreference renderer ID");
    const option = exactObject(raw, ["themeTemplateId", "iconThemeId"], "PortalPreference renderer option 无效");
    const themeTemplateId = optionalID(option.themeTemplateId, "themeTemplateId");
    const iconThemeId = optionalID(option.iconThemeId, "iconThemeId");
    out[rendererID] = Object.freeze({ ...(themeTemplateId === undefined ? {} : { themeTemplateId }), ...(iconThemeId === undefined ? {} : { iconThemeId }) });
  }
  return Object.freeze(out);
}

function parseCollections(value: unknown): Readonly<Record<string, CollectionPreference>> {
  const record = object(value, "PortalPreference collections 无效");
  if (Object.keys(record).length > 128) throw new Error("PortalPreference collections 超过上限");
  const out: Record<string, CollectionPreference> = {};
  for (const [collectionID, raw] of Object.entries(record)) {
    requiredID(collectionID, "PortalPreference collection ID");
    const item = exactObject(raw, ["columns", "hiddenColumns", "density", "pageSize"], "PortalPreference collection 无效");
    const columns = item.columns === undefined ? undefined : parseColumns(item.columns);
    const hiddenColumns = item.hiddenColumns === undefined ? undefined : parseColumns(item.hiddenColumns);
    if (item.density !== undefined && item.density !== "compact" && item.density !== "standard" && item.density !== "comfortable") throw new Error("PortalPreference density 无效");
    if (item.pageSize !== undefined && (!positiveInteger(item.pageSize) || item.pageSize > 1000)) throw new Error("PortalPreference pageSize 无效");
    out[collectionID] = Object.freeze({ ...(columns === undefined ? {} : { columns }), ...(hiddenColumns === undefined ? {} : { hiddenColumns }), ...(item.density === undefined ? {} : { density: item.density }), ...(item.pageSize === undefined ? {} : { pageSize: item.pageSize }) });
  }
  return Object.freeze(out);
}

function parseColumns(value: unknown): readonly string[] {
  if (!Array.isArray(value) || value.length > 128) throw new Error("PortalPreference columns 无效");
  const columns = value.map((column) => requiredID(column, "PortalPreference column ID"));
  if (new Set(columns).size !== columns.length) throw new Error("PortalPreference column ID 重复");
  return Object.freeze(columns);
}

function catalogScope(value: Readonly<Record<string, unknown>>, label: string): PreferenceCatalogScope {
  const id = requiredID(value.id, `${label} catalog ID`);
  if (typeof value.uiContract !== "string") throw new Error(`${label} uiContract 缺失`);
  const major = contractMajor(value.uiContract);
  if (major === undefined) throw new Error(`${label} uiContract major 无效`);
  return Object.freeze({ id, contractMajor: major });
}

function contractMajor(value: string): number | undefined {
  const match = value.trim().match(/^(?:\^|~|>=?)?\s*([1-9][0-9]{0,4})(?:\.|$)/);
  if (match === null) return undefined;
  const major = Number(match[1]);
  return Number.isSafeInteger(major) && major <= 65535 ? major : undefined;
}

function objectField(value: Readonly<Record<string, unknown>>, key: string): Readonly<Record<string, unknown>> { return object(value[key], `Portal ${key} 缺失`); }
function object(value: unknown, message: string): Readonly<Record<string, unknown>> { if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error(message); return value as Readonly<Record<string, unknown>>; }
function exactObject(value: unknown, allowed: readonly string[], message: string): Readonly<Record<string, unknown>> { const record = object(value, message); if (Object.keys(record).some((key) => !allowed.includes(key))) throw new Error(`${message}: 包含未知字段`); return record; }
function requiredID(value: unknown, label: string): string { if (typeof value !== "string" || !idPattern.test(value)) throw new Error(`${label} 无效`); return value; }
function optionalID(value: unknown, label: string): string | undefined { return value === undefined ? undefined : requiredID(value, `PortalPreference ${label}`); }
function positiveInteger(value: unknown): value is number { return Number.isSafeInteger(value) && (value as number) > 0; }
function nonnegativeInteger(value: unknown): value is number { return Number.isSafeInteger(value) && (value as number) >= 0; }
