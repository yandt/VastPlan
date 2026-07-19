import { describe, expect, it, vi } from "vitest";
import type { FrontendPluginHotLifecycle } from "@vastplan/ui-primitives";
import { PortalGenerationManager, type PortalGenerationDiagnostic } from "./portal-generation";
import type { PortalRuntimeSpec } from "./module-loader";
import type { FrontendPluginModule, PreparedPortal } from "./portal-runtime";

function spec(revision: number): PortalRuntimeSpec {
  return { portal: { revision } as PortalRuntimeSpec["portal"], modules: [] };
}

function prepared(revision: number, hot?: FrontendPluginHotLifecycle, secondHot?: FrontendPluginHotLifecycle): PreparedPortal {
  const module = (lifecycle: FrontendPluginHotLifecycle | undefined): FrontendPluginModule => ({
    provenance: { signed: true, firstParty: true, integrity: `sha256:${revision}` },
    hot: lifecycle,
  });
  return {
    portal: { revision } as PreparedPortal["portal"],
    renderAdapter: {} as PreparedPortal["renderAdapter"],
    shell: {} as PreparedPortal["shell"],
    workbench: {} as PreparedPortal["workbench"],
    pages: [],
    shellContributions: [],
    messageCatalogs: {},
    modules: [
      { ref: { id: "cn.vastplan.feature", version: "1.0.0" }, module: module(hot) },
      ...(secondHot === undefined ? [] : [{ ref: { id: "cn.vastplan.second", version: "1.0.0" }, module: module(secondHot) }]),
    ],
  };
}

describe("PortalGenerationManager", () => {
  it("captures state, restores the candidate, commits once, then disposes the old generation", async () => {
    const order: string[] = [];
    const oldHot: FrontendPluginHotLifecycle = {
      capture(context) { order.push(`capture:${context.generation}`); return { draft: "kept" }; },
      dispose(context) { order.push(`dispose:${context.generation}:${context.signal.aborted}`); },
    };
    const nextHot: FrontendPluginHotLifecycle = {
      restore(state, context) { order.push(`restore:${context.generation}:${JSON.stringify(state)}`); },
    };
    const prepare = vi.fn(async (runtime: PortalRuntimeSpec) => prepared(runtime.portal.revision, runtime.portal.revision === 1 ? oldHot : nextHot));
    const manager = new PortalGenerationManager({ prepare });
    const committed: string[] = [];
    manager.subscribe((generation) => committed.push(generation.id));

    const first = await manager.start(spec(1));
    const second = await manager.replace(spec(2));

    expect(committed).toEqual(["generation-1", "generation-2"]);
    expect(manager.active).toBe(second);
    expect(first.signal.aborted).toBe(true);
    expect(second.signal.aborted).toBe(false);
    expect(order).toEqual([
      "capture:generation-1",
      'restore:generation-2:{"draft":"kept"}',
      "dispose:generation-1:true",
    ]);
  });

  it("keeps the active generation and cleans the candidate when restore fails", async () => {
    const candidateDisposed = vi.fn();
    const manager = new PortalGenerationManager({
      prepare: async (runtime) => prepared(runtime.portal.revision,
        runtime.portal.revision === 1
          ? { capture: () => ({ count: 1 }) }
          : { restore: () => { throw new Error("restore rejected"); }, dispose: candidateDisposed }),
    });
    const first = await manager.start(spec(1));

    await expect(manager.replace(spec(2))).rejects.toThrow("restore rejected");

    expect(manager.active).toBe(first);
    expect(first.signal.aborted).toBe(false);
    expect(candidateDisposed).toHaveBeenCalledOnce();
    expect(candidateDisposed.mock.calls[0][0].signal.aborted).toBe(true);
  });

  it("rejects non-JSON or oversized state before committing the candidate", async () => {
    for (const state of [new Date(), { text: "x".repeat(32) }]) {
      const manager = new PortalGenerationManager({
        stateLimitBytes: 16,
        prepare: async (runtime) => prepared(runtime.portal.revision, runtime.portal.revision === 1 ? { capture: () => state as never } : { restore: vi.fn() }),
      });
      const first = await manager.start(spec(1));
      await expect(manager.replace(spec(2))).rejects.toMatchObject({ code: state instanceof Date ? "STATE_NOT_JSON" : "STATE_TOO_LARGE" });
      expect(manager.active).toBe(first);
    }
  });

  it("serializes overlapping replacements so an older candidate cannot commit late", async () => {
    let releaseSecond!: () => void;
    const secondReady = new Promise<void>((resolve) => { releaseSecond = resolve; });
    const calls: number[] = [];
    const manager = new PortalGenerationManager({
      prepare: async (runtime) => {
        calls.push(runtime.portal.revision);
        if (runtime.portal.revision === 2) await secondReady;
        return prepared(runtime.portal.revision);
      },
    });
    await manager.start(spec(1));
    const second = manager.replace(spec(2));
    const third = manager.replace(spec(3));
    await Promise.resolve();
    expect(calls).toEqual([1, 2]);
    releaseSecond();
    await Promise.all([second, third]);
    expect(calls).toEqual([1, 2, 3]);
    expect(manager.active?.prepared.portal.revision).toBe(3);
  });

  it("reports listener and dispose failures without rolling back an already committed generation", async () => {
    const diagnostics: PortalGenerationDiagnostic[] = [];
    const manager = new PortalGenerationManager({
      disposeTimeoutMs: 1,
      onDiagnostic: (item) => diagnostics.push(item),
      prepare: async (runtime) => prepared(runtime.portal.revision, runtime.portal.revision === 1 ? { dispose: () => new Promise(() => undefined) } : undefined),
    });
    manager.subscribe(() => { throw new Error("observer failed"); });
    await manager.start(spec(1));
    const second = await manager.replace(spec(2));

    expect(manager.active).toBe(second);
    expect(diagnostics.map((item) => item.phase)).toEqual(["listener", "listener", "dispose"]);
    expect(diagnostics[2]).toMatchObject({ pluginID: "cn.vastplan.feature", generation: "generation-1" });
  });

  it("aborts and disposes the active generation on shutdown in reverse plugin order", async () => {
    const order: string[] = [];
    const manager = new PortalGenerationManager({
      prepare: async () => prepared(1, { dispose: () => { order.push("first"); } }, { dispose: () => { order.push("second"); } }),
    });
    const active = await manager.start(spec(1));
    await manager.shutdown();
    expect(active.signal.aborted).toBe(true);
    expect(order).toEqual(["second", "first"]);
    expect(manager.active).toBeUndefined();
  });
});
