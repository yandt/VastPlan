import type { JSONValue } from "@vastplan/ui-contract";
import type { PortalFetch, PortalFetchResponse } from "./interaction-client.js";

export interface PortalPluginRef {
  id: string;
  version: string;
  channel?: string;
}

export interface PortalCompositionRef {
  id: string;
  revision: number;
  digest: string;
}

export interface PortalManagementGrant {
  capability: string;
  read?: string[];
  write?: string[];
}

export interface PortalManagementBinding {
  tenantId: string;
  portalId: string;
  platformProfile: PortalCompositionRef;
  services: Array<{
    id: string;
    label?: string;
    logicalService: string;
    routingDomain: string;
    capabilities: PortalManagementGrant[];
  }>;
}

export interface PortalApplicationComposition {
  version: 1;
  revision: number;
  id: string;
  target: { kernel: "frontend" };
  route: string;
  domains?: string[];
  audience?: string[];
  branding?: Record<string, JSONValue>;
  plugins: PortalPluginRef[];
  config: Record<string, JSONValue>;
}

export type PortalRevisionStatus = "Draft" | "PendingApproval" | "Approved" | "Published";

export interface PortalRevision {
  id: number;
  tenantId: string;
  portalId: string;
  status: PortalRevisionStatus;
  active: boolean;
  composition: PortalApplicationComposition;
  resolved: {
    revision: number;
    id: string;
    tenantId: string;
    route: string;
    domains?: string[];
    audience?: string[];
    branding?: Record<string, JSONValue>;
    designSystem: PortalPluginRef & { uiContract: string };
    composition: PortalPluginRef & { uiContract: string };
    layout: PortalPluginRef & { uiContract: string; config?: Record<string, JSONValue> };
    plugins: PortalPluginRef[];
    config?: Record<string, JSONValue>;
    management: PortalManagementBinding;
    resolution: {
      platformCatalog: PortalCompositionRef;
      platformProfile: PortalCompositionRef;
      applicationComposition: PortalCompositionRef;
      managementBindingDigest: string;
      pluginOrigins: Record<string, "platform-profile" | "application">;
    };
  };
  submittedBy?: string;
  approvedBy?: string;
  publishedBy?: string;
  createdAt: string;
  updatedAt: string;
}

export interface PortalAuditEvent {
  id: number;
  tenantId: string;
  portalId: string;
  revisionId: number;
  action: string;
  actorId: string;
  reason?: string;
  priority: string;
  at: string;
}

export interface PortalControlClientOptions {
  fetch: PortalFetch;
  basePath?: string;
  csrfPath?: string;
}

/** Typed Web adapter for Portal composition governance. Identity remains server-owned. */
export class PortalControlClient {
  private readonly basePath: string;
  private readonly csrfPath: string;

  public constructor(private readonly options: PortalControlClientOptions) {
    this.basePath = options.basePath ?? "/v1/portal-drafts";
    this.csrfPath = options.csrfPath ?? "/v1/csrf";
  }

  public list(): Promise<PortalRevision[]> {
    return this.call<PortalRevision[]>(this.basePath, { method: "GET" });
  }

  public create(composition: PortalApplicationComposition): Promise<PortalRevision> {
    return this.mutate<PortalRevision>(this.basePath, "POST", composition);
  }

  public update(id: number, composition: PortalApplicationComposition): Promise<PortalRevision> {
    return this.mutate<PortalRevision>(this.revisionPath(id), "PUT", composition);
  }

  public submit(id: number): Promise<PortalRevision> {
    return this.mutate<PortalRevision>(`${this.revisionPath(id)}/submit`, "POST", {});
  }

  public approve(id: number): Promise<PortalRevision> {
    return this.mutate<PortalRevision>(`${this.revisionPath(id)}/approve`, "POST", {});
  }

  public publish(id: number, breakGlassReason = ""): Promise<PortalRevision> {
    return this.mutate<PortalRevision>(`${this.revisionPath(id)}/publish`, "POST", { breakGlassReason });
  }

  public rollback(id: number, breakGlassReason = ""): Promise<PortalRevision> {
    return this.mutate<PortalRevision>(`${this.revisionPath(id)}/rollback`, "POST", { breakGlassReason });
  }

  public audit(id: number): Promise<PortalAuditEvent[]> {
    return this.call<PortalAuditEvent[]>(`${this.revisionPath(id)}/audit`, { method: "GET" });
  }

  private revisionPath(id: number): string {
    if (!Number.isSafeInteger(id) || id <= 0) throw new PortalControlError(400, "invalid_revision");
    return `${this.basePath}/${id}`;
  }

  private async mutate<T>(path: string, method: "POST" | "PUT", body: unknown): Promise<T> {
    const csrf = await this.call<{ token: string }>(this.csrfPath, { method: "GET" });
    if (!csrf.token) throw new PortalControlError(403, "csrf_required");
    return this.call<T>(path, {
      method,
      headers: { "Content-Type": "application/json", "X-VastPlan-CSRF": csrf.token },
      body: JSON.stringify(body),
    });
  }

  private async call<T>(path: string, init: { method: string; headers?: Record<string, string>; body?: string }): Promise<T> {
    let response: PortalFetchResponse;
    try {
      response = await this.options.fetch(path, { ...init, credentials: "include" });
    } catch {
      throw new PortalControlError(0, "network_unavailable");
    }
    const value = await response.json();
    if (!response.ok) {
      const code = typeof value === "object" && value !== null && "error" in value && typeof value.error === "string" ? value.error : "request_rejected";
      throw new PortalControlError(response.status, code);
    }
    return value as T;
  }
}

export class PortalControlError extends Error {
  public constructor(public readonly status: number, public readonly code: string) {
    super(`Portal control request failed: ${code}`);
    this.name = "PortalControlError";
  }
}
