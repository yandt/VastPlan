import type { ModuleFetcher, PortalRuntimeSpec } from "./module-loader";

export class PortalGenerationCommitError extends Error {
  public constructor(public readonly code: string) {
    super(code);
    this.name = "PortalGenerationCommitError";
  }
}

/** Commits a prepared Server Generation before the Browser candidate becomes visible. */
export class PortalGenerationCommitClient {
  private readonly committed = new Set<string>();

  public constructor(private readonly fetcher: ModuleFetcher, private readonly endpoint = "/v1/portal-generation-commit", private readonly csrfEndpoint = "/v1/csrf") {}

  public async commit(spec: PortalRuntimeSpec): Promise<void> {
    const coordination = spec.coordination;
    if (coordination === undefined || coordination.state === "committed" || this.committed.has(coordination.transactionId)) return;
    const csrf = await this.request(this.csrfEndpoint, { method: "GET", credentials: "same-origin", cache: "no-store" });
    const token = recordString(csrf, "token");
    if (token === undefined) throw new PortalGenerationCommitError("CSRF_REQUIRED");
    const result = await this.request(this.endpoint, {
      method: "POST", credentials: "same-origin", cache: "no-store",
      headers: { "Content-Type": "application/json", "X-VastPlan-CSRF": token },
      body: JSON.stringify({ transactionId: coordination.transactionId }),
    });
    if (recordString(result, "state") !== "committed" || recordNumber(result, "activationId") !== coordination.activationId) {
      throw new PortalGenerationCommitError("GENERATION_COMMIT_INVALID");
    }
    if (this.committed.size >= 128) this.committed.clear();
    this.committed.add(coordination.transactionId);
  }

  private async request(path: string, init: RequestInit): Promise<unknown> {
    let response: Response;
    try { response = await this.fetcher(path, init); }
    catch { throw new PortalGenerationCommitError("NETWORK_UNAVAILABLE"); }
    let value: unknown;
    try { value = await response.json(); }
    catch { throw new PortalGenerationCommitError("GENERATION_COMMIT_INVALID"); }
    if (!response.ok) throw new PortalGenerationCommitError(recordString(value, "error") ?? `HTTP_${response.status}`);
    return value;
  }
}

function recordString(value: unknown, key: string): string | undefined {
  return typeof value === "object" && value !== null && !Array.isArray(value) && typeof (value as Record<string, unknown>)[key] === "string"
    ? (value as Record<string, string>)[key] : undefined;
}

function recordNumber(value: unknown, key: string): number | undefined {
  const result = typeof value === "object" && value !== null && !Array.isArray(value) ? (value as Record<string, unknown>)[key] : undefined;
  return Number.isSafeInteger(result) ? Number(result) : undefined;
}
