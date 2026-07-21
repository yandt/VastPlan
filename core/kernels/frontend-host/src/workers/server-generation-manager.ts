import type { FrontendServerRenderInput, FrontendServerRenderResult } from "@vastplan/frontend-engine-contract";
import type { PortalDeliveryStore } from "../runtime/portal-delivery-store";
import type { PortalSpec } from "../runtime/portal-runtime-contract";
import { ServerGeneration } from "./server-generation";
import { materializeServerGeneration, type MaterializedServerGeneration } from "./server-generation-materializer";

interface ManagedGeneration {
  readonly runtime: ServerGeneration;
  readonly materialized: MaterializedServerGeneration;
}

export class ServerGenerationManager {
  private readonly current = new Map<string, ManagedGeneration>();
  private readonly preparing = new Map<string, Promise<ManagedGeneration | undefined>>();
	private readonly retiring = new Set<Promise<void>>();

  public constructor(private readonly delivery: PortalDeliveryStore, private readonly generationRoot: string, private readonly workerScript: string) {}

  public async render(tenantId: string, spec: PortalSpec, input: FrontendServerRenderInput): Promise<FrontendServerRenderResult | undefined> {
    const slot = `${tenantId}/${spec.id}`;
    const generation = await this.ensure(slot, tenantId, spec, input);
    return generation?.runtime.render(input);
  }

  public async shutdown(): Promise<void> {
    const generations = [...this.current.values()];
    this.current.clear();
		await Promise.allSettled([...generations.map((generation) => disposeManaged(generation)), ...this.retiring]);
  }

  private async ensure(slot: string, tenantId: string, spec: PortalSpec, healthInput: FrontendServerRenderInput): Promise<ManagedGeneration | undefined> {
    const server = await this.delivery.serverRuntime(tenantId, spec);
    const expected = (server.moduleGraphs ?? []).find((graph) => graph.id === runtimeEngineID(spec) && graph.version === runtimeEngineVersion(spec));
    if (expected === undefined) return undefined;
    const key = `${tenantId}/${spec.id}/${spec.revision}/${expected.digest}`;
    const active = this.current.get(slot);
    if (active?.runtime.key === key) return active;
    const inFlight = this.preparing.get(key);
    if (inFlight !== undefined) return inFlight;
    const candidate = this.prepareAndCommit(slot, tenantId, spec, healthInput);
    this.preparing.set(key, candidate);
    try { return await candidate; }
    finally { this.preparing.delete(key); }
  }

  private async prepareAndCommit(slot: string, tenantId: string, spec: PortalSpec, healthInput: FrontendServerRenderInput): Promise<ManagedGeneration | undefined> {
    const server = await this.delivery.serverRuntime(tenantId, spec);
    const materialized = await materializeServerGeneration(this.delivery, this.generationRoot, tenantId, spec, server);
    if (materialized === undefined) return undefined;
    let runtime: ServerGeneration | undefined;
    try {
      runtime = await ServerGeneration.start(materialized.key, this.workerScript, materialized.entryPath);
      await runtime.render(healthInput);
      const candidate = { runtime, materialized };
      const previous = this.current.get(slot);
      this.current.set(slot, candidate);
			if (previous !== undefined) this.retire(previous);
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

function runtimeEngineID(spec: PortalSpec): string { return runtimeEngineField(spec, "id"); }
function runtimeEngineVersion(spec: PortalSpec): string { return runtimeEngineField(spec, "version"); }
function runtimeEngineField(spec: PortalSpec, field: string): string {
  if (typeof spec.runtimeEngine !== "object" || spec.runtimeEngine === null || Array.isArray(spec.runtimeEngine)) throw new Error("PortalSpec 缺少 Runtime Engine 精确引用");
  const value = (spec.runtimeEngine as Readonly<Record<string, unknown>>)[field];
  if (typeof value !== "string" || value === "") throw new Error("PortalSpec 缺少 Runtime Engine 精确引用");
  return value;
}
