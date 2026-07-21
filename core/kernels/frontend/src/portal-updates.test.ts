import { describe, expect, it, vi } from "vitest";
import type { DevelopmentEventSource } from "./portal-development";
import type { PortalGenerationManager } from "./portal-generation";
import { startPortalActivationUpdates } from "./portal-updates";
import type { PortalRuntimeSpec } from "./module-loader";

class FakeEventSource implements DevelopmentEventSource {
  public closed = false;
  private readonly listeners = new Map<string, Array<(event: MessageEvent<string>) => void>>();
  public addEventListener(type: string, listener: (event: MessageEvent<string>) => void): void { this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener]); }
  public emit(type: string, value: unknown): void { for (const listener of this.listeners.get(type) ?? []) listener({ data: JSON.stringify(value) } as MessageEvent<string>); }
  public close(): void { this.closed = true; }
}

const runtime = (revision: number) => ({ portal: { revision }, modules: [] }) as unknown as PortalRuntimeSpec;

describe("Portal Activation updates", () => {
  it("atomically replaces compatible generations and ignores the current handshake", async () => {
    const source = new FakeEventSource();
    const replace = vi.fn(async () => ({} as never));
    const onRuntime = vi.fn();
    const fetchRuntime = vi.fn(async () => runtime(2));
    startPortalActivationUpdates({ manager: { replace } as unknown as PortalGenerationManager, currentRevision: () => 1, pathname: () => "/operations", policy: "automatic", eventSource: source, fetchRuntime, onRuntime });
    source.emit("activation", { portalId: "operations", activationId: 1, mode: "current" });
    source.emit("activation", { portalId: "operations", activationId: 2, mode: "generation" });
    await vi.waitFor(() => expect(replace).toHaveBeenCalledOnce());
    expect(onRuntime).toHaveBeenCalledWith(expect.objectContaining({ portal: expect.objectContaining({ revision: 2 }) }));
  });

  it("requests a Portal-host catch-up from the exact active revision", () => {
    const source = new FakeEventSource();
    const factory = vi.fn(() => source);
    startPortalActivationUpdates({ manager: {} as PortalGenerationManager, currentRevision: () => 11, pathname: () => "/operations/settings", policy: "notify", eventSourceFactory: factory, fetchRuntime: async () => runtime(11) });
    expect(factory).toHaveBeenCalledWith("/v1/portal-updates?path=%2Foperations%2Fsettings&revision=11");
  });

  it("preflights a Host Epoch before closing the stream and reloading", async () => {
    const source = new FakeEventSource();
    const preflight = vi.fn(async () => undefined);
    const reload = vi.fn();
    const onHostEpoch = vi.fn();
    startPortalActivationUpdates({ manager: { preflight } as unknown as PortalGenerationManager, currentRevision: () => 3, pathname: () => "/", policy: "automatic", eventSource: source, fetchRuntime: async () => runtime(4), reload, onHostEpoch });
    source.emit("activation", { portalId: "operations", activationId: 4, mode: "host-epoch" });
    await vi.waitFor(() => expect(reload).toHaveBeenCalledOnce());
    expect(preflight).toHaveBeenCalledOnce();
    expect(onHostEpoch).toHaveBeenCalledWith(4);
    expect(source.closed).toBe(true);
  });

  it("notifies without fetching or mutating when policy is notify", () => {
    const source = new FakeEventSource();
    const onNotify = vi.fn();
    const fetchRuntime = vi.fn(async () => runtime(8));
    startPortalActivationUpdates({ manager: {} as PortalGenerationManager, currentRevision: () => 7, pathname: () => "/", policy: "notify", eventSource: source, fetchRuntime, onNotify });
    source.emit("activation", { portalId: "operations", activationId: 8, mode: "generation" });
    expect(onNotify).toHaveBeenCalledOnce();
    expect(fetchRuntime).not.toHaveBeenCalled();
  });
});
