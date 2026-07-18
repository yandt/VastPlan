export interface PlatformFetchResponse {
  ok: boolean;
  status: number;
  json(): Promise<unknown>;
}

export interface PlatformFetch {
  (input: string, init?: { method?: string; headers?: Record<string, string>; body?: string; credentials?: "include" }): Promise<PlatformFetchResponse>;
}

export interface Setting {
  key: string;
  value: unknown;
  version: number;
  updatedAt: string;
}

export interface CredentialMetadata {
  name: string;
  version: number;
  keyVersion: string;
  createdAt: string;
  updatedAt: string;
  revoked: boolean;
}

export interface DatabaseConnection {
  name: string;
  driver: string;
  endpoint: string;
  database?: string;
  credential: string;
}

export interface DatabaseProbe { ready: boolean; message?: string; }
export interface ArtifactRepositoryStatus { ready: boolean; listen?: string; }

export interface NodeBootstrapPlan {
  target: { address: string; port?: number; user: string };
  release: { version: string; url: string; sha256: string };
  node: {
    id: string; tenant: string; deployment: string; labels?: string;
    natsUrl: string; natsCa: string; natsCert: string; natsKey: string; natsSeed: string;
    transportSeed: string; transportTrust: string;
    repositoryUrl: string; repositoryCa?: string; repositoryTrust: string;
    capacityCpuMillis?: number; capacityMemoryBytes?: number; capacityGpu?: number;
  };
  sshIdentityCredential: string;
  sshKnownHostsCredential: string;
  secretFiles: Array<{ credential: string; destination: string; mode?: number }>;
}

export interface ManagedNode { id: string; plan: NodeBootstrapPlan; version: number; createdAt: string; updatedAt: string; }
export type BootstrapJobState = "Pending" | "Approved" | "Connecting" | "Installing" | "SystemdActive" | "Ready" | "Failed" | "Expired";
export interface BootstrapJob {
  id: string; nodeId: string; nodeVersion: number; state: BootstrapJobState;
  requestedBy: string; approvedBy?: string; errorCode?: string;
  createdAt: string; updatedAt: string; expiresAt: string;
}

export class PlatformAdminClient {
  public constructor(private readonly fetcher: PlatformFetch, private readonly basePath = "/v1/platform", private readonly csrfPath = "/v1/csrf") {}

  public listSettings(prefix = ""): Promise<Setting[]> { return this.get(`${this.basePath}/settings${query({ prefix })}`); }
  public putSetting(key: string, value: unknown, ifVersion?: number): Promise<Setting> {
    return this.mutate(`${this.basePath}/settings/${segment(key)}`, "PUT", { value, ...(ifVersion === undefined ? {} : { ifVersion }) });
  }
  public deleteSetting(key: string, ifVersion?: number): Promise<void> {
    const suffix = ifVersion === undefined ? "" : query({ ifVersion: String(ifVersion) });
    return this.mutate(`${this.basePath}/settings/${segment(key)}${suffix}`, "DELETE").then(() => undefined);
  }

  public listCredentials(prefix = ""): Promise<CredentialMetadata[]> { return this.get(`${this.basePath}/credentials${query({ prefix })}`); }
  public putCredential(name: string, value: string): Promise<CredentialMetadata> { return this.mutate(`${this.basePath}/credentials/${segment(name)}`, "PUT", { value }); }
  public rotateCredential(name: string): Promise<CredentialMetadata> { return this.mutate(`${this.basePath}/credentials/${segment(name)}/rotate`, "POST", {}); }
  public revokeCredential(name: string): Promise<CredentialMetadata> { return this.mutate(`${this.basePath}/credentials/${segment(name)}/revoke`, "POST", {}); }

  public listDatabaseConnections(): Promise<DatabaseConnection[]> { return this.get(`${this.basePath}/database-connections`); }
  public putDatabaseConnection(name: string, value: Omit<DatabaseConnection, "name">): Promise<DatabaseConnection> {
    return this.mutate(`${this.basePath}/database-connections/${segment(name)}`, "PUT", value);
  }
  public deleteDatabaseConnection(name: string): Promise<void> { return this.mutate(`${this.basePath}/database-connections/${segment(name)}`, "DELETE").then(() => undefined); }
  public probeDatabaseConnection(name: string): Promise<DatabaseProbe> { return this.mutate(`${this.basePath}/database-connections/${segment(name)}/probe`, "POST", {}); }
  public artifactRepositoryStatus(): Promise<ArtifactRepositoryStatus> { return this.get(`${this.basePath}/artifacts/status`); }

  public listManagedNodes(): Promise<ManagedNode[]> { return this.get(`${this.basePath}/deployment/nodes`); }
  public putManagedNode(id: string, plan: NodeBootstrapPlan, ifVersion?: number): Promise<ManagedNode> {
    return this.mutate(`${this.basePath}/deployment/nodes/${segment(id)}`, "PUT", { plan, ...(ifVersion === undefined ? {} : { ifVersion }) });
  }
  public listBootstrapJobs(): Promise<BootstrapJob[]> { return this.get(`${this.basePath}/deployment/bootstrap-jobs`); }
  public createBootstrapJob(nodeId: string): Promise<BootstrapJob> {
    return this.mutate(`${this.basePath}/deployment/nodes/${segment(nodeId)}/bootstrap`, "POST", {});
  }
  public approveBootstrapJob(jobId: string): Promise<BootstrapJob> {
    return this.mutate(`${this.basePath}/deployment/bootstrap-jobs/${segment(jobId)}/approve`, "POST", {});
  }

  private get<T>(path: string): Promise<T> { return this.call<T>(path, { method: "GET" }); }

  private async mutate<T>(path: string, method: "POST" | "PUT" | "DELETE", body?: unknown): Promise<T> {
    const csrf = await this.get<{ token: string }>(this.csrfPath);
    if (!csrf.token) throw new PlatformAdminError(403, "csrf_required");
    return this.call<T>(path, {
      method,
      headers: { "Content-Type": "application/json", "X-VastPlan-CSRF": csrf.token },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    });
  }

  private async call<T>(path: string, init: { method: string; headers?: Record<string, string>; body?: string }): Promise<T> {
    let response: PlatformFetchResponse;
    try {
      response = await this.fetcher(path, { ...init, credentials: "include" });
    } catch {
      throw new PlatformAdminError(0, "network_unavailable");
    }
    const value = await response.json();
    if (!response.ok) {
      const code = typeof value === "object" && value !== null && "error" in value && typeof value.error === "string" ? value.error : "request_rejected";
      throw new PlatformAdminError(response.status, code);
    }
    return value as T;
  }
}

export function createBrowserPlatformAdminClient(): PlatformAdminClient {
  const fetcher: PlatformFetch = (input, init) => globalThis.fetch(input, init as RequestInit);
  return new PlatformAdminClient(fetcher);
}

export class PlatformAdminError extends Error {
  public constructor(public readonly status: number, public readonly code: string) {
    super(`Platform administration request failed: ${code}`);
    this.name = "PlatformAdminError";
  }
}

function segment(value: string): string {
  if (value.trim() === "" || value.includes("/") || value.includes("\\")) throw new PlatformAdminError(400, "invalid_resource_name");
  return encodeURIComponent(value);
}

function query(values: Record<string, string>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) if (value !== "") params.set(key, value);
  const encoded = params.toString();
  return encoded === "" ? "" : `?${encoded}`;
}
