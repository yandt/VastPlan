export interface PluginArtifactRef {
  readonly pluginId: string;
  readonly version: string;
  readonly channel: string;
}

export interface PluginArtifactIdentity {
  readonly ref: PluginArtifactRef;
  readonly sha256: string;
  readonly publisher: string;
}

export interface IndexedPluginContribution {
  readonly kind: string;
  readonly surface: string;
  readonly id: string;
  readonly contract?: string;
  readonly owner: PluginArtifactIdentity;
  readonly descriptor: Readonly<Record<string, unknown>>;
}

export interface ContributionIndexSnapshot {
  readonly schemaVersion: 1;
  readonly generation: number;
  readonly inventoryDigest: string;
  readonly contributions: readonly IndexedPluginContribution[];
  readonly digest: string;
}

export class PluginInventoryContractError extends Error {
  public constructor(message: string) { super(message); this.name = "PluginInventoryContractError"; }
}

/** Parses the trusted-host projection; raw Manifest data is never accepted. */
export function parseContributionIndex(value: unknown): ContributionIndexSnapshot {
  if (!isRecord(value) || value.schemaVersion !== 1 || !positiveInteger(value.generation) || !sha256(value.inventoryDigest) || !sha256(value.digest) || !Array.isArray(value.contributions)) {
    throw new PluginInventoryContractError("Contribution Index 快照无效");
  }
  const contributions = value.contributions.map(parseContribution);
  const identities = new Set(contributions.map((item) => `${item.kind}\u0000${item.id}\u0000${item.owner.ref.pluginId}@${item.owner.ref.version}/${item.owner.ref.channel}#${item.owner.sha256}`));
  if (identities.size !== contributions.length) throw new PluginInventoryContractError("Contribution Index 身份重复");
  return Object.freeze({ schemaVersion: 1, generation: value.generation, inventoryDigest: value.inventoryDigest, contributions: Object.freeze(contributions), digest: value.digest });
}

export function contributionsByKind(index: ContributionIndexSnapshot, kind: string): readonly IndexedPluginContribution[] {
  return index.contributions.filter((item) => item.kind === kind);
}

export type { ActivationSelection, PluginReconciliationAction, PluginReconciliationPlan, PluginTarget } from "./reconciliation.js";
export { parsePluginReconciliationPlan } from "./reconciliation.js";

function parseContribution(value: unknown): IndexedPluginContribution {
  if (!isRecord(value) || !name(value.surface) || typeof value.kind !== "string" || !value.kind.startsWith(`${value.surface}.`) || !name(value.id) ||
      (value.contract !== undefined && typeof value.contract !== "string") || !isRecord(value.owner) || !isRecord(value.owner.ref) ||
      !pluginID(value.owner.ref.pluginId) || !semver(value.owner.ref.version) || !name(value.owner.ref.channel) || !sha256(value.owner.sha256) ||
      !boundedText(value.owner.publisher) || !isRecord(value.descriptor) || value.descriptor.id !== value.id) {
    throw new PluginInventoryContractError("Contribution Index 贡献无效");
  }
  const descriptor = deepFreeze(structuredClone(value.descriptor));
  return Object.freeze({ kind: value.kind, surface: value.surface, id: value.id, ...(value.contract === undefined ? {} : { contract: value.contract }), owner: Object.freeze({ ref: Object.freeze({ pluginId: value.owner.ref.pluginId, version: value.owner.ref.version, channel: value.owner.ref.channel }), sha256: value.owner.sha256, publisher: value.owner.publisher }), descriptor });
}

function deepFreeze<T>(value: T): T {
  if (Array.isArray(value)) for (const item of value) deepFreeze(item);
  else if (isRecord(value)) for (const item of Object.values(value)) deepFreeze(item);
  return Object.freeze(value);
}

function positiveInteger(value: unknown): value is number { return Number.isSafeInteger(value) && Number(value) > 0; }
function sha256(value: unknown): value is string { return typeof value === "string" && /^[a-f0-9]{64}$/.test(value); }
function semver(value: unknown): value is string { return typeof value === "string" && /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.test(value); }
function pluginID(value: unknown): value is string { return typeof value === "string" && /^[a-z0-9]+(?:[.-][a-z0-9]+)+$/.test(value); }
function name(value: unknown): value is string { return typeof value === "string" && /^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$/.test(value) && value.length <= 160; }
function boundedText(value: unknown): value is string { return typeof value === "string" && value.length > 0 && value.length <= 160; }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
