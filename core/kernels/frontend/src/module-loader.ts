import type { DesignSystemAdapter } from "@vastplan/portal-ui";
import type { FrontendPluginLoader, FrontendPluginModule, PluginRef, PortalSpec } from "./portal-runtime";

export interface FrontendModuleDescriptor extends PluginRef {
  entry: string;
  url: string;
  sha256: string;
  packageSha256: string;
}

export interface PortalRuntimeSpec {
  portal: PortalSpec;
  modules: FrontendModuleDescriptor[];
}

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

  public constructor(
    descriptors: readonly FrontendModuleDescriptor[],
    private readonly fetcher: ModuleFetcher = globalThis.fetch.bind(globalThis),
    private readonly importer: ModuleImporter = importModuleBytes,
  ) {
    for (const descriptor of descriptors) {
      validateDescriptor(descriptor);
      const key = moduleKey(descriptor);
      if (this.modules.has(key)) {
        throw new ModuleLoadError("MODULE_DESCRIPTOR_DUPLICATE", `前端模块描述重复: ${key}`);
      }
      this.modules.set(key, { ...descriptor });
    }
  }

  public load(ref: PluginRef): Promise<FrontendPluginModule> {
    const key = moduleKey(ref);
    const descriptor = this.modules.get(key);
    if (descriptor === undefined) {
      return Promise.reject(new ModuleLoadError("MODULE_NOT_LOCKED", `Portal 运行描述未锁定模块: ${key}`));
    }
    const existing = this.pending.get(key);
    if (existing !== undefined) return existing;
    const started = this.loadVerified(descriptor);
    this.pending.set(key, started);
    return started;
  }

  private async loadVerified(descriptor: FrontendModuleDescriptor): Promise<FrontendPluginModule> {
    const response = await this.fetcher(descriptor.url, { credentials: "same-origin", cache: "no-store" });
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
    return normalizeModule(namespace, descriptor);
  }
}

export class ModuleLoadError extends Error {
  public constructor(public readonly code: string, message: string) {
    super(message);
    this.name = "ModuleLoadError";
  }
}

export function parsePortalRuntimeSpec(value: unknown): PortalRuntimeSpec {
  if (!isRecord(value) || !isRecord(value.portal) || !Array.isArray(value.modules)) {
    throw new ModuleLoadError("RUNTIME_SPEC_INVALID", "Portal RuntimeSpec 结构无效");
  }
  const modules = value.modules.map((item) => {
    if (!isRecord(item)) throw new ModuleLoadError("RUNTIME_SPEC_INVALID", "Portal 模块描述无效");
    const descriptor = item as unknown as FrontendModuleDescriptor;
    validateDescriptor(descriptor);
    return { ...descriptor };
  });
  return { portal: value.portal as unknown as PortalSpec, modules };
}

function normalizeModule(namespace: unknown, descriptor: FrontendModuleDescriptor): FrontendPluginModule {
  if (!isRecord(namespace)) {
    throw new ModuleLoadError("MODULE_EXPORT_INVALID", `前端模块没有对象导出: ${descriptor.id}`);
  }
  const exported = isRecord(namespace.default) ? namespace.default : namespace;
  const provenance = { signed: true, firstParty: true, integrity: `sha256:${descriptor.sha256}` };
  if (exported.id === "ui.design-system" && typeof exported.Provider === "function") {
    return { provenance, designSystem: exported as unknown as DesignSystemAdapter };
  }
  if (typeof exported.register === "function") {
    return { provenance, register: exported.register.bind(exported) as FrontendPluginModule["register"] };
  }
  throw new ModuleLoadError("MODULE_EXPORT_INVALID", `前端模块未导出设计系统或 register: ${descriptor.id}`);
}

async function sha256Hex(bytes: Uint8Array): Promise<string> {
  const digest = await globalThis.crypto.subtle.digest("SHA-256", ownedBuffer(bytes));
  return [...new Uint8Array(digest)].map((value) => value.toString(16).padStart(2, "0")).join("");
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

function ownedBuffer(source: Uint8Array): ArrayBuffer {
  const copy = new Uint8Array(source.byteLength);
  copy.set(source);
  return copy.buffer;
}

function validateDescriptor(descriptor: FrontendModuleDescriptor): void {
  const governedURL = descriptor.url.startsWith("/v1/portal-modules/") || descriptor.url.startsWith("/v1/portal-recovery-modules/");
  if (!descriptor.id || !descriptor.version || !governedURL ||
      !/^[a-f0-9]{64}$/.test(descriptor.sha256) || !/^[a-f0-9]{64}$/.test(descriptor.packageSha256) ||
      (!descriptor.entry.endsWith(".js") && !descriptor.entry.endsWith(".mjs"))) {
    throw new ModuleLoadError("MODULE_DESCRIPTOR_INVALID", `前端模块描述无效: ${descriptor.id || "unknown"}`);
  }
}

function moduleKey(ref: PluginRef): string {
  return `${ref.id}@${ref.version}/${ref.channel ?? "stable"}`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
