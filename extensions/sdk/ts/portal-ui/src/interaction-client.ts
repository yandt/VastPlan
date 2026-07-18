import type { InteractionRecord, InteractionResponse } from "@vastplan/ui-contract";

export interface PortalFetchResponse {
  ok: boolean;
  status: number;
  json(): Promise<unknown>;
}

export interface PortalFetch {
  (input: string, init?: { method?: string; headers?: Record<string, string>; body?: string; credentials?: "include" }): Promise<PortalFetchResponse>;
}

export interface PortalInteractionClientOptions {
  fetch: PortalFetch;
  /** Obtained from Edge /v1/csrf and held by the Portal shell. */
  csrfToken(): string | undefined;
  basePath?: string;
}

/** Web Adapter for the Broker HTTP boundary; identity and surface remain server-owned. */
export class PortalInteractionClient {
  private readonly basePath: string;

  public constructor(private readonly options: PortalInteractionClientOptions) {
    this.basePath = options.basePath ?? "/v1/interactions";
  }

  public list(): Promise<InteractionRecord[]> { return this.call<InteractionRecord[]>(this.basePath, { method: "GET" }); }
  public get(id: string): Promise<InteractionRecord> { return this.call<InteractionRecord>(`${this.basePath}/${encodeURIComponent(id)}`, { method: "GET" }); }
  public present(id: string): Promise<InteractionRecord> { return this.mutate<InteractionRecord>(`${this.basePath}/${encodeURIComponent(id)}/present`, {}); }
  public respond(id: string, response: InteractionResponse): Promise<InteractionRecord> { return this.mutate<InteractionRecord>(`${this.basePath}/${encodeURIComponent(id)}/respond`, response); }

  private async mutate<T>(path: string, body: unknown): Promise<T> {
    const token = this.options.csrfToken();
    if (!token) throw new PortalInteractionError(403, "csrf_required");
    return this.call<T>(path, { method: "POST", headers: { "Content-Type": "application/json", "X-VastPlan-CSRF": token }, body: JSON.stringify(body) });
  }

  private async call<T>(path: string, init: { method: string; headers?: Record<string, string>; body?: string }): Promise<T> {
    const response = await this.options.fetch(path, { ...init, credentials: "include" });
    const value = await response.json();
    if (!response.ok) {
      const code = typeof value === "object" && value !== null && "error" in value && typeof value.error === "string" ? value.error : "request_rejected";
      throw new PortalInteractionError(response.status, code);
    }
    return value as T;
  }
}

export class PortalInteractionError extends Error {
  public constructor(public readonly status: number, public readonly code: string) {
    super(`Portal interaction request failed: ${code}`);
    this.name = "PortalInteractionError";
  }
}
