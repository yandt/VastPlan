import type { PluginRef, PortalSpec } from "./portal-contracts";
import { ModuleLoadError } from "./module-errors";
import { validateModuleGraphDescriptor, type FrontendModuleGraphDescriptor } from "./module-graph-contract";

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
  coordination?: PortalGenerationCoordination;
}

export type PortalGenerationCoordination =
  | { state: "prepared"; activationId: number; transactionId: string }
  | { state: "committed"; activationId: number };

export type ModuleDescriptorPolicy = "production" | "development";

export function parsePortalRuntimeSpec(value: unknown): PortalRuntimeSpec {
  return parseRuntimeSpec(value, "production");
}

/** Separate parser for local platformdev overlays; production URLs remain valid. */
export function parseDevelopmentRuntimeSpec(value: unknown): PortalRuntimeSpec {
  return parseRuntimeSpec(value, "development");
}

export function validateFrontendModuleDescriptor(descriptor: FrontendModuleDescriptor, policy: ModuleDescriptorPolicy): void {
  const active = /^\/v1\/portal-modules\/[1-9]\d*\/([a-f0-9]{64})\.js$/.exec(descriptor.url);
  const recovery = /^\/v1\/portal-recovery-modules\/[1-9]\d*\/[1-9]\d*\/([a-f0-9]{64})\.js$/.exec(descriptor.url);
  const development = policy === "development" ? /^\/__vastplan_dev\/modules\/([a-f0-9]{64})\.js$/.exec(descriptor.url) : null;
  const governedDigest = active?.[1] ?? recovery?.[1] ?? development?.[1];
  if (!descriptor.id || !descriptor.version || governedDigest === undefined ||
      !/^[a-f0-9]{64}$/.test(descriptor.sha256) || !/^[a-f0-9]{64}$/.test(descriptor.packageSha256) ||
      (!descriptor.entry.endsWith(".js") && !descriptor.entry.endsWith(".mjs")) ||
      (descriptor.deferred !== undefined && typeof descriptor.deferred !== "boolean")) {
    throw new ModuleLoadError("MODULE_DESCRIPTOR_INVALID", `前端模块描述无效: ${descriptor.id || "unknown"}`);
  }
  if (governedDigest !== descriptor.sha256) throw new ModuleLoadError("MODULE_DESCRIPTOR_INVALID", `前端模块 URL 未按内容摘要寻址: ${descriptor.id}`);
}

export function isDevelopmentModuleURL(url: string): boolean {
  return url.startsWith("/__vastplan_dev/modules/");
}

function parseRuntimeSpec(value: unknown, policy: ModuleDescriptorPolicy): PortalRuntimeSpec {
  if (!isRecord(value) || !isRecord(value.portal) || (value.modules !== undefined && !Array.isArray(value.modules)) || (value.moduleGraphs !== undefined && !Array.isArray(value.moduleGraphs))) {
    throw new ModuleLoadError("RUNTIME_SPEC_INVALID", "Portal RuntimeSpec 结构无效");
  }
  const modules = (value.modules ?? []).map((item) => {
    if (!isRecord(item)) throw new ModuleLoadError("RUNTIME_SPEC_INVALID", "Portal 模块描述无效");
    const descriptor = item as unknown as FrontendModuleDescriptor;
    validateFrontendModuleDescriptor(descriptor, policy);
    return { ...descriptor };
  });
  const moduleGraphs = (value.moduleGraphs ?? []).map((item) => {
    if (!isRecord(item)) throw new ModuleLoadError("RUNTIME_SPEC_INVALID", "Portal Module Graph 描述无效");
    const graph = item as unknown as FrontendModuleGraphDescriptor;
    validateModuleGraphDescriptor(graph, policy);
    return { ...graph, externals: [...graph.externals], nodes: graph.nodes.map((node) => ({ ...node, dependencies: node.dependencies.map((dependency) => ({ ...dependency })) })) };
  });
  if (modules.length === 0 && moduleGraphs.length === 0) throw new ModuleLoadError("RUNTIME_SPEC_INVALID", "Portal RuntimeSpec 没有前端模块");
  const portal = value.portal as unknown as PortalSpec;
  const coordination = parseCoordination(value.coordination, portal.revision);
  return { portal, modules, moduleGraphs, ...(coordination === undefined ? {} : { coordination }) };
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
