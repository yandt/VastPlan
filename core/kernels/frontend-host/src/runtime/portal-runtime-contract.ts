import { createHash } from "node:crypto";

export interface PortalSpec extends Readonly<Record<string, unknown>> {
  readonly revision: number;
  readonly id: string;
  readonly tenantId: string;
  readonly route: string;
  readonly domains?: readonly string[];
  readonly audience?: readonly string[];
}

export interface FrontendModule extends Readonly<Record<string, unknown>> {
  readonly id: string;
  readonly version: string;
  readonly channel?: string;
  readonly entry: string;
  readonly url: string;
  readonly sha256: string;
  readonly packageSha256: string;
  readonly mediaType?: string;
  readonly deferred?: boolean;
}

export interface FrontendModuleNode extends Readonly<Record<string, unknown>> {
  readonly path: string;
  readonly url: string;
  readonly sha256: string;
  readonly size: number;
  readonly mediaType: string;
}

export interface FrontendModuleGraph extends Readonly<Record<string, unknown>> {
  readonly id: string;
  readonly version: string;
  readonly channel?: string;
  readonly entry: string;
  readonly packageSha256: string;
  readonly deferred?: boolean;
  readonly nodes: readonly FrontendModuleNode[];
}

export interface PortalRuntimeSpec extends Readonly<Record<string, unknown>> {
  readonly portal: PortalSpec;
  readonly modules?: readonly FrontendModule[];
  readonly moduleGraphs?: readonly FrontendModuleGraph[];
}

export interface FrontendObjectDescriptor {
  readonly id: string;
  readonly version: string;
  readonly channel?: string;
  readonly url: string;
  readonly sha256: string;
  readonly packageSha256: string;
  readonly mediaType: string;
  readonly deferred: boolean;
}

export class PortalRuntimeContractError extends Error {
  public constructor(message: string) {
    super(message);
    this.name = "PortalRuntimeContractError";
  }
}

export function parseDeliverySnapshot(raw: Uint8Array, expected: PortalSpec): PortalRuntimeSpec {
  let value: unknown;
  try { value = JSON.parse(new TextDecoder().decode(raw)) as unknown; }
  catch { throw new PortalRuntimeContractError("Portal 交付快照 JSON 无效"); }
  const snapshot = objectValue(value, "Portal 交付快照必须是对象");
  const runtime = objectValue(snapshot.runtime, "Portal RuntimeSpec 缺失") as PortalRuntimeSpec;
  const portal = objectValue(runtime.portal, "Portal RuntimeSpec 缺少 portal") as PortalSpec;
  if (!digest(snapshot.specSha256) || snapshot.specSha256 !== portalSpecDigest(expected)) {
    throw new PortalRuntimeContractError("Portal 交付快照与活动解析锁不一致");
  }
  if (stableJSON(portal) !== stableJSON(expected)) {
    throw new PortalRuntimeContractError("Portal RuntimeSpec 与活动 Portal 不一致");
  }
  validateRuntime(runtime);
  return structuredClone(runtime);
}

export function portalSpecDigest(spec: PortalSpec): string {
  return createHash("sha256").update(JSON.stringify(spec)).digest("hex");
}

export function runtimeObjects(runtime: PortalRuntimeSpec): readonly FrontendObjectDescriptor[] {
  const modules = (runtime.modules ?? []).map((module) => ({
    id: module.id, version: module.version, ...(module.channel === undefined ? {} : { channel: module.channel }),
    url: module.url, sha256: module.sha256, packageSha256: module.packageSha256,
    mediaType: module.mediaType ?? "text/javascript", deferred: module.deferred === true,
  }));
  const graphNodes = (runtime.moduleGraphs ?? []).flatMap((graph) => graph.nodes.map((node) => ({
    id: graph.id, version: graph.version, ...(graph.channel === undefined ? {} : { channel: graph.channel }),
    url: node.url, sha256: node.sha256, packageSha256: graph.packageSha256,
    mediaType: node.mediaType, deferred: graph.deferred === true,
  })));
  return Object.freeze([...modules, ...graphNodes]);
}

export function recoveryRuntime(runtime: PortalRuntimeSpec, activeRevision: number, fallbackRevision: number): PortalRuntimeSpec {
  const cloned = structuredClone(runtime) as PortalRuntimeSpec;
  const prefix = `/v1/portal-recovery-modules/${activeRevision}/${fallbackRevision}/`;
  for (const module of cloned.modules ?? []) (module as { url: string }).url = prefix + module.url.slice(module.url.lastIndexOf("/") + 1);
  for (const graph of cloned.moduleGraphs ?? []) {
    for (const node of graph.nodes) (node as { url: string }).url = prefix + node.url.slice(node.url.lastIndexOf("/") + 1);
  }
  return cloned;
}

function validateRuntime(runtime: PortalRuntimeSpec): void {
  if (!Number.isSafeInteger(runtime.portal.revision) || runtime.portal.revision < 1) throw new PortalRuntimeContractError("Portal RuntimeSpec revision 无效");
  if (!Array.isArray(runtime.modules ?? []) || !Array.isArray(runtime.moduleGraphs ?? [])) throw new PortalRuntimeContractError("Portal RuntimeSpec 模块列表无效");
  for (const module of runtime.modules ?? []) validateDescriptor(module, module.mediaType ?? "text/javascript", runtime.portal.revision);
  for (const graph of runtime.moduleGraphs ?? []) {
    if (!nonempty(graph.id) || !nonempty(graph.version) || !nonempty(graph.entry) || !digest(graph.packageSha256) || !Array.isArray(graph.nodes)) {
      throw new PortalRuntimeContractError("Portal Module Graph 无效");
    }
    for (const node of graph.nodes) {
      if (!nonempty(node.path) || !Number.isSafeInteger(node.size) || node.size < 0) throw new PortalRuntimeContractError("Portal Module Graph 节点无效");
      validateDescriptor({ ...node, id: graph.id, version: graph.version, packageSha256: graph.packageSha256, entry: node.path }, node.mediaType, runtime.portal.revision);
    }
  }
}

function validateDescriptor(value: Readonly<Record<string, unknown>>, mediaType: unknown, portalRevision: number): void {
  const allowedMediaTypes = new Set(["text/javascript", "text/css", "application/json", "application/wasm", "application/octet-stream", "image/svg+xml", "font/woff2"]);
  if (!nonempty(value.id) || !nonempty(value.version) || !nonempty(value.entry) || !nonempty(value.url)
    || !digest(value.sha256) || !digest(value.packageSha256) || !nonempty(mediaType) || !allowedMediaTypes.has(mediaType)) {
    throw new PortalRuntimeContractError("Portal 内容对象描述符无效");
  }
  const revision = (value.url as string).match(/^\/v1\/portal-modules\/([1-9][0-9]*)\/([0-9a-f]{64})\.(js|css|json|wasm|bin)$/);
  const expectedExtension = ({ "text/javascript": "js", "text/css": "css", "application/json": "json", "application/wasm": "wasm" } as Readonly<Record<string, string>>)[mediaType] ?? "bin";
  if (revision === null || Number(revision[1]) !== portalRevision || revision[2] !== value.sha256 || revision[3] !== expectedExtension) {
    throw new PortalRuntimeContractError("Portal 内容对象 URL 未绑定 revision、摘要和媒体类型");
  }
}

function objectValue(value: unknown, message: string): Readonly<Record<string, unknown>> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new PortalRuntimeContractError(message);
  return value as Readonly<Record<string, unknown>>;
}

function nonempty(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
function digest(value: unknown): value is string { return typeof value === "string" && /^[0-9a-f]{64}$/.test(value); }

function stableJSON(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(stableJSON).join(",")}]`;
  if (typeof value === "object" && value !== null) {
    const object = value as Readonly<Record<string, unknown>>;
    return `{${Object.keys(object).sort().map((key) => `${JSON.stringify(key)}:${stableJSON(object[key])}`).join(",")}}`;
  }
  return JSON.stringify(value);
}
