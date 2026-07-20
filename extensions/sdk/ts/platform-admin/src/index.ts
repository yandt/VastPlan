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
  resourceId: string;
  revision: number;
  providerId: string;
  endpoint: string;
  database?: string;
  options: Record<string, unknown>;
  pool: DatabasePoolPolicy;
  runtime: "ready" | "pending";
  credential: { managed: boolean; version: number };
}

export interface DatabasePoolPolicy {
  minIdle?: number; maxIdle: number; maxOpen: number; maxLifetimeMs: number;
  maxIdleTimeMs: number; acquireTimeoutMs: number; idlePoolTtlMs: number;
}

export interface PutDatabaseConnectionRequest {
  providerId: string;
  endpoint: string;
  database?: string;
  options: Record<string, unknown>;
  pool?: DatabasePoolPolicy;
  credentialValue?: string;
}

export interface DatabaseProbe { ready: boolean; message?: string; }
export interface ArtifactRepositoryStatus { ready: boolean; listen?: string; storageProvider?: string; }

export interface NodeBootstrapPlan {
  target: { address: string; port?: number; user: string };
  release: { version: string; url: string; sha256: string };
  node: {
    id: string; tenant: string; deployment: string; labels?: string;
    natsUrl: string; natsCa: string; natsCert: string; natsKey: string; natsSeed: string;
    transportSeed: string; transportTrust: string; transportPublicKey: string;
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

export interface CompositionRef { id: string; revision: number; digest: string; }
export interface DeploymentTarget { deploymentName: string; platformProfile: CompositionRef; }
export interface BackendPluginRef { id: string; version: string; channel?: string; }
export interface BackendServiceUnit {
  id: string; kind: string; plugins: BackendPluginRef[]; config?: Record<string, unknown>; enabled: boolean;
  service_role: string; logical_service?: string; instance_policy?: string; state_model?: string;
  visibility?: string; routing?: string; routing_domain?: string; partition_keys?: string[];
  depends_on?: string[]; replicas: number; autoscaling?: Record<string, unknown>;
  resources?: Record<string, unknown>; placement?: Record<string, unknown>;
}
export interface BackendApplicationComposition {
  version: 1; revision: number; id: string; target: { kernel: "backend" };
  metadata: { name: string; tenant?: string };
  units: Array<{ serviceClass: string; spec: BackendServiceUnit }>;
}
export type ServiceRevisionStatus = "Draft" | "PendingApproval" | "Approved" | "Publishing" | "Published";
export interface ServiceRevision {
  id: number; deployment: string; status: ServiceRevisionStatus; active: boolean;
  composition: BackendApplicationComposition; preview: Record<string, unknown>; previewDigest: string; kvRevision?: number;
  submittedBy?: string; approvedBy?: string; publishedBy?: string; createdAt: string; updatedAt: string;
}
export interface ServiceAuditEvent { id: number; revisionId: number; deployment: string; action: string; actorId: string; at: string; }

export class PlatformAdminClient {
	private readonly basePath: string;
	public constructor(private readonly fetcher: PlatformFetch, portalID: string, serviceID: string, private readonly csrfPath = "/v1/csrf") {
		this.basePath = `/v1/portals/${segment(portalID)}/platform/services/${segment(serviceID)}`;
	}

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
  public putDatabaseConnection(name: string, value: PutDatabaseConnectionRequest): Promise<DatabaseConnection> {
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

  public listDeploymentTargets(): Promise<DeploymentTarget[]> { return this.get(`${this.basePath}/deployment/targets`); }
  public listServiceRevisions(): Promise<ServiceRevision[]> { return this.get(`${this.basePath}/deployment/service-revisions`); }
  public createServiceDraft(composition: BackendApplicationComposition): Promise<ServiceRevision> {
    return this.mutate(`${this.basePath}/deployment/service-revisions`, "POST", { composition });
  }
  public updateServiceDraft(id: number, composition: BackendApplicationComposition): Promise<ServiceRevision> {
    return this.mutate(`${this.basePath}/deployment/service-revisions/${revision(id)}`, "PUT", { composition });
  }
  public submitServiceDraft(id: number): Promise<ServiceRevision> { return this.serviceRevisionAction(id, "submit"); }
  public approveServiceRevision(id: number): Promise<ServiceRevision> { return this.serviceRevisionAction(id, "approve"); }
  public publishServiceRevision(id: number): Promise<ServiceRevision> { return this.serviceRevisionAction(id, "publish"); }
  public rollbackServiceRevision(id: number): Promise<ServiceRevision> { return this.serviceRevisionAction(id, "rollback"); }
  public listServiceRevisionAudit(id: number): Promise<ServiceAuditEvent[]> { return this.get(`${this.basePath}/deployment/service-revisions/${revision(id)}/audit`); }

  private serviceRevisionAction(id: number, action: string): Promise<ServiceRevision> {
    return this.mutate(`${this.basePath}/deployment/service-revisions/${revision(id)}/${action}`, "POST", {});
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

export function createBrowserPlatformAdminClient(portalID: string, serviceID: string): PlatformAdminClient {
	const fetcher: PlatformFetch = (input, init) => globalThis.fetch(input, init as RequestInit);
	return new PlatformAdminClient(fetcher, portalID, serviceID);
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

function revision(value: number): string {
  if (!Number.isSafeInteger(value) || value < 1) throw new PlatformAdminError(400, "invalid_revision_id");
  return String(value);
}

function query(values: Record<string, string>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) if (value !== "") params.set(key, value);
  const encoded = params.toString();
  return encoded === "" ? "" : `?${encoded}`;
}
