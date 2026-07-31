import type { PluginRef, PortalSpec } from "./portal-contracts";
import { ModuleLoadError } from "./module-errors";
import { validateModuleGraphDescriptor, type FrontendModuleGraphDescriptor } from "./module-graph-contract";
import type { FrontendRuntimeProtocol } from "./frontend-runtime-protocol";
import { parsePortalExtensionGraph } from "@vastplan/plugin-extension-contract";
import type { PortalExtensionGraph } from "@vastplan/plugin-extension-contract";
import { parseContributionIndex, type ContributionIndexSnapshot } from "@vastplan/plugin-inventory-contract";

export interface FrontendModuleDescriptor extends PluginRef {
  entry: string;
  url: string;
  sha256: string;
  packageSha256: string;
  deferred?: boolean;
}

export interface PortalRuntimeSpec {
  portal: PortalSpec;
  modules: FrontendModuleDescriptor[];
  moduleGraphs: FrontendModuleGraphDescriptor[];
  extensions?: PortalExtensionGraph;
  contributions: ContributionIndexSnapshot;
  coordination?: PortalGenerationCoordination;
}

export type PortalGenerationCoordination =
  | { state: "prepared"; activationId: number; transactionId: string }
  | { state: "committed"; activationId: number };

export function validateFrontendModuleDescriptor(descriptor: FrontendModuleDescriptor, protocol: FrontendRuntimeProtocol): void {
  const governedDigest = protocol.governedDigest(descriptor.url, "entry");
  if (!descriptor.id || !descriptor.version || governedDigest === undefined ||
      !/^[a-f0-9]{64}$/.test(descriptor.sha256) || !/^[a-f0-9]{64}$/.test(descriptor.packageSha256) ||
      (!descriptor.entry.endsWith(".js") && !descriptor.entry.endsWith(".mjs")) ||
      (descriptor.deferred !== undefined && typeof descriptor.deferred !== "boolean")) {
    throw new ModuleLoadError("MODULE_DESCRIPTOR_INVALID", `前端模块描述无效: ${descriptor.id || "unknown"}`);
  }
  if (governedDigest !== descriptor.sha256) throw new ModuleLoadError("MODULE_DESCRIPTOR_INVALID", `前端模块 URL 未按内容摘要寻址: ${descriptor.id}`);
}

export function parseRuntimeSpec(value: unknown, protocol: FrontendRuntimeProtocol): PortalRuntimeSpec {
  if (!isRecord(value) || !isRecord(value.portal) || (value.modules !== undefined && !Array.isArray(value.modules)) || (value.moduleGraphs !== undefined && !Array.isArray(value.moduleGraphs))) {
    throw new ModuleLoadError("RUNTIME_SPEC_INVALID", "Portal RuntimeSpec 结构无效");
  }
  const modules = (value.modules ?? []).map((item) => {
    if (!isRecord(item)) throw new ModuleLoadError("RUNTIME_SPEC_INVALID", "Portal 模块描述无效");
    const descriptor = item as unknown as FrontendModuleDescriptor;
    validateFrontendModuleDescriptor(descriptor, protocol);
    return { ...descriptor };
  });
  const moduleGraphs = (value.moduleGraphs ?? []).map((item) => {
    if (!isRecord(item)) throw new ModuleLoadError("RUNTIME_SPEC_INVALID", "Portal Module Graph 描述无效");
    const graph = item as unknown as FrontendModuleGraphDescriptor;
    validateModuleGraphDescriptor(graph, protocol);
    return { ...graph, externals: [...graph.externals], nodes: graph.nodes.map((node) => ({ ...node, dependencies: node.dependencies.map((dependency) => ({ ...dependency })) })) };
  });
  if (modules.length === 0 && moduleGraphs.length === 0) throw new ModuleLoadError("RUNTIME_SPEC_INVALID", "Portal RuntimeSpec 没有前端模块");
  const portal = value.portal as unknown as PortalSpec;
  let extensions: PortalExtensionGraph;
  try { extensions = parsePortalExtensionGraph(value.extensions); }
  catch (error) { throw new ModuleLoadError("EXTENSION_GRAPH_INVALID", String(error)); }
  let contributions: ContributionIndexSnapshot;
  try { contributions = parseContributionIndex(value.contributions); }
  catch (error) { throw new ModuleLoadError("CONTRIBUTION_INDEX_INVALID", String(error)); }
  validateContributionOwners(contributions, modules, moduleGraphs);
  const coordination = parseCoordination(value.coordination, portal.revision);
  return { portal, modules, moduleGraphs, extensions, contributions, ...(coordination === undefined ? {} : { coordination }) };
}

function validateContributionOwners(index: ContributionIndexSnapshot, modules: readonly FrontendModuleDescriptor[], graphs: readonly FrontendModuleGraphDescriptor[]): void {
  const packages = new Map<string, string>();
  for (const module of modules) packages.set(pluginKey(module), module.packageSha256);
  for (const graph of graphs) packages.set(pluginKey(graph), graph.packageSha256);
  for (const contribution of index.contributions) {
    const key = `${contribution.owner.ref.pluginId}@${contribution.owner.ref.version}/${contribution.owner.ref.channel || "stable"}`;
    if (packages.get(key) !== contribution.owner.sha256) throw new ModuleLoadError("CONTRIBUTION_OWNER_INVALID", `Contribution 所有者未绑定已交付模块: ${contribution.kind}/${contribution.id}`);
  }
}

function pluginKey(ref: { id: string; version: string; channel?: string }): string {
  return `${ref.id}@${ref.version}/${ref.channel || "stable"}`;
}

function parseCoordination(value: unknown, revision: number): PortalGenerationCoordination | undefined {
  if (value === undefined) return undefined;
  if (!isRecord(value) || (value.state !== "prepared" && value.state !== "committed") || !Number.isSafeInteger(value.activationId)
    || Number(value.activationId) !== revision) throw new ModuleLoadError("RUNTIME_SPEC_INVALID", "Portal Generation 协调信息无效");
  if (value.state === "committed") {
    if (Object.keys(value).some((key) => key !== "state" && key !== "activationId")) throw new ModuleLoadError("RUNTIME_SPEC_INVALID", "Portal Generation 协调信息无效");
    return { state: "committed", activationId: revision };
  }
  if (Object.keys(value).some((key) => key !== "state" && key !== "activationId" && key !== "transactionId")
    || typeof value.transactionId !== "string" || !/^[a-f0-9]{64}$/.test(value.transactionId)) throw new ModuleLoadError("RUNTIME_SPEC_INVALID", "Portal Generation 协调事务无效");
  return { state: "prepared", activationId: revision, transactionId: value.transactionId };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
