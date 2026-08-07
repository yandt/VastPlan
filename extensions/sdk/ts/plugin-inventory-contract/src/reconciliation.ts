import type { PluginArtifactIdentity } from "./index.js";

export type PluginTarget = "backend" | "frontend" | "desktop" | "mobile";

export interface ActivationSelection {
  readonly schemaVersion: 1;
  readonly policyId: string;
  readonly target: PluginTarget;
  readonly generation: number;
  readonly inventoryDigest: string;
  readonly contributionDigest: string;
  readonly artifacts: readonly PluginArtifactIdentity[];
  readonly digest: string;
}

export interface PluginReconciliationAction {
  readonly pluginId: string;
  readonly operation: "activate" | "replace" | "deactivate" | "retain";
  readonly strategy: string;
  readonly current?: PluginArtifactIdentity;
  readonly candidate?: PluginArtifactIdentity;
}

export interface PluginReconciliationPlan {
  readonly schemaVersion: 1;
  readonly target: PluginTarget;
  readonly generation: number;
  readonly selectionDigest: string;
  readonly contributionDigest: string;
  readonly actions: readonly PluginReconciliationAction[];
  readonly digest: string;
}

export function parsePluginReconciliationPlan(value: unknown): PluginReconciliationPlan {
  if (!record(value) || value.schemaVersion !== 1 || !target(value.target) || !positive(value.generation) || !digest(value.selectionDigest) || !digest(value.contributionDigest) || !digest(value.digest) || !Array.isArray(value.actions)) throw new Error("Plugin Reconciliation Plan 无效");
  const actions = value.actions.map((action) => parseAction(action));
  if (new Set(actions.map((action) => action.pluginId)).size !== actions.length) throw new Error("Plugin Reconciliation Action 重复");
  return Object.freeze({ schemaVersion: 1, target: value.target, generation: value.generation, selectionDigest: value.selectionDigest, contributionDigest: value.contributionDigest, actions: Object.freeze(actions), digest: value.digest });
}

function parseAction(value: unknown): PluginReconciliationAction {
  if (!record(value) || typeof value.pluginId !== "string" || value.pluginId.length === 0 || !["activate", "replace", "deactivate", "retain"].includes(String(value.operation)) || typeof value.strategy !== "string" || value.strategy.length === 0) throw new Error("Plugin Reconciliation Action 无效");
  return Object.freeze({ pluginId: value.pluginId, operation: value.operation, strategy: value.strategy, ...(value.current === undefined ? {} : { current: value.current }), ...(value.candidate === undefined ? {} : { candidate: value.candidate }) }) as PluginReconciliationAction;
}

function target(value: unknown): value is PluginTarget { return value === "backend" || value === "frontend" || value === "desktop" || value === "mobile"; }
function positive(value: unknown): value is number { return Number.isSafeInteger(value) && Number(value) > 0; }
function digest(value: unknown): value is string { return typeof value === "string" && /^[a-f0-9]{64}$/.test(value); }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
