import type { PlatformFetch, PlatformFetchResponse } from "./types.js";
import { PlatformAdminError } from "./client.js";
import { CSRFTokenCache, withCSRF } from "./csrf-token-cache.js";

export class ManagementAPIClient {
  private readonly basePath: string;
  private readonly csrf: CSRFTokenCache;

  public constructor(private readonly fetcher: PlatformFetch, portalID: string, serviceID: string, apiID: string, private readonly csrfPath = "/v1/csrf") {
    this.basePath = `/v1/portals/${segment(portalID)}/platform/services/${segment(serviceID)}/api/${segment(apiID)}`;
    this.csrf = new CSRFTokenCache(async () => {
      const value = await this.call<{ token: string }>(this.csrfPath, { method: "GET" });
      if (!value.token) throw new PlatformAdminError(403, "csrf_required");
      return value.token;
    });
  }

  public get<T>(path: string): Promise<T> { return this.call<T>(this.route(path), { method: "GET" }); }

  public async mutate<T>(path: string, method: "POST" | "PUT" | "PATCH" | "DELETE", body: unknown = {}): Promise<T> {
    return withCSRF(this.csrf, (token) => this.call<T>(this.route(path), {
      method, headers: { "Content-Type": "application/json", "X-VastPlan-CSRF": token }, body: JSON.stringify(body),
    }), isCSRFRejected);
  }

  private route(path: string): string {
    if (!path.startsWith("/") || path.startsWith("//") || path.includes("\\") || path.split("?")[0].split("/").includes("..")) throw new PlatformAdminError(400, "invalid_management_api_path");
    return this.basePath + path;
  }

  private async call<T>(path: string, init: { method: string; headers?: Record<string, string>; body?: string }): Promise<T> {
    let response: PlatformFetchResponse;
    try { response = await this.fetcher(path, { ...init, credentials: "include" }); }
    catch { throw new PlatformAdminError(0, "network_unavailable"); }
    const value = await response.json();
    if (!response.ok) {
      const code = typeof value === "object" && value !== null && "error" in value && typeof value.error === "string" ? value.error : "request_rejected";
      throw new PlatformAdminError(response.status, code);
    }
    return value as T;
  }
}

function isCSRFRejected(error: unknown): boolean {
  return error instanceof PlatformAdminError && error.status === 403 && error.code === "csrf_rejected";
}

export function createBrowserManagementAPIClient(portalID: string, serviceID: string, apiID: string): ManagementAPIClient {
  const fetcher: PlatformFetch = (input, init) => globalThis.fetch(input, init as RequestInit);
  return new ManagementAPIClient(fetcher, portalID, serviceID, apiID);
}

function segment(value: string): string {
  if (value.trim() === "" || value.includes("/") || value.includes("\\")) throw new PlatformAdminError(400, "invalid_resource_name");
  return encodeURIComponent(value);
}
