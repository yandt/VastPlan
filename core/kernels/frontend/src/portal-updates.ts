import type { PortalGenerationManager } from "./portal-generation";
import type { PortalRuntimeSpec } from "./module-loader";
import type { DevelopmentEventSource } from "./portal-development";

export interface PortalActivationUpdate {
  portalId: string;
  activationId: number;
  mode: "current" | "generation" | "host-epoch";
}

export interface PortalUpdateOptions {
  manager: PortalGenerationManager;
  currentRevision(): number;
  pathname(): string;
  policy: "notify" | "automatic";
  fetchRuntime(pathname: string): Promise<PortalRuntimeSpec>;
  eventSource?: DevelopmentEventSource;
  eventSourceFactory?(url: string): DevelopmentEventSource;
  endpoint?: string;
  reload?(): void;
  onRuntime?(spec: PortalRuntimeSpec): void;
  onNotify?(update: PortalActivationUpdate): void;
  onHostEpoch?(revision: number): void;
  onError?(error: unknown): void;
}

/** Applies only Edge-issued Activation facts; reconnects never infer latest. */
export function startPortalActivationUpdates(options: PortalUpdateOptions): () => void {
  const endpoint = options.endpoint ?? "/v1/portal-updates";
  const separator = endpoint.includes("?") ? "&" : "?";
  const url = `${endpoint}${separator}path=${encodeURIComponent(options.pathname())}&revision=${options.currentRevision()}`;
  const source = options.eventSource ?? (options.eventSourceFactory ?? defaultEventSourceFactory)(url);
  let requested = options.currentRevision();
  let running = false;
  let closed = false;
  let pending: PortalActivationUpdate | undefined;

  const drain = async () => {
    if (running || closed || pending === undefined) return;
    running = true;
    const update = pending;
    pending = undefined;
    try {
      const spec = await options.fetchRuntime(options.pathname());
      if (spec.portal.revision !== update.activationId) throw new PortalUpdateError("ACTIVATION_MISMATCH", "Portal update event and RuntimeSpec revision differ");
      if (update.mode === "host-epoch") {
        await options.manager.preflight(spec);
        options.onHostEpoch?.(spec.portal.revision);
        closed = true;
        source.close();
        (options.reload ?? (() => globalThis.location?.reload()))();
      } else {
        await options.manager.replace(spec);
        options.onRuntime?.(spec);
      }
    } catch (error) {
      options.onError?.(error);
    } finally {
      running = false;
      if (!closed && pending !== undefined) void drain();
    }
  };

  source.addEventListener("activation", (event) => {
    try {
      const value = JSON.parse(event.data) as Partial<PortalActivationUpdate>;
      if (value.mode === "current") { if (Number.isSafeInteger(value.activationId)) requested = Math.max(requested, Number(value.activationId)); return; }
      if ((value.mode !== "generation" && value.mode !== "host-epoch") || !Number.isSafeInteger(value.activationId) || Number(value.activationId) <= requested || typeof value.portalId !== "string") return;
      requested = Number(value.activationId);
      const update = value as PortalActivationUpdate;
      if (options.policy === "notify") { options.onNotify?.(update); return; }
      pending = update;
      void drain();
    } catch (error) { options.onError?.(error); }
  });
  return () => { closed = true; source.close(); };
}

function defaultEventSourceFactory(url: string): DevelopmentEventSource {
  return new EventSource(url, { withCredentials: true });
}

export class PortalUpdateError extends Error {
  public constructor(public readonly code: string, message: string) { super(message); this.name = "PortalUpdateError"; }
}
