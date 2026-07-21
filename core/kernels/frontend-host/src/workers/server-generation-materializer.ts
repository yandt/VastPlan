import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import { dirname, join, resolve, sep } from "node:path";
import type { PortalDeliveryStore } from "../runtime/portal-delivery-store";
import type { FrontendModuleGraph, PortalSpec, ServerRuntimeSpec } from "../runtime/portal-runtime-contract";

export interface MaterializedServerGeneration {
  readonly key: string;
  readonly entryPath: string;
  cleanup(): Promise<void>;
}

export async function materializeServerGeneration(delivery: PortalDeliveryStore, root: string, tenantId: string, spec: PortalSpec, server: ServerRuntimeSpec): Promise<MaterializedServerGeneration | undefined> {
  const graph = selectRuntimeEngineGraph(spec, server);
  if (graph === undefined) return undefined;
  await mkdir(root, { recursive: true, mode: 0o700 });
  const directory = await mkdtemp(join(root, ".candidate-"));
  try {
    await writeFile(join(directory, "package.json"), '{"type":"module"}\n', { mode: 0o400, flag: "wx" });
    for (const node of graph.nodes) {
      const object = await delivery.serverObject(tenantId, spec, node.sha256);
      const filename = containedPath(directory, node.path);
      await mkdir(dirname(filename), { recursive: true, mode: 0o700 });
      await writeFile(filename, object.content, { mode: 0o400, flag: "wx" });
    }
    return Object.freeze({
      key: `${tenantId}/${spec.id}/${spec.revision}/${graph.digest}`,
      entryPath: containedPath(directory, graph.entry),
      cleanup: () => rm(directory, { recursive: true, force: true }),
    });
  } catch (error) {
    await rm(directory, { recursive: true, force: true });
    throw error;
  }
}

function selectRuntimeEngineGraph(spec: PortalSpec, server: ServerRuntimeSpec): FrontendModuleGraph | undefined {
  const engine = typeof spec.runtimeEngine === "object" && spec.runtimeEngine !== null && !Array.isArray(spec.runtimeEngine)
    ? spec.runtimeEngine as Readonly<Record<string, unknown>> : undefined;
  const id = typeof engine?.id === "string" ? engine.id : undefined;
  const version = typeof engine?.version === "string" ? engine.version : undefined;
  if (id === undefined || version === undefined) throw new Error("PortalSpec 缺少 Runtime Engine 精确引用");
  const matches = (server.moduleGraphs ?? []).filter((graph) => graph.id === id && graph.version === version);
  if (matches.length > 1) throw new Error("Portal Server Runtime 包含重复 Runtime Engine 图");
  return matches[0];
}

function containedPath(root: string, relativePath: string): string {
  const path = resolve(root, relativePath);
  if (!path.startsWith(`${root}${sep}`)) throw new Error("Server Module Graph 路径越界");
  return path;
}
