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
export interface ArtifactRepositoryMigration {
  migrationId?: string; phase?: string; sourceProvider?: string; sourceVolumeId?: string;
  targetProvider?: string; targetVolumeId?: string; files?: number; bytes?: number; digest?: string;
  observationUntil?: string; lastError?: string; configuredActive: boolean;
  canRollback: boolean; canFinalize: boolean; canRelease: boolean;
}
export interface ArtifactRepositoryStatus {
  ready: boolean; listen?: string; storageProvider?: string; storageVolumeId?: string;
  catalog?: { revision: number; artifacts: number; inventorySHA256?: string };
  migration?: ArtifactRepositoryMigration;
}
export interface PrepareArtifactMigrationRequest { migrationId: string; targetProvider: string; targetVolumeId: string; }
export interface ArtifactRef { pluginId: string; version: string; channel: string; }
export interface ArtifactRequirement { pluginId: string; constraint: string; }
export interface ArtifactLifecycleRequest {
  ref: ArtifactRef; status: "active" | "deprecated" | "yanked" | "revoked"; reason: string;
  replacement?: ArtifactRequirement; expectedRevision: number;
}
export interface ArtifactLifecycleResult {
  revision: number;
  entry: { ref: ArtifactRef; lifecycleStatus: string; lifecycleRevision: number; lifecycleReason?: string; replacement?: ArtifactRequirement };
}
export interface ArtifactReference { ref: ArtifactRef; sha256: string; purpose: string; }
export interface ArtifactReferenceSnapshotValue {
  schemaVersion: "v1"; ownerKind: string; ownerId: string; generation: number; ttlSeconds?: number;
  references: ArtifactReference[]; digest: string;
}
export interface ArtifactReferenceSnapshot {
  tenantId: string; publisherId: string; value: ArtifactReferenceSnapshotValue; reportedAt: string; expiresAt?: string;
}
export interface ArtifactReferencePage { revision: number; items: ArtifactReferenceSnapshot[]; }
export interface ArtifactGCBlocker { code: string; message: string; }
export interface ArtifactGCCandidate { ref: ArtifactRef; sha256: string; size: number; lifecycle: "yanked" | "revoked"; }
export interface ArtifactGCPlan {
  schemaVersion: "v1"; planId?: string; ready: boolean; createdAt: string;
  catalogRevision: number; referenceRevision: number; candidates: ArtifactGCCandidate[];
  bytes: number; blockers?: ArtifactGCBlocker[];
}
export interface ArtifactGCRecord extends ArtifactGCCandidate {
  retirementId: string; status: "quarantining" | "quarantined" | "sweeping" | "swept";
  quarantinedAt: string; sweepAfter: string; sweptAt?: string;
}
export interface ArtifactGCStatus { revision: number; items: ArtifactGCRecord[]; }
export interface ArtifactCapacityBucket { namespace: string; publisher: string; channel: string; artifacts: number; bytes: number; }
export interface ArtifactQuotaUsage {
  id: string; namespace?: string; publisher?: string; channel?: string; artifacts: number; bytes: number;
  maxArtifacts?: number; maxBytes?: number; exceeded: boolean;
}
export interface ArtifactCapacity {
  catalogRevision: number; gcRevision: number; activeArtifacts: number; activeBytes: number;
  quarantinedArtifacts: number; quarantinedBytes: number; sweptArtifacts: number;
  reclaimedBytes: number; storedBytes: number; buckets: ArtifactCapacityBucket[]; quotas: ArtifactQuotaUsage[];
}

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
  referencePending?: boolean;
  submittedBy?: string; approvedBy?: string; publishedBy?: string; createdAt: string; updatedAt: string;
}
export interface ServiceAuditEvent { id: number; revisionId: number; deployment: string; action: string; actorId: string; at: string; }
export interface ArtifactRef { pluginId: string; version: string; channel: string; }
export interface TestTargetBinding {
  id: string; kind: "backend"; deployment: string; unitId: string; pluginId: string;
  allowedPublishers: string[]; enabled: boolean; version: number; createdAt: string; updatedAt: string;
}
export interface PutTestTargetBindingRequest {
  kind: "backend"; deployment: string; unitId: string; pluginId: string;
  allowedPublishers: string[]; enabled: boolean; ifVersion?: number;
}
export type TestReleaseStatus = "Queued" | "Resolving" | "Preparing" | "Validating" | "Activating" | "Ready" | "RollingBack" | "RolledBack" | "Failed" | "Superseded";
export interface TestRelease {
  id: number; bindingId: string; artifact: ArtifactRef; sha256: string; repositoryRevision: number;
  status: TestReleaseStatus; previousServiceRevisionId?: number; candidateServiceRevisionId?: number;
  rollbackServiceRevisionId?: number; rollbackRequired?: boolean; errorCode?: string; errorMessage?: string;
  requestedBy: string; createdAt: string; updatedAt: string;
}
export interface CreateTestReleaseRequest { bindingId: string; artifact: ArtifactRef; sha256: string; repositoryRevision: number; }

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
  public artifactRepositoryCapacity(): Promise<ArtifactCapacity> { return this.get(`${this.basePath}/artifacts/capacity`); }
  public listArtifactReferences(): Promise<ArtifactReferencePage> { return this.get(`${this.basePath}/artifacts/references`); }
  public planArtifactGarbageCollection(): Promise<ArtifactGCPlan> { return this.get(`${this.basePath}/artifacts/gc/plan`); }
  public artifactGarbageCollectionStatus(): Promise<ArtifactGCStatus> { return this.get(`${this.basePath}/artifacts/gc/status`); }
  public quarantineArtifacts(planId: string, graceHours: number): Promise<ArtifactGCStatus> {
    if (!/^[a-f0-9]{64}$/.test(planId) || !Number.isSafeInteger(graceHours) || graceHours < 24 || graceHours > 24 * 365) throw new PlatformAdminError(400, "invalid_gc_request");
    return this.mutate(`${this.basePath}/artifacts/gc/quarantine`, "POST", { planId, graceHours });
  }
  public sweepArtifacts(): Promise<ArtifactGCStatus> { return this.mutate(`${this.basePath}/artifacts/gc/sweep`, "POST", {}); }
  public setArtifactLifecycle(request: ArtifactLifecycleRequest): Promise<ArtifactLifecycleResult> { return this.mutate(`${this.basePath}/artifacts/lifecycle`, "POST", request); }
  public artifactMigrationStatus(): Promise<ArtifactRepositoryMigration> { return this.get(`${this.basePath}/artifacts/migration`); }
  public prepareArtifactMigration(request: PrepareArtifactMigrationRequest): Promise<ArtifactRepositoryMigration> { return this.mutate(`${this.basePath}/artifacts/migrations`, "POST", request); }
  public syncArtifactMigration(id: string): Promise<ArtifactRepositoryMigration> { return this.artifactMigrationAction(id, "sync"); }
  public cutoverArtifactMigration(id: string, observationSeconds: number): Promise<ArtifactRepositoryMigration> {
    if (!Number.isSafeInteger(observationSeconds) || observationSeconds < 60 || observationSeconds > 7 * 24 * 60 * 60) throw new PlatformAdminError(400, "invalid_observation_seconds");
    return this.artifactMigrationAction(id, "cutover", { observationSeconds });
  }
  public rollbackArtifactMigration(id: string): Promise<ArtifactRepositoryMigration> { return this.artifactMigrationAction(id, "rollback"); }
  public finalizeArtifactMigration(id: string): Promise<ArtifactRepositoryMigration> { return this.artifactMigrationAction(id, "finalize"); }
  public releaseArtifactMigration(id: string): Promise<ArtifactRepositoryMigration> { return this.artifactMigrationAction(id, "release"); }

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
  public listTestTargetBindings(): Promise<TestTargetBinding[]> { return this.get(`${this.basePath}/deployment/test-target-bindings`); }
  public putTestTargetBinding(id: string, request: PutTestTargetBindingRequest): Promise<TestTargetBinding> {
    return this.mutate(`${this.basePath}/deployment/test-target-bindings/${segment(id)}`, "PUT", request);
  }
  public listTestReleases(): Promise<TestRelease[]> { return this.get(`${this.basePath}/deployment/test-releases`); }
  public createTestRelease(request: CreateTestReleaseRequest): Promise<TestRelease> {
    return this.mutate(`${this.basePath}/deployment/test-releases`, "POST", request);
  }
  public rollbackTestRelease(id: number): Promise<TestRelease> {
    return this.mutate(`${this.basePath}/deployment/test-releases/${revision(id)}/rollback`, "POST", {});
  }

  private serviceRevisionAction(id: number, action: string): Promise<ServiceRevision> {
    return this.mutate(`${this.basePath}/deployment/service-revisions/${revision(id)}/${action}`, "POST", {});
  }

  private artifactMigrationAction(id: string, action: string, body: Record<string, unknown> = {}): Promise<ArtifactRepositoryMigration> {
    return this.mutate(`${this.basePath}/artifacts/migrations/${segment(id)}/${action}`, "POST", body);
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
