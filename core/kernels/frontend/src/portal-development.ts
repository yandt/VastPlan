import type { ModuleFetcher, PortalRuntimeSpec } from "./module-loader";
import { parseRuntimeSpec } from "./module-runtime-spec";
import type { FrontendRuntimeProtocol } from "./frontend-runtime-protocol";
import type { PortalGenerationManager } from "./portal-generation";
import type { PortalRuntimeSource } from "./portal-runtime-source";

export interface DevelopmentEventSource {
  addEventListener(type: string, listener: (event: MessageEvent<string>) => void): void;
  close(): void;
}

export interface PortalDevelopmentOptions {
  manager: PortalGenerationManager;
  runtimeSource: PortalRuntimeSource;
  pathname(): string;
  eventSource?: DevelopmentEventSource;
  eventSourceFactory?(url: string): DevelopmentEventSource;
  eventsEndpoint?: string;
  epochStore?: DevelopmentEpochStore;
  reload?(): void;
  onError?(error: unknown): void;
  onRuntime?(spec: PortalRuntimeSpec): void;
}

export interface DevelopmentEpochStore {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

const developmentEpochKey = "vastplan.portal.development.epoch";

/** Coalesces local build events and never lets an older update overtake a newer one. */
export function startPortalDevelopmentUpdates(options: PortalDevelopmentOptions): () => void {
  const eventsEndpoint = options.eventsEndpoint ?? "/__vastplan_dev/events";
  const source = options.eventSource ?? (options.eventSourceFactory ?? defaultEventSourceFactory)(eventsEndpoint);
  const epochStore = options.epochStore ?? sessionEpochStore();
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
        const spec = await options.runtimeSource.read(options.pathname());
        await options.manager.replace(spec);
        options.onRuntime?.(spec);
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
  source.addEventListener("hello", (event) => {
    if (closed) return;
    try {
      const payload = JSON.parse(event.data) as { epoch?: unknown };
      if (typeof payload.epoch !== "string" || payload.epoch.length === 0) throw new PortalDevelopmentError("EPOCH_INVALID", "开发态 Portal epoch 无效");
      const previous = epochStore?.getItem(developmentEpochKey);
      epochStore?.setItem(developmentEpochKey, payload.epoch);
      if (previous === null || previous === undefined || previous === payload.epoch) return;
      closed = true;
      source.close();
      (options.reload ?? (() => globalThis.location?.reload()))();
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
  source.addEventListener("reload", (event) => {
    if (closed) return;
    try {
      const payload = JSON.parse(event.data) as { generation?: unknown };
      if (!Number.isSafeInteger(payload.generation) || Number(payload.generation) <= 0) throw new PortalDevelopmentError("RELOAD_INVALID", "开发态 Portal reload 事件无效");
      closed = true;
      source.close();
      (options.reload ?? (() => globalThis.location?.reload()))();
    } catch (error) {
      options.onError?.(error);
    }
  });

  return () => { closed = true; source.close(); };
}

export async function fetchDevelopmentRuntime(fetcher: ModuleFetcher, endpoint: string, pathname: string, protocol: FrontendRuntimeProtocol): Promise<PortalRuntimeSpec> {
  const separator = endpoint.includes("?") ? "&" : "?";
  const url = `${endpoint}${separator}path=${encodeURIComponent(pathname)}`;
  let retry = 0;
  for (;;) {
    const response = await fetcher(url, { credentials: "same-origin", cache: "no-store" });
    if (response.ok) return parseRuntimeSpec(await response.json(), protocol);
    const delay = protocol.runtimeRetryDelay(response.status, retry);
    if (delay === undefined) throw new PortalDevelopmentError("RUNTIME_FETCH_FAILED", `开发态 Portal 运行描述获取失败 (${response.status})`);
    retry++;
    await new Promise((resolve) => setTimeout(resolve, delay));
  }
}

function defaultEventSourceFactory(url: string): DevelopmentEventSource {
  return new EventSource(url, { withCredentials: true });
}

function sessionEpochStore(): DevelopmentEpochStore | undefined {
  try { return globalThis.sessionStorage; } catch { return undefined; }
}

export class PortalDevelopmentError extends Error {
  public constructor(public readonly code: string, message: string) {
    super(message);
    this.name = "PortalDevelopmentError";
  }
}
