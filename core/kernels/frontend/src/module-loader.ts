import type { FrontendPluginLoader, FrontendPluginModule, PluginRef } from "./portal-runtime";
import { ModuleLoadError } from "./module-errors";
import { normalizeFrontendModule } from "./module-exports";
import { ownedBuffer, sha256Hex } from "./module-integrity";
import { VerifiedModuleGraphLoader, type FrontendModuleGraphDescriptor } from "./module-graph-loader";
import { RuntimeSpecModuleResolver, pluginKey } from "./module-resolution";
import { validateFrontendModuleDescriptor, type FrontendModuleDescriptor, type PortalRuntimeSpec } from "./module-runtime-spec";
import type { FrontendRuntimeProtocol } from "./frontend-runtime-protocol";

export { ModuleLoadError } from "./module-errors";
export type { FrontendModuleGraphDescriptor } from "./module-graph-loader";
export { parseDevelopmentRuntimeSpec, parsePortalRuntimeSpec } from "./module-runtime-spec";
export type { FrontendModuleDescriptor, PortalRuntimeSpec } from "./module-runtime-spec";
export type { FrontendRuntimeProtocol } from "./frontend-runtime-protocol";

export type ModuleFetcher = (input: string, init?: RequestInit) => Promise<Response>;
export type ModuleImporter = (source: Uint8Array, sourceURL: string) => Promise<unknown>;

export interface FrontendPluginLoaderOptions {
  protocol: FrontendRuntimeProtocol;
  fetcher?: ModuleFetcher;
  importer?: ModuleImporter;
}

/**
 * Loads only modules listed in the trusted Portal-issued RuntimeSpec. The JavaScript is
 * fetched as bytes, checked against the server-governed digest, and imported
 * from an opaque blob URL; a plugin cannot self-assert provenance.
 */
export class VerifiedFrontendPluginLoader implements FrontendPluginLoader {
  private readonly pending = new Map<string, Promise<FrontendPluginModule>>();
  private readonly graphLoader: VerifiedModuleGraphLoader;
  private readonly resolver: RuntimeSpecModuleResolver;
  private readonly fetcher: ModuleFetcher;
  private readonly importer: ModuleImporter;
  private readonly protocol: FrontendRuntimeProtocol;

  public constructor(
    input: readonly FrontendModuleDescriptor[] | PortalRuntimeSpec,
    options: FrontendPluginLoaderOptions,
  ) {
    this.protocol = options.protocol;
    this.fetcher = options.fetcher ?? globalThis.fetch.bind(globalThis);
    this.importer = options.importer ?? importModuleBytes;
    const runtimeSpec = Array.isArray(input) ? undefined : input as PortalRuntimeSpec;
    const descriptors = runtimeSpec?.modules ?? input as readonly FrontendModuleDescriptor[];
    const graphs = runtimeSpec?.moduleGraphs ?? [];
    for (const descriptor of descriptors) {
      validateFrontendModuleDescriptor(descriptor, this.protocol);
    }
    this.graphLoader = new VerifiedModuleGraphLoader(graphs, { protocol: this.protocol, fetcher: this.fetcher });
    this.resolver = new RuntimeSpecModuleResolver(descriptors, graphs, this.protocol);
  }

  public canLoad(ref: PluginRef): boolean {
    return this.resolver.resolve(ref) !== undefined;
  }

  public load(ref: PluginRef): Promise<FrontendPluginModule> {
    const candidate = this.resolver.resolve(ref);
    if (candidate === undefined) return Promise.reject(new ModuleLoadError("MODULE_NOT_LOCKED", `Portal 运行描述未锁定模块: ${pluginKey(ref)}`));
    const descriptor = candidate.descriptor;
    const key = pluginKey(descriptor);
    const existing = this.pending.get(key);
    if (existing !== undefined) return existing;
    const started = candidate.kind === "module"
      ? this.loadVerified(candidate.descriptor)
      : this.loadVerifiedGraph(ref, candidate.descriptor);
    this.pending.set(key, started);
    return started;
  }

  public dispose(): void { this.graphLoader.dispose(); }

  private async loadVerifiedGraph(ref: PluginRef, graph: FrontendModuleGraphDescriptor): Promise<FrontendPluginModule> {
    const namespace = await this.graphLoader.load(graph);
    const entry = graph.nodes.find((node) => node.path === graph.entry)!;
    return normalizeFrontendModule(namespace, { id: ref.id, sha256: entry.sha256 });
  }

  private async loadVerified(descriptor: FrontendModuleDescriptor): Promise<FrontendPluginModule> {
    const response = await this.fetcher(descriptor.url, { credentials: "include", cache: this.protocol.requestCache(descriptor.url) });
    if (!response.ok) {
      throw new ModuleLoadError("MODULE_FETCH_FAILED", `前端模块获取失败: ${descriptor.id} (${response.status})`);
    }
    const bytes = new Uint8Array(await response.arrayBuffer());
    const actual = await sha256Hex(bytes);
    if (actual !== descriptor.sha256) {
      throw new ModuleLoadError("MODULE_INTEGRITY_MISMATCH", `前端模块摘要不匹配: ${descriptor.id}`);
    }
    const responseDigest = response.headers.get("X-VastPlan-Module-SHA256");
    if (responseDigest !== null && responseDigest !== descriptor.sha256) {
      throw new ModuleLoadError("MODULE_RESPONSE_UNBOUND", `前端模块响应与运行描述不一致: ${descriptor.id}`);
    }
    const namespace = await this.importer(bytes, descriptor.url);
    return normalizeFrontendModule(namespace, descriptor);
  }
}

async function importModuleBytes(source: Uint8Array, sourceURL: string): Promise<unknown> {
  const blob = new Blob([ownedBuffer(source)], { type: "text/javascript" });
  const objectURL = URL.createObjectURL(blob);
  try {
    return await import(/* @vite-ignore */ objectURL);
  } catch (error) {
    throw new ModuleLoadError("MODULE_IMPORT_FAILED", `无法导入前端模块 ${sourceURL}: ${String(error)}`);
  } finally {
    URL.revokeObjectURL(objectURL);
  }
}
