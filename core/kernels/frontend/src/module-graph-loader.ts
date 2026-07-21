import type { PluginRef } from "./portal-contracts";
import { ModuleLoadError } from "./module-errors";
import { ownedBuffer, sha256Hex } from "./module-integrity";
import { computeModuleGraphDigest, topologicalModuleOrder, validateModuleGraphDescriptor, type FrontendModuleGraphDescriptor } from "./module-graph-contract";

export { computeModuleGraphDigest, validateModuleGraphDescriptor } from "./module-graph-contract";
export type { FrontendModuleDependencyDescriptor, FrontendModuleGraphDescriptor, FrontendModuleNodeDescriptor } from "./module-graph-contract";

export type GraphFetcher = (input: string, init?: RequestInit) => Promise<Response>;
export type GraphNamespaceImporter = (entryURL: string, sourceURL: string) => Promise<unknown>;

export class VerifiedModuleGraphLoader {
  private readonly graphs = new Map<string, FrontendModuleGraphDescriptor>();
  private readonly pending = new Map<string, Promise<unknown>>();
  private readonly objectURLs = new Set<string>();

  public constructor(
    graphs: readonly FrontendModuleGraphDescriptor[],
    private readonly fetcher: GraphFetcher,
    private readonly importer: GraphNamespaceImporter = importGraphEntry,
  ) {
    for (const graph of graphs) {
      validateModuleGraphDescriptor(graph);
      const key = pluginKey(graph);
      if (this.graphs.has(key)) throw new ModuleLoadError("MODULE_GRAPH_DUPLICATE", `前端 Module Graph 重复: ${key}`);
      this.graphs.set(key, graph);
    }
  }

  public has(ref: PluginRef): boolean { return this.graphs.has(pluginKey(ref)); }

  public load(ref: PluginRef): Promise<unknown> {
    const key = pluginKey(ref);
    const graph = this.graphs.get(key);
    if (graph === undefined) return Promise.reject(new ModuleLoadError("MODULE_NOT_LOCKED", `Portal 未锁定 Module Graph: ${key}`));
    const existing = this.pending.get(key);
    if (existing !== undefined) return existing;
    const started = this.loadGraph(graph);
    this.pending.set(key, started);
    return started;
  }

  public dispose(): void {
    for (const url of this.objectURLs) URL.revokeObjectURL(url);
    this.objectURLs.clear();
  }

  private async loadGraph(graph: FrontendModuleGraphDescriptor): Promise<unknown> {
    const canonicalDigest = await computeModuleGraphDigest(graph);
    if (canonicalDigest !== graph.digest) throw new ModuleLoadError("MODULE_GRAPH_DIGEST_MISMATCH", `Module Graph digest 不匹配: ${graph.id}`);
    const nodes = new Map(graph.nodes.map((node) => [node.path, node]));
    const bytes = new Map<string, Uint8Array>();
    await Promise.all(graph.nodes.map(async (node) => {
      const response = await this.fetcher(node.url, { credentials: "include", cache: "force-cache" });
      if (!response.ok) throw new ModuleLoadError("MODULE_FETCH_FAILED", `Module Graph 节点获取失败: ${graph.id}/${node.path} (${response.status})`);
      const content = new Uint8Array(await response.arrayBuffer());
      if (content.byteLength !== node.size || await sha256Hex(content) !== node.sha256) throw new ModuleLoadError("MODULE_INTEGRITY_MISMATCH", `Module Graph 节点摘要不匹配: ${graph.id}/${node.path}`);
      const responseDigest = response.headers.get("X-VastPlan-Module-SHA256");
      if (responseDigest !== null && responseDigest !== node.sha256) throw new ModuleLoadError("MODULE_RESPONSE_UNBOUND", `Module Graph 响应未绑定: ${graph.id}/${node.path}`);
      bytes.set(node.path, content);
    }));

    const urls = new Map<string, string>();
    for (const path of topologicalModuleOrder(graph, nodes)) {
      const node = nodes.get(path)!;
      let content = bytes.get(path)!;
      if (node.mediaType === "text/javascript" || node.mediaType === "text/css") {
        let source = new TextDecoder().decode(content);
        for (const dependency of node.dependencies) source = rewriteSpecifier(source, dependency.specifier, urls.get(dependency.path)!);
        content = new TextEncoder().encode(source);
      }
      const url = URL.createObjectURL(new Blob([ownedBuffer(content)], { type: node.mediaType }));
      urls.set(path, url);
      this.objectURLs.add(url);
    }
    return this.importer(urls.get(graph.entry)!, nodes.get(graph.entry)!.url);
  }
}

function rewriteSpecifier(source: string, specifier: string, target: string): string {
  const candidates = specifier.startsWith(".") || specifier.startsWith("/") ? [specifier] : [specifier, `./${specifier}`];
  let rewritten = source;
  for (const candidate of candidates) {
    for (const quote of ['"', "'"]) rewritten = rewritten.replaceAll(`${quote}${candidate}${quote}`, `${quote}${target}${quote}`);
    rewritten = rewritten.replaceAll(`url(${candidate})`, `url(${target})`);
  }
  if (rewritten === source) throw new ModuleLoadError("MODULE_GRAPH_SPECIFIER_MISSING", `构建产物未包含锁定 specifier: ${specifier}`);
  return rewritten;
}


async function importGraphEntry(entryURL: string, sourceURL: string): Promise<unknown> {
  try { return await import(/* @vite-ignore */ entryURL); }
  catch (error) { throw new ModuleLoadError("MODULE_IMPORT_FAILED", `无法导入 Module Graph ${sourceURL}: ${String(error)}`); }
}

function pluginKey(ref: PluginRef): string { return `${ref.id}@${ref.version}/${ref.channel ?? "stable"}`; }
