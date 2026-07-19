import { describe, expect, it, vi } from "vitest";
import { fetchDevelopmentRuntime, PortalDevelopmentError, startPortalDevelopmentUpdates, type DevelopmentEventSource } from "./portal-development";
import type { PortalGenerationManager } from "./portal-generation";

class FakeEventSource implements DevelopmentEventSource {
  public closed = false;
  private readonly listeners = new Map<string, Array<(event: MessageEvent<string>) => void>>();

  public addEventListener(type: string, listener: (event: MessageEvent<string>) => void): void {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener]);
  }

  public emit(type: string, data: unknown): void {
    for (const listener of this.listeners.get(type) ?? []) listener({ data: JSON.stringify(data) } as MessageEvent<string>);
  }

  public close(): void { this.closed = true; }
}

const digest = "a".repeat(64);
const runtime = {
  portal: { revision: 1 },
  modules: [{ id: "com.vastplan.feature", version: "1.0.0", entry: "frontend/dist/index.js", url: `/__vastplan_dev/modules/${digest}.js`, sha256: digest, packageSha256: "b".repeat(64) }],
};

describe("portal development updates", () => {
  it("fetches only a validated development runtime overlay", async () => {
    const fetcher = vi.fn(async () => new Response(JSON.stringify(runtime), { status: 200, headers: { "Content-Type": "application/json" } }));
    const loaded = await fetchDevelopmentRuntime(fetcher, "/__vastplan_dev/runtime", "/operations/settings");
    expect(loaded.modules[0].url).toBe(runtime.modules[0].url);
    expect(fetcher).toHaveBeenCalledWith("/__vastplan_dev/runtime?path=%2Foperations%2Fsettings", { credentials: "same-origin", cache: "no-store" });

    await expect(fetchDevelopmentRuntime(async () => new Response("", { status: 503 }), "/dev", "/")).rejects.toMatchObject({ code: "RUNTIME_FETCH_FAILED" } satisfies Partial<PortalDevelopmentError>);
  });

  it("coalesces generation events, applies them serially, and reports build errors", async () => {
    const source = new FakeEventSource();
    const errors: unknown[] = [];
    let release!: () => void;
    const blocked = new Promise<void>((resolve) => { release = resolve; });
    const replace = vi.fn(async () => { if (replace.mock.calls.length === 1) await blocked; return {} as never; });
    const manager = { replace } as unknown as PortalGenerationManager;
    const fetcher = vi.fn(async () => new Response(JSON.stringify(runtime), { status: 200, headers: { "Content-Type": "application/json" } }));
    const stop = startPortalDevelopmentUpdates({ manager, pathname: () => "/operations", eventSource: source, fetcher, onError: (error) => errors.push(error) });

    source.emit("generation", { generation: 1 });
    source.emit("generation", { generation: 2 });
    await vi.waitFor(() => expect(replace).toHaveBeenCalledTimes(1));
    release();
    await vi.waitFor(() => expect(replace).toHaveBeenCalledTimes(2));
    expect(fetcher).toHaveBeenCalledTimes(2);

    source.emit("build-error", { message: "TS 编译失败" });
    expect(errors[0]).toMatchObject({ code: "BUILD_FAILED", message: "TS 编译失败" });
    stop();
    expect(source.closed).toBe(true);
  });

  it("ignores duplicate or stale generation numbers", async () => {
    const source = new FakeEventSource();
    const replace = vi.fn(async () => ({} as never));
    startPortalDevelopmentUpdates({
      manager: { replace } as unknown as PortalGenerationManager,
      pathname: () => "/",
      eventSource: source,
      fetcher: async () => new Response(JSON.stringify(runtime), { status: 200, headers: { "Content-Type": "application/json" } }),
    });
    source.emit("generation", { generation: 2 });
    await vi.waitFor(() => expect(replace).toHaveBeenCalledOnce());
    source.emit("generation", { generation: 2 });
    source.emit("generation", { generation: 1 });
    await new Promise((resolve) => setTimeout(resolve, 5));
    expect(replace).toHaveBeenCalledOnce();
  });

  it("reloads the whole host instead of importing plugins against stale shared vendors", () => {
    const source = new FakeEventSource();
    const replace = vi.fn(async () => ({} as never));
    const reload = vi.fn();
    const errors: unknown[] = [];
    startPortalDevelopmentUpdates({
      manager: { replace } as unknown as PortalGenerationManager,
      pathname: () => "/operations",
      eventSource: source,
      reload,
      onError: (error) => errors.push(error),
    });
    source.emit("reload", { generation: 3 });
    source.emit("generation", { generation: 4 });
    expect(reload).toHaveBeenCalledOnce();
    expect(source.closed).toBe(true);
    expect(replace).not.toHaveBeenCalled();
    expect(errors).toEqual([]);
  });
});
