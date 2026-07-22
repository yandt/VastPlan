import type { PluginRef } from "./portal-contracts";
import { ModuleLoadError } from "./module-errors";
import { sha256Hex } from "./module-integrity";

export interface FrontendModuleDependencyDescriptor {
  specifier: string;
  path: string;
  kind: "static" | "dynamic" | "asset";
}

export interface FrontendModuleNodeDescriptor {
  path: string;
  url: string;
  sha256: string;
  size: number;
  mediaType: string;
  purpose: "entry" | "chunk" | "style" | "locale" | "asset" | "source-map";
  dependencies: readonly FrontendModuleDependencyDescriptor[];
}

export interface FrontendModuleGraphDescriptor extends PluginRef {
  target: "browser";
  entry: string;
  digest: string;
  packageSha256: string;
  externals: readonly string[];
  nodes: readonly FrontendModuleNodeDescriptor[];
  deferred?: boolean;
}

const allowedSharedExternals = new Set([
  "react", "react-dom", "react-dom/client", "react/jsx-runtime",
  "@vastplan/rjsf-csp-validator", "@vastplan/ui-primitives", "@vastplan/ui-contract", "@vastplan/workbench-sdk", "@vastplan/frontend-engine-contract",
]);

/** Validates the untrusted Browser RuntimeSpec projection before any fetch. */
export function validateModuleGraphDescriptor(graph: FrontendModuleGraphDescriptor): void {
  if (!graph.id || !graph.version || graph.target !== "browser" || !/^[a-f0-9]{64}$/.test(graph.digest) || !/^[a-f0-9]{64}$/.test(graph.packageSha256) ||
      !validModulePath(graph.entry) || !Array.isArray(graph.externals) || !Array.isArray(graph.nodes) || graph.nodes.length === 0 || graph.nodes.length > 512) {
    throw new ModuleLoadError("MODULE_GRAPH_INVALID", `前端 Module Graph 描述无效: ${graph.id || "unknown"}`);
  }
  if (graph.externals.length > 32 || new Set(graph.externals).size !== graph.externals.length || graph.externals.some((external) => !allowedSharedExternals.has(external)) ||
      (graph.deferred !== undefined && typeof graph.deferred !== "boolean")) {
    throw new ModuleLoadError("MODULE_GRAPH_EXTERNAL_REJECTED", `Module Graph 请求重复或未知共享依赖: ${graph.id}`);
  }
  const paths = new Set<string>();
  const digests = new Set<string>();
  let totalSize = 0;
  for (const node of graph.nodes) {
    if (!validModulePath(node.path) || paths.has(node.path) || digests.has(node.sha256) || !Number.isSafeInteger(node.size) || node.size <= 0 || node.size > 16 * 1024 * 1024 ||
        !/^[a-f0-9]{64}$/.test(node.sha256) || governedDigest(node.url) !== node.sha256 || !validMediaType(node.mediaType) || !validPurpose(node.purpose) || !Array.isArray(node.dependencies) || node.dependencies.length > 128) {
      throw new ModuleLoadError("MODULE_GRAPH_INVALID", `Module Graph 节点无效或重复: ${graph.id}/${node.path}`);
    }
    totalSize += node.size;
    if (totalSize > 64 * 1024 * 1024) throw new ModuleLoadError("MODULE_GRAPH_INVALID", `Module Graph 总大小超过 64 MiB: ${graph.id}`);
    paths.add(node.path);
    digests.add(node.sha256);
  }
  const entry = graph.nodes.find((node) => node.path === graph.entry);
  if (entry?.purpose !== "entry") throw new ModuleLoadError("MODULE_GRAPH_INVALID", `Module Graph 入口无效: ${graph.id}`);
  for (const node of graph.nodes) {
    const specifiers = new Set<string>();
    for (const dependency of node.dependencies) {
      if (!validSpecifier(dependency.specifier) || !["static", "dynamic", "asset"].includes(dependency.kind) || !paths.has(dependency.path) || dependency.path === node.path || specifiers.has(dependency.specifier)) {
        throw new ModuleLoadError("MODULE_GRAPH_OPEN", `Module Graph 依赖未闭合、指向自身或重复: ${graph.id}/${node.path}`);
      }
      specifiers.add(dependency.specifier);
    }
  }
  topologicalModuleOrder(graph, new Map(graph.nodes.map((node) => [node.path, node])));
}

export function computeModuleGraphDigest(graph: FrontendModuleGraphDescriptor): Promise<string> {
  return sha256Hex(new TextEncoder().encode(JSON.stringify(canonicalGraph(graph))));
}

export function topologicalModuleOrder(graph: FrontendModuleGraphDescriptor, nodes: ReadonlyMap<string, FrontendModuleNodeDescriptor>): string[] {
  const order: string[] = [], visiting = new Set<string>(), visited = new Set<string>();
  const depths = new Map<string, number>();
  const visit = (path: string): number => {
    if (visiting.has(path)) throw new ModuleLoadError("MODULE_GRAPH_CYCLE", `Module Graph 不允许循环依赖: ${graph.id}/${path}`);
    if (visited.has(path)) return depths.get(path)!;
    visiting.add(path);
    let depth = 1;
    for (const dependency of nodes.get(path)!.dependencies) depth = Math.max(depth, visit(dependency.path) + 1);
    if (depth > 64) throw new ModuleLoadError("MODULE_GRAPH_DEPTH", `Module Graph 依赖深度超过 64: ${graph.id}/${path}`);
    visiting.delete(path);
    visited.add(path);
    depths.set(path, depth);
    order.push(path);
    return depth;
  };
  for (const node of graph.nodes) visit(node.path);
  return order;
}

function canonicalGraph(graph: FrontendModuleGraphDescriptor): object {
  return {
    schemaVersion: "v1", target: graph.target, entry: graph.entry, externals: [...graph.externals].sort(),
    nodes: graph.nodes.map((node) => ({
      path: node.path, sha256: node.sha256, size: node.size, mediaType: node.mediaType, purpose: node.purpose,
      dependencies: [...node.dependencies].sort(compareDependency),
    })).sort((left, right) => left.path.localeCompare(right.path)),
  };
}

function compareDependency(left: FrontendModuleDependencyDescriptor, right: FrontendModuleDependencyDescriptor): number {
  if (left.specifier !== right.specifier) return left.specifier.localeCompare(right.specifier);
  return left.path === right.path ? left.kind.localeCompare(right.kind) : left.path.localeCompare(right.path);
}

function validModulePath(value: string): boolean {
  return typeof value === "string" && value.length <= 512 && /^[A-Za-z0-9][A-Za-z0-9._/-]*$/.test(value) && !value.includes("//") && !/(^|\/)\.(\/|$)/.test(value);
}

function validSpecifier(value: string): boolean {
  return typeof value === "string" && value.length <= 512 && /^[A-Za-z0-9._/-]+$/.test(value);
}

function validMediaType(value: string): boolean {
  return ["text/javascript", "text/css", "application/json", "application/wasm", "application/octet-stream", "image/svg+xml", "font/woff2"].includes(value);
}

function validPurpose(value: string): boolean {
  return ["entry", "chunk", "style", "locale", "asset", "source-map"].includes(value);
}

function governedDigest(url: string): string | undefined {
  return /^\/v1\/portal-modules\/[1-9]\d*\/([a-f0-9]{64})\.(?:js|css|json|wasm|bin)$/.exec(url)?.[1] ??
    /^\/v1\/portal-recovery-modules\/[1-9]\d*\/[1-9]\d*\/([a-f0-9]{64})\.(?:js|css|json|wasm|bin)$/.exec(url)?.[1];
}
