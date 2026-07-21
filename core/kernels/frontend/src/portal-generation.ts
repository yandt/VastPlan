import type { FrontendPluginLifecycleContext, JSONValue } from "@vastplan/ui-primitives";
import { VerifiedFrontendPluginLoader, type ModuleDescriptorPolicy, type ModuleFetcher, type PortalRuntimeSpec } from "./module-loader";
import { PortalRuntime, type PreparedFrontendPlugin, type PreparedPortal } from "./portal-runtime";

const defaultStateLimit = 64 * 1024;
const defaultStateDepth = 32;

export interface PortalGeneration {
  readonly id: string;
  readonly prepared: PreparedPortal;
  readonly signal: AbortSignal;
}

interface OwnedPortalGeneration extends PortalGeneration {
  readonly controller: AbortController;
}

export interface PortalGenerationDiagnostic {
  phase: "listener" | "dispose";
  generation: string;
  pluginID?: string;
  error: unknown;
}

export interface PortalGenerationManagerOptions {
  fetcher?: ModuleFetcher;
  disposeTimeoutMs?: number;
  stateLimitBytes?: number;
  stateDepth?: number;
  descriptorPolicy?: ModuleDescriptorPolicy;
  onDiagnostic?(diagnostic: PortalGenerationDiagnostic): void;
  prepare?(spec: PortalRuntimeSpec, context: { generation: string; signal: AbortSignal; reason: "bootstrap" | "replace" }): Promise<PreparedPortal>;
}

/** Serializes candidate preparation and commits only fully valid Portal generations. */
export class PortalGenerationManager {
  private activeGeneration?: OwnedPortalGeneration;
  private sequence = 0;
  private transaction: Promise<unknown> = Promise.resolve();
  private readonly listeners = new Set<(generation: PortalGeneration) => void>();
  private readonly fetcher: ModuleFetcher;
  private readonly disposeTimeoutMs: number;
  private readonly stateLimitBytes: number;
  private readonly stateDepth: number;

  public constructor(private readonly options: PortalGenerationManagerOptions = {}) {
    this.fetcher = options.fetcher ?? globalThis.fetch.bind(globalThis);
    this.disposeTimeoutMs = options.disposeTimeoutMs ?? 2_000;
    this.stateLimitBytes = options.stateLimitBytes ?? defaultStateLimit;
    this.stateDepth = options.stateDepth ?? defaultStateDepth;
  }

  public get active(): PortalGeneration | undefined { return this.activeGeneration; }

  public subscribe(listener: (generation: PortalGeneration) => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  public start(spec: PortalRuntimeSpec): Promise<PortalGeneration> {
    return this.enqueue(() => this.replaceNow(spec, "bootstrap"));
  }

  public replace(spec: PortalRuntimeSpec): Promise<PortalGeneration> {
    return this.enqueue(() => this.replaceNow(spec, "replace"));
  }

  /** Loads and assembles a Host Epoch candidate without changing the live tree. */
  public preflight(spec: PortalRuntimeSpec): Promise<void> {
    return this.enqueue(async () => {
      const controller = new AbortController();
      const id = `preflight-${++this.sequence}`;
      let candidate: OwnedPortalGeneration | undefined;
      try {
        const prepared = await this.prepare(spec, { generation: id, signal: controller.signal, reason: "replace" });
        candidate = { id, prepared, signal: controller.signal, controller };
      } catch (error) {
        controller.abort(error);
        throw error;
      } finally {
        if (candidate !== undefined) await this.dispose(candidate, "replace");
      }
    });
  }

  public shutdown(): Promise<void> {
    return this.enqueue(async () => {
      const active = this.activeGeneration;
      this.activeGeneration = undefined;
      if (active !== undefined) await this.dispose(active, "shutdown");
    });
  }

  private enqueue<T>(operation: () => Promise<T>): Promise<T> {
    const queued = this.transaction.then(operation, operation);
    this.transaction = queued.then(() => undefined, () => undefined);
    return queued;
  }

  private async replaceNow(spec: PortalRuntimeSpec, reason: "bootstrap" | "replace"): Promise<PortalGeneration> {
    const controller = new AbortController();
    const id = `generation-${++this.sequence}`;
    let candidate: OwnedPortalGeneration | undefined;
    try {
      const prepared = await this.prepare(spec, { generation: id, signal: controller.signal, reason });
      candidate = { id, prepared, signal: controller.signal, controller };
      const state = this.activeGeneration === undefined ? new Map<string, JSONValue | undefined>() : await this.capture(this.activeGeneration);
      await this.restore(candidate, state);
    } catch (error) {
      controller.abort(error);
      if (candidate !== undefined) await this.dispose(candidate, "replace");
      throw error;
    }

    const previous = this.activeGeneration;
    this.activeGeneration = candidate;
    for (const listener of this.listeners) {
      try { listener(candidate); } catch (error) { this.report({ phase: "listener", generation: id, error }); }
    }
    if (previous !== undefined) await this.dispose(previous, "replace");
    return candidate;
  }

  private prepare(spec: PortalRuntimeSpec, context: { generation: string; signal: AbortSignal; reason: "bootstrap" | "replace" }): Promise<PreparedPortal> {
    if (this.options.prepare !== undefined) return this.options.prepare(spec, context);
    const loader = new VerifiedFrontendPluginLoader(spec.modules, this.fetcher, undefined, this.options.descriptorPolicy ?? "production");
    return new PortalRuntime(loader).prepare(spec.portal, context);
  }

  private async capture(generation: OwnedPortalGeneration): Promise<Map<string, JSONValue | undefined>> {
    const state = new Map<string, JSONValue | undefined>();
    for (const plugin of generation.prepared.modules) {
      if (plugin.module.hot?.capture === undefined) continue;
      const captured = await plugin.module.hot.capture(lifecycleContext(plugin, generation, "replace"));
      state.set(plugin.ref.id, validateState(captured, this.stateLimitBytes, this.stateDepth));
    }
    return state;
  }

  private async restore(generation: OwnedPortalGeneration, state: ReadonlyMap<string, JSONValue | undefined>): Promise<void> {
    for (const plugin of generation.prepared.modules) {
      if (plugin.module.hot?.restore === undefined) continue;
      await plugin.module.hot.restore(state.get(plugin.ref.id), lifecycleContext(plugin, generation, "replace"));
    }
  }

  private async dispose(generation: OwnedPortalGeneration, reason: "replace" | "shutdown"): Promise<void> {
    generation.controller.abort(reason);
    for (const plugin of [...generation.prepared.modules].reverse()) {
      const dispose = plugin.module.hot?.dispose;
      if (dispose === undefined) continue;
      try {
        await withTimeout(dispose(lifecycleContext(plugin, generation, reason)), this.disposeTimeoutMs, plugin.ref.id);
      } catch (error) {
        this.report({ phase: "dispose", generation: generation.id, pluginID: plugin.ref.id, error });
      }
    }
  }

  private report(diagnostic: PortalGenerationDiagnostic): void { this.options.onDiagnostic?.(diagnostic); }
}

function lifecycleContext(plugin: PreparedFrontendPlugin, generation: PortalGeneration, reason: "replace" | "shutdown"): Readonly<FrontendPluginLifecycleContext> {
  return Object.freeze({ pluginID: plugin.ref.id, generation: generation.id, signal: generation.signal, reason });
}

function validateState(value: JSONValue | undefined, limitBytes: number, maxDepth: number): JSONValue | undefined {
  if (value === undefined) return undefined;
  assertJSONValue(value, 0, maxDepth);
  const encoded = JSON.stringify(value);
  if (new TextEncoder().encode(encoded).byteLength > limitBytes) {
    throw new PortalGenerationError("STATE_TOO_LARGE", `插件热替换状态超过 ${limitBytes} 字节`);
  }
  return JSON.parse(encoded) as JSONValue;
}

function assertJSONValue(value: unknown, depth: number, maxDepth: number): asserts value is JSONValue {
  if (depth > maxDepth) throw new PortalGenerationError("STATE_TOO_DEEP", `插件热替换状态深度超过 ${maxDepth}`);
  if (value === null || typeof value === "string" || typeof value === "boolean") return;
  if (typeof value === "number" && Number.isFinite(value)) return;
  if (Array.isArray(value)) {
    for (const item of value) assertJSONValue(item, depth + 1, maxDepth);
    return;
  }
  if (typeof value === "object") {
    const prototype = Object.getPrototypeOf(value);
    if (prototype !== Object.prototype && prototype !== null) throw new PortalGenerationError("STATE_NOT_JSON", "插件热替换状态必须是普通 JSON 对象");
    for (const item of Object.values(value)) assertJSONValue(item, depth + 1, maxDepth);
    return;
  }
  throw new PortalGenerationError("STATE_NOT_JSON", "插件热替换状态包含非 JSON 值");
}

async function withTimeout(operation: void | Promise<void>, timeoutMs: number, pluginID: string): Promise<void> {
  let timeout: ReturnType<typeof setTimeout> | undefined;
  try {
    await Promise.race([
      Promise.resolve(operation),
      new Promise<never>((_, reject) => { timeout = setTimeout(() => reject(new PortalGenerationError("DISPOSE_TIMEOUT", `插件清理超时: ${pluginID}`)), timeoutMs); }),
    ]);
  } finally {
    if (timeout !== undefined) clearTimeout(timeout);
  }
}

export class PortalGenerationError extends Error {
  public constructor(public readonly code: string, message: string) {
    super(message);
    this.name = "PortalGenerationError";
  }
}
