import type { FrontendServerRenderInput, FrontendServerRenderResult } from "@vastplan/frontend-engine-contract";
import type { PortalDeliveryStore } from "../runtime/portal-delivery-store";
import type { PortalSpec, ServerRuntimeSpec } from "../runtime/portal-runtime-contract";
import { ServerGeneration } from "./server-generation";
import { materializeServerGeneration, type MaterializedServerGeneration } from "./server-generation-materializer";

interface ManagedGeneration {
  readonly slot: string;
  readonly runtime: ServerGeneration;
  readonly materialized: MaterializedServerGeneration;
}

export interface PreparedServerGeneration {
  readonly slot: string;
  readonly key: string;
}

/** Owns Server Worker candidates; only commit() may change the active generation. */
export class ServerGenerationManager {
  private readonly current = new Map<string, ManagedGeneration>();
  private readonly candidates = new Map<string, ManagedGeneration>();
  private readonly preparing = new Map<string, Promise<ManagedGeneration | undefined>>();
  private readonly retiring = new Set<Promise<void>>();

  public constructor(private readonly delivery: PortalDeliveryStore, private readonly generationRoot: string, private readonly workerScript: string) {}

  public async prepare(tenantId: string, spec: PortalSpec, healthInput: FrontendServerRenderInput): Promise<PreparedServerGeneration | undefined> {
    const server = await this.delivery.serverRuntime(tenantId, spec);
    const identity = generationIdentity(tenantId, spec, server);
    if (identity === undefined) return undefined;
    if (this.current.get(identity.slot)?.runtime.key === identity.key || this.candidates.has(identity.key)) return identity;
    const inFlight = this.preparing.get(identity.key);
    if (inFlight !== undefined) {
      const prepared = await inFlight;
      return prepared === undefined ? undefined : identity;
    }
    const candidate = this.prepareCandidate(identity.slot, tenantId, spec, server, healthInput);
    this.preparing.set(identity.key, candidate);
    try {
      const prepared = await candidate;
      return prepared === undefined ? undefined : identity;
    } finally {
      this.preparing.delete(identity.key);
    }
  }

  public isCommitted(prepared: PreparedServerGeneration): boolean {
    return this.current.get(prepared.slot)?.runtime.key === prepared.key;
  }

  public commit(prepared: PreparedServerGeneration): void {
    if (this.isCommitted(prepared)) return;
    const candidate = this.candidates.get(prepared.key);
    if (candidate === undefined || candidate.slot !== prepared.slot) throw new Error("Server Generation 候选不存在或已过期");
    this.candidates.delete(prepared.key);
    const previous = this.current.get(prepared.slot);
    this.current.set(prepared.slot, candidate);
    if (previous !== undefined) this.retire(previous);
  }

  public async discard(prepared: PreparedServerGeneration): Promise<void> {
    const candidate = this.candidates.get(prepared.key);
    if (candidate === undefined || candidate.slot !== prepared.slot) return;
    this.candidates.delete(prepared.key);
    await disposeManaged(candidate);
  }

  /** SSR never prepares or commits implicitly; it renders only an exact active generation. */
  public async renderActive(tenantId: string, spec: PortalSpec, input: FrontendServerRenderInput): Promise<FrontendServerRenderResult | undefined> {
    const server = await this.delivery.serverRuntime(tenantId, spec);
    const identity = generationIdentity(tenantId, spec, server);
    if (identity === undefined) return undefined;
    const active = this.current.get(identity.slot);
    if (active?.runtime.key !== identity.key) return undefined;
    return active.runtime.render(input);
  }

  public async shutdown(): Promise<void> {
    const generations = new Set([...this.current.values(), ...this.candidates.values()]);
    this.current.clear();
    this.candidates.clear();
    await Promise.allSettled([...generations].map((generation) => disposeManaged(generation)).concat([...this.retiring]));
  }

  private async prepareCandidate(slot: string, tenantId: string, spec: PortalSpec, server: ServerRuntimeSpec, healthInput: FrontendServerRenderInput): Promise<ManagedGeneration | undefined> {
    const materialized = await materializeServerGeneration(this.delivery, this.generationRoot, tenantId, spec, server);
    if (materialized === undefined) return undefined;
    let runtime: ServerGeneration | undefined;
    try {
      runtime = await ServerGeneration.start(materialized.key, this.workerScript, materialized.entryPath);
      await runtime.render(healthInput);
      const candidate = { slot, runtime, materialized };
      this.candidates.set(materialized.key, candidate);
      return candidate;
    } catch (error) {
      await runtime?.dispose().catch(() => undefined);
      await materialized.cleanup();
      throw error;
    }
  }

  private retire(generation: ManagedGeneration): void {
    const retiring = disposeManaged(generation);
    this.retiring.add(retiring);
    void retiring.finally(() => this.retiring.delete(retiring)).catch(() => undefined);
  }
}

async function disposeManaged(generation: ManagedGeneration): Promise<void> {
  try { await generation.runtime.dispose(); }
  finally { await generation.materialized.cleanup(); }
}

function generationIdentity(tenantId: string, spec: PortalSpec, server: ServerRuntimeSpec): PreparedServerGeneration | undefined {
  const engine = record(spec.runtimeEngine);
  const id = requiredString(engine?.id), version = requiredString(engine?.version);
  if (id === undefined || version === undefined) throw new Error("PortalSpec 缺少 Runtime Engine 精确引用");
  const matches = (server.moduleGraphs ?? []).filter((graph) => graph.id === id && graph.version === version);
  if (matches.length > 1) throw new Error("Portal Server Runtime 包含重复 Runtime Engine 图");
  const graph = matches[0];
  return graph === undefined ? undefined : Object.freeze({ slot: `${tenantId}/${spec.id}`, key: `${tenantId}/${spec.id}/${spec.revision}/${graph.digest}` });
}

function record(value: unknown): Readonly<Record<string, unknown>> | undefined {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Readonly<Record<string, unknown>> : undefined;
}

function requiredString(value: unknown): string | undefined { return typeof value === "string" && value !== "" ? value : undefined; }
