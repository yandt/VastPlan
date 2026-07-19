import { parseDevelopmentRuntimeSpec, type ModuleFetcher, type PortalRuntimeSpec } from "./module-loader";
import type { PortalGenerationManager } from "./portal-generation";

export interface DevelopmentEventSource {
  addEventListener(type: string, listener: (event: MessageEvent<string>) => void): void;
  close(): void;
}

export interface PortalDevelopmentOptions {
  manager: PortalGenerationManager;
  pathname(): string;
  fetcher?: ModuleFetcher;
  eventSource?: DevelopmentEventSource;
  eventSourceFactory?(url: string): DevelopmentEventSource;
  eventsEndpoint?: string;
  runtimeEndpoint?: string;
  onError?(error: unknown): void;
}

/** Coalesces local build events and never lets an older update overtake a newer one. */
export function startPortalDevelopmentUpdates(options: PortalDevelopmentOptions): () => void {
  const fetcher = options.fetcher ?? globalThis.fetch.bind(globalThis);
  const eventsEndpoint = options.eventsEndpoint ?? "/__vastplan_dev/events";
  const runtimeEndpoint = options.runtimeEndpoint ?? "/__vastplan_dev/runtime";
  const source = options.eventSource ?? (options.eventSourceFactory ?? defaultEventSourceFactory)(eventsEndpoint);
  let requested = 0;
  let applied = 0;
  let running = false;
  let closed = false;

  const drain = async () => {
    if (running || closed) return;
    running = true;
    try {
      while (!closed && applied < requested) {
        const target = requested;
        const spec = await fetchDevelopmentRuntime(fetcher, runtimeEndpoint, options.pathname());
        await options.manager.replace(spec);
        applied = target;
      }
    } catch (error) {
      applied = requested;
      options.onError?.(error);
    } finally {
      running = false;
      if (!closed && applied < requested) void drain();
    }
  };

  source.addEventListener("generation", (event) => {
    try {
      const payload = JSON.parse(event.data) as { generation?: unknown };
      if (!Number.isSafeInteger(payload.generation) || Number(payload.generation) <= requested) return;
      requested = Number(payload.generation);
      void drain();
    } catch (error) {
      options.onError?.(error);
    }
  });
  source.addEventListener("build-error", (event) => {
    try {
      const payload = JSON.parse(event.data) as { message?: unknown };
      options.onError?.(new PortalDevelopmentError("BUILD_FAILED", typeof payload.message === "string" ? payload.message : "前端插件构建失败"));
    } catch (error) {
      options.onError?.(error);
    }
  });

  return () => { closed = true; source.close(); };
}

export async function fetchDevelopmentRuntime(fetcher: ModuleFetcher, endpoint: string, pathname: string): Promise<PortalRuntimeSpec> {
  const separator = endpoint.includes("?") ? "&" : "?";
  const response = await fetcher(`${endpoint}${separator}path=${encodeURIComponent(pathname)}`, { credentials: "same-origin", cache: "no-store" });
  if (!response.ok) throw new PortalDevelopmentError("RUNTIME_FETCH_FAILED", `开发态 Portal 运行描述获取失败 (${response.status})`);
  return parseDevelopmentRuntimeSpec(await response.json());
}

function defaultEventSourceFactory(url: string): DevelopmentEventSource {
  return new EventSource(url, { withCredentials: true });
}

export class PortalDevelopmentError extends Error {
  public constructor(public readonly code: string, message: string) {
    super(message);
    this.name = "PortalDevelopmentError";
  }
}
