import type { FrontendPluginLoader, FrontendPluginModule, PluginRef } from "./portal-runtime";
import { ModuleLoadError } from "./module-errors";
import { normalizeFrontendModule } from "./module-exports";
import { ownedBuffer, sha256Hex } from "./module-integrity";
import { VerifiedModuleGraphLoader, type FrontendModuleGraphDescriptor } from "./module-graph-loader";
import { isDevelopmentModuleURL, validateFrontendModuleDescriptor, type FrontendModuleDescriptor, type ModuleDescriptorPolicy, type PortalRuntimeSpec } from "./module-runtime-spec";

export { ModuleLoadError } from "./module-errors";
export type { FrontendModuleGraphDescriptor } from "./module-graph-loader";
export { parseDevelopmentRuntimeSpec, parsePortalRuntimeSpec } from "./module-runtime-spec";
export type { FrontendModuleDescriptor, ModuleDescriptorPolicy, PortalRuntimeSpec } from "./module-runtime-spec";

export type ModuleFetcher = (input: string, init?: RequestInit) => Promise<Response>;
export type ModuleImporter = (source: Uint8Array, sourceURL: string) => Promise<unknown>;

/**
 * Loads only modules listed in the Edge-issued RuntimeSpec. The JavaScript is
 * fetched as bytes, checked against the server-governed digest, and imported
 * from an opaque blob URL; a plugin cannot self-assert provenance.
 */
export class VerifiedFrontendPluginLoader implements FrontendPluginLoader {
  private readonly modules = new Map<string, FrontendModuleDescriptor>();
  private readonly pending = new Map<string, Promise<FrontendPluginModule>>();
  private readonly graphLoader: VerifiedModuleGraphLoader;
  private readonly graphDescriptors = new Map<string, FrontendModuleGraphDescriptor>();

  public constructor(
    input: readonly FrontendModuleDescriptor[] | PortalRuntimeSpec,
    private readonly fetcher: ModuleFetcher = globalThis.fetch.bind(globalThis),
    private readonly importer: ModuleImporter = importModuleBytes,
    private readonly policy: ModuleDescriptorPolicy = "production",
  ) {
    const runtimeSpec = Array.isArray(input) ? undefined : input as PortalRuntimeSpec;
    const descriptors = runtimeSpec?.modules ?? input as readonly FrontendModuleDescriptor[];
    const graphs = runtimeSpec?.moduleGraphs ?? [];
    for (const descriptor of descriptors) {
      validateFrontendModuleDescriptor(descriptor, policy);
      const key = moduleKey(descriptor);
      if (this.modules.has(key)) {
        throw new ModuleLoadError("MODULE_DESCRIPTOR_DUPLICATE", `前端模块描述重复: ${key}`);
      }
      this.modules.set(key, { ...descriptor });
    }
    for (const graph of graphs) this.graphDescriptors.set(moduleKey(graph), graph);
    this.graphLoader = new VerifiedModuleGraphLoader(graphs, fetcher);
  }

  public load(ref: PluginRef): Promise<FrontendPluginModule> {
    const key = moduleKey(ref);
    const descriptor = this.modules.get(key);
    if (descriptor === undefined) {
      if (!this.graphLoader.has(ref)) return Promise.reject(new ModuleLoadError("MODULE_NOT_LOCKED", `Portal 运行描述未锁定模块: ${key}`));
      const existing = this.pending.get(key);
      if (existing !== undefined) return existing;
      const started = this.loadVerifiedGraph(ref);
      this.pending.set(key, started);
      return started;
    }
    const existing = this.pending.get(key);
    if (existing !== undefined) return existing;
    const started = this.loadVerified(descriptor);
    this.pending.set(key, started);
    return started;
  }

  public dispose(): void { this.graphLoader.dispose(); }

  private async loadVerifiedGraph(ref: PluginRef): Promise<FrontendPluginModule> {
    const namespace = await this.graphLoader.load(ref);
    const graph = this.runtimeGraph(ref);
    const entry = graph.nodes.find((node) => node.path === graph.entry)!;
    return normalizeFrontendModule(namespace, { id: ref.id, sha256: entry.sha256 });
  }

  private runtimeGraph(ref: PluginRef): FrontendModuleGraphDescriptor {
    const graph = this.graphDescriptors.get(moduleKey(ref));
    if (graph === undefined) throw new ModuleLoadError("MODULE_NOT_LOCKED", `Portal 未锁定 Module Graph: ${moduleKey(ref)}`);
    return graph;
  }

  private async loadVerified(descriptor: FrontendModuleDescriptor): Promise<FrontendPluginModule> {
    const response = await this.fetcher(descriptor.url, { credentials: "include", cache: isDevelopmentModuleURL(descriptor.url) ? "no-store" : "force-cache" });
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

function moduleKey(ref: PluginRef): string {
  return `${ref.id}@${ref.version}/${ref.channel ?? "stable"}`;
}
