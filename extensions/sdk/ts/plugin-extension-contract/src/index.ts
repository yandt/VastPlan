export type PluginExtensionSurface = "backend" | "frontend" | "desktop" | "mobile";
export type PluginExtensionDispatch = "single" | "select" | "fanout" | "mount";

/** Trusted projection of one signed extension-point declaration. */
export interface PortalExtensionPoint {
  readonly id: string;
  readonly ownerPluginId: string;
  readonly surface: PluginExtensionSurface;
  readonly contract: string;
  readonly kind: string;
  readonly dispatch: PluginExtensionDispatch;
  readonly targets?: readonly string[];
}

/** JSON-only extension contribution validated by the trusted artifact catalog. */
export interface PortalExtensionContribution {
  readonly point: string;
  readonly id: string;
  readonly pluginId: string;
  readonly contract: string;
  readonly order?: number;
  readonly descriptor: Readonly<Record<string, unknown>>;
}

export interface PortalExtensionGraph {
  readonly points: readonly PortalExtensionPoint[];
  readonly contributions: readonly PortalExtensionContribution[];
}

/**
 * A plugin receives only its own view of the immutable Generation graph.
 * Point owners can enumerate all contributions; contributors can see only
 * their own bindings and cannot mutate or impersonate another plugin.
 */
export interface PluginExtensionAccess {
  owns(pointId: string): boolean;
  contributes(pointId: string): boolean;
  list(pointId: string): readonly PortalExtensionContribution[];
}

export class PluginExtensionContractError extends Error {
  public constructor(message: string) { super(message); this.name = "PluginExtensionContractError"; }
}

export const emptyPortalExtensionGraph: PortalExtensionGraph = Object.freeze({ points: Object.freeze([]), contributions: Object.freeze([]) });

/** Shared parser used at every frontend trust boundary. */
export function parsePortalExtensionGraph(value: unknown): PortalExtensionGraph {
  if (value === undefined) return emptyPortalExtensionGraph;
  if (!isRecord(value) || !Array.isArray(value.points) || !Array.isArray(value.contributions)) {
    throw new PluginExtensionContractError("Portal 插件扩展图结构无效");
  }
  const points = value.points.map(parsePoint);
  const pointIDs = new Set(points.map((point) => point.id));
  if (pointIDs.size !== points.length) throw new PluginExtensionContractError("Portal 插件扩展点重复");
  const targetOwners = new Map<string, string>();
  for (const point of points) {
    if (!point.id.startsWith(`${point.ownerPluginId}.`)) throw new PluginExtensionContractError(`扩展点不属于声明的所有者: ${point.id}`);
    if (point.targets !== undefined && new Set(point.targets).size !== point.targets.length) throw new PluginExtensionContractError(`扩展点目标重复: ${point.id}`);
    for (const target of point.targets ?? []) {
      const key = `${point.kind}\u0000${target}`;
      const owner = targetOwners.get(key);
      if (owner !== undefined && owner !== point.ownerPluginId) throw new PluginExtensionContractError(`扩展目标被多个插件拥有: ${point.kind}/${target}`);
      targetOwners.set(key, point.ownerPluginId);
    }
  }
  const contributions = value.contributions.map((item) => parseContribution(item, pointIDs));
  const contributionIDs = new Set(contributions.map((item) => `${item.point}\u0000${item.id}`));
  if (contributionIDs.size !== contributions.length) throw new PluginExtensionContractError("Portal 插件扩展贡献重复");
  for (const contribution of contributions) {
    if (!contribution.id.startsWith(`${contribution.pluginId}.`)) throw new PluginExtensionContractError(`扩展贡献不属于声明的插件: ${contribution.id}`);
  }
  return Object.freeze({ points: Object.freeze(points), contributions: Object.freeze(contributions) });
}

function parsePoint(value: unknown): PortalExtensionPoint {
  if (!isRecord(value) || !name(value.id) || !name(value.ownerPluginId) || !["backend", "frontend", "desktop", "mobile"].includes(String(value.surface)) ||
      typeof value.contract !== "string" || !name(value.kind) || !["single", "select", "fanout", "mount"].includes(String(value.dispatch)) ||
      (value.targets !== undefined && (!Array.isArray(value.targets) || value.targets.some((target) => !name(target))))) {
    throw new PluginExtensionContractError("Portal 插件扩展点无效");
  }
  return Object.freeze({ id: value.id, ownerPluginId: value.ownerPluginId, surface: value.surface, contract: value.contract, kind: value.kind, dispatch: value.dispatch, ...(value.targets === undefined ? {} : { targets: Object.freeze([...value.targets]) }) }) as PortalExtensionPoint;
}

function parseContribution(value: unknown, pointIDs: ReadonlySet<string>): PortalExtensionContribution {
  if (!isRecord(value) || !name(value.point) || !pointIDs.has(value.point) || !name(value.id) || !name(value.pluginId) || typeof value.contract !== "string" ||
      (value.order !== undefined && (!Number.isSafeInteger(value.order) || Math.abs(Number(value.order)) > 1_000_000)) || !isRecord(value.descriptor)) {
    throw new PluginExtensionContractError("Portal 插件扩展贡献无效");
  }
  return Object.freeze({ point: value.point, id: value.id, pluginId: value.pluginId, contract: value.contract, ...(value.order === undefined ? {} : { order: value.order }), descriptor: freezeRecord(value.descriptor) }) as PortalExtensionContribution;
}

function freezeRecord(value: Record<string, unknown>): Readonly<Record<string, unknown>> {
  const clone = structuredClone(value);
  for (const child of Object.values(clone)) freezeValue(child);
  return Object.freeze(clone);
}

function freezeValue(value: unknown): void {
  if (Array.isArray(value)) { for (const child of value) freezeValue(child); Object.freeze(value); }
  else if (isRecord(value)) { for (const child of Object.values(value)) freezeValue(child); Object.freeze(value); }
}

function name(value: unknown): value is string { return typeof value === "string" && /^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$/.test(value) && value.length <= 160; }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
