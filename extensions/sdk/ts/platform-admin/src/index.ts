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

export interface PluginConfigurationDefinition {
  id: string;
  deployment: string;
  unitId: string;
  pluginId: string;
  pluginName: string;
  origin: "platform-profile" | "application";
  artifact: { version: string; channel: string; sha256: string };
  scope: "service" | "tenant" | "user";
  applyMode: "restart" | "hot";
  applyPath: "application-deployment" | "platform-profile" | "hot-service" | "hot-scoped";
  controllerAvailable: boolean;
  resourceControllerAvailable?: boolean;
  resourceCollections?: PluginConfigurationResourceCollection[];
  schema: Record<string, unknown>;
  schemaDigest: string;
  managedCredentials: Array<{ id: string; title: string; description?: string; purpose: string; required?: boolean }>;
  credentialStates?: Array<{ fieldId: string; configured: boolean; version?: number }>;
  values: Record<string, unknown>;
  deploymentRevision: number;
  deploymentDigest: string;
  catalogDigest: string;
}

export interface PluginConfigurationResourceCollection {
  id: string; kind: "profile"; title: string; description?: string;
  schema: Record<string, unknown>; schemaDigest: string;
  managedCredentials?: Array<{ id: string; title: string; description?: string; purpose: string; required?: boolean }>;
  minItems?: number; maxItems: number;
}

export interface PluginConfigurationResourceItem {
  resourceId: string; active: { revision: number; digest: string }; values: Record<string, unknown>;
  credentialStates?: Array<{ fieldId: string; configured: boolean; version?: number }>;
  updatedAt: string;
}

export interface PluginConfigurationResourcePage {
  protocol: "configuration.resource.v1"; collectionId: string; items: PluginConfigurationResourceItem[];
  nextCursor?: string; observedAt: string;
}

export interface PluginConfigurationResourceResponse {
  protocol: "configuration.resource.v1"; collectionId: string; item: PluginConfigurationResourceItem; observedAt: string;
}

export type PluginConfigurationCandidateStatus = "Draft" | "Preparing" | "Publishing" | "Activating" | "Ready" | "Failed" | "RollingBack" | "RolledBack";
export interface PluginConfigurationCandidate {
  id: string; configurationId: string; revision: number; status: PluginConfigurationCandidateStatus;
  applyPath: "application-deployment" | "platform-profile" | "hot-service" | "hot-scoped" | "resource-profile";
  resourceCollectionId?: string; resourceId?: string; resourceAction?: "create" | "update" | "delete";
  scopeSubjectId?: string;
  catalogDigest: string; schemaDigest: string; artifactSha256: string; values: Record<string, unknown>;
  createdBy: string; createdAt: string; updatedAt: string; errorCode?: string; errorMessage?: string;
  externalRevision?: number; externalDigest?: string; externalStatus?: "Preparing" | "Prepared" | "PendingApproval" | "Approved" | "Activating" | "FinalizingCredentials" | "Aborting" | "Committed" | "CatalogActivated" | "Publishing" | "Ready" | "RollingBack" | "Failed" | "RolledBack" | "Aborted"; rollbackRevision?: number;
  managedCredentials?: Array<{ fieldId: string; staged: boolean; state: string; version?: number }>;
}

export interface CredentialMetadata {
  name: string;
  version: number;
  keyVersion: string;
  createdAt: string;
  updatedAt: string;
  revoked: boolean;
}

export interface ManagedCredentialAuditEvent {
  id: number;
  credentialFingerprint: string;
  action: string;
  state: "Preparing" | "Candidate" | "Active" | "Aborted" | "Retired";
  owner: string;
  purpose: string;
  resource: string;
  delegated: boolean;
  candidateId?: string;
  configurationId?: string;
  fieldId?: string;
  occurredAt: string;
}

export interface ManagedCredentialMaintenanceStatus {
  lastRunAt?: string;
  autoAborted: number;
  collected: number;
  counts: Record<string, number>;
}

export interface ManagedCredentialAuditPage {
  items: ManagedCredentialAuditEvent[];
  nextBeforeId?: number;
  maintenance: ManagedCredentialMaintenanceStatus;
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
export type AuthenticationProviderState = "draft" | "validated" | "tested" | "approved" | "published" | "retired";
export type AuthenticationProviderReadiness = "unknown" | "blocked" | "ready" | "degraded" | "failed";
export interface AuthenticationProviderProfile {
  version: 1; revision: number; id: string; contributionId: string; configuration: CompositionRef;
  purposes: string[]; methods: string[]; subjectNamespace: string; requiredCapabilities: string[];
}
export interface ManagedAuthenticationProvider {
  profile: AuthenticationProviderProfile;
  lifecycle: { schemaVersion: "v1"; profile: CompositionRef; state: AuthenticationProviderState; readiness: AuthenticationProviderReadiness; unmetCapabilities: string[]; updatedAt: string; testedAt?: string; approvedAt?: string; publishedAt?: string };
  testedBy?: string; approvedBy?: string;
}
export interface AuthenticationProviderManagementState {
  version: 1;
  generation: number;
  providers: ManagedAuthenticationProvider[];
  catalog?: unknown;
  accessCatalog?: unknown;
  updatedAt: string;
}
export interface SeedHandoffState {
  version: 1;
  generation: number;
  phase: "uninitialized" | "seed-active" | "provider-configured" | "provider-verified" | "handoff-ready" | "enterprise-active" | "recovery-lease";
  providerProfile?: CompositionRef;
  providerSubject?: { id: string; issuer: string };
  handoff?: { providerProfile: CompositionRef; subject: { id: string; issuer: string }; policySnapshot: CompositionRef; sessionId: string; authenticatedAt: string; expiresAt: string; recoveryReady: boolean; digest: string };
  updatedAt: string;
}
export interface ArtifactRepositoryMigration {
  migrationId?: string; phase?: string; sourceProvider?: string; sourceVolumeId?: string;
  targetProvider?: string; targetVolumeId?: string; files?: number; bytes?: number; digest?: string;
  observationUntil?: string; lastError?: string; configuredActive: boolean;
  canRollback: boolean; canFinalize: boolean; canRelease: boolean;
}
export interface ArtifactRepositoryStatus {
  ready: boolean; listen?: string; storageProvider?: string; storageVolumeId?: string;
  catalog?: { revision: number; artifacts: number; inventorySHA256?: string; publicationRevision?: number; publicationInventorySHA256?: string };
  securityAssessment?: { artifacts: number; unassessed: number; admissionCurrent: number; rescanPassed: number; rescanFailed: number; stale: number; invalid: number; alert: boolean };
  migration?: ArtifactRepositoryMigration;
}
export interface ArtifactAssessmentRevisionStatus {
  databaseRevision: string; artifacts: number; current: number; failed: number; stale: number; invalid: number; lastEvaluatedAt?: string;
}
export interface ArtifactAssessmentInventory {
  observedAt: string; reportArchiveReady: boolean; truncated: boolean; revisions: ArtifactAssessmentRevisionStatus[];
}
export interface DataPlaneTicketGrant { endpoint: string; leaseId: string; ticket: string; expiresAt: string; }
export interface ArtifactAssessmentReportGrant { sha256: string; resource: string; }
export interface ArtifactCatalogQuery {
  pluginId?: string; pluginPrefix?: string; namespace?: string; publisher?: string; version?: string;
  channel?: string; target?: "backend" | "frontend" | "runner" | "mobile";
  lifecycle?: "active" | "deprecated" | "yanked" | "revoked"; page?: number; pageSize?: number;
}
export interface ArtifactCatalogEntry {
  ref: ArtifactRef; sha256: string; size: number; publisher: string; keyId: string;
  signedAt: string; publishedAt: string; repositoryRevision: number; name: string; description: string;
  namespace: string; license?: string; targets: string[]; platforms?: string[];
  lifecycleStatus: "active" | "deprecated" | "yanked" | "revoked";
  lifecycleRevision?: number; lifecycleReason?: string;
  replacement?: ArtifactRequirement;
  sbom?: { format: "cyclonedx-json"; specVersion: "1.5" | "1.6"; sha256: string };
  pythonLock?: ArtifactPythonLockDeclaration;
  provenance?: ArtifactProvenanceDeclaration;
	securityAdmission?: ArtifactSecurityAdmissionDeclaration;
	securityStatus?: ArtifactSecurityStatusEvidence;
}
export interface ArtifactPythonLockDeclaration { format: "pylock-toml"; specVersion: "1.0"; sha256: string; }
export interface ArtifactProvenanceDeclaration {
  provenanceSha256: string; verificationSha256: string; predicateType: string; builderId: string; buildType: string;
  providerId: string; keyId: string; policyId: string; verifiedAt: string; expiresAt: string;
}
export interface ArtifactSecurityAdmissionDeclaration {
  admissionSha256: string; providerId: string; keyId: string; policyId: string;
  scannerId: string; scannerVersion: string; databaseRevision: string; decision: "pass" | "fail";
  evaluatedAt: string; expiresAt: string; critical: number; high: number; medium: number; low: number;
  unknownVulnerability: number; deniedLicense: number; unknownLicense: number;
}
export interface ArtifactSecurityStatusEvidence {
  sequence: number; recordSha256: string; previousSha256: string; decision: "pass" | "fail";
  databaseRevision: string; evaluatedAt: string; expiresAt: string; critical: number; high: number;
  deniedLicense: number; unknownLicense: number; vulnerabilityReportSha256?: string; licenseReportSha256?: string; verification: "verified";
}
export interface ArtifactCatalogPage { revision: number; total: number; page: number; pageSize: number; items: ArtifactCatalogEntry[]; }
export interface PrepareArtifactMigrationRequest { migrationId: string; targetProvider: string; targetVolumeId: string; }
export interface ArtifactRef { pluginId: string; version: string; channel: string; }
export interface ArtifactRepositoryReceipt {
  schemaVersion: 1; repositoryId: string;
  protocol: "artifact.repository.local-test.v1" | "artifact.repository.remote.v1";
  profileDigest: string; ref: ArtifactRef; sha256: string; revision: number;
  workspaceLease?: string; expiresAt?: string;
}
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
export type ArtifactPublicationStatus = "PendingApproval" | "Approved" | "Published" | "Rejected" | "Cancelled" | "Expired";
export interface ArtifactPublication {
  id: string; revision: number; status: ArtifactPublicationStatus; source: ArtifactRef; target: ArtifactRef;
  sha256: string; publisher: string; keyId: string; sourceAttestationSha256: string; publishedAttestationSha256?: string;
  sourceProvenanceSha256?: string; sourceProvenanceVerificationSha256?: string;
	 sourceSecurityAdmissionSha256?: string;
  publishedProvenanceSha256?: string; publishedProvenanceVerificationSha256?: string;
	 publishedSecurityAdmissionSha256?: string;
  reason: string; submittedBy: string; approvedBy?: string; submittedAt: string; expiresAt: string; approvedAt?: string; publishedAt?: string;
  terminalReason?: string; terminalBy?: string; terminalAt?: string;
}
export interface ArtifactPublicationPage { revision: number; items: ArtifactPublication[]; }
export interface SubmitArtifactPublicationRequest { source: ArtifactRef; targetChannel: "stable"; reason: string; expectedRevision: number; }
export interface ArtifactPublicationResult { revision: number; entry: ArtifactPublication; }
export interface ArtifactSupplyChainEvidence {
  ref: ArtifactRef; sha256: string; size: number; publisher: string; keyId: string; signedAt: string;
  attestationSha256: string; verification: "verified"; name: string; description: string; license?: string;
  targets: string[]; engines: Record<string, string>; repositoryRevision: number; lifecycleStatus: string; publications: ArtifactPublication[];
  sbom?: { format: "cyclonedx-json"; specVersion: "1.5" | "1.6"; sha256: string; serialNumber?: string; components: number; verification: "verified" };
  pythonLock?: ArtifactPythonLockDeclaration & { requiresPython: string; createdBy: string; packages: number; wheels: number; verification: "verified" };
  provenance?: ArtifactProvenanceDeclaration & { sources: number; verification: "verified" };
	securityAdmission?: ArtifactSecurityAdmissionDeclaration & { vulnerabilityReportSha256?: string; licenseReportSha256?: string; verification: "verified" };
	securityStatus?: ArtifactSecurityStatusEvidence;
}

export type APIExposureStatus = "Draft" | "PendingApproval" | "Approved" | "Published" | "Superseded" | "Retired";
export interface APIExposureRevision {
  id: number; status: APIExposureStatus;
  exposure: {
    id: string; revision: number; routeKey: string; displayName: string; tenantId: string; portalId?: string;
    hosts: string[]; contract: { pluginId: string; artifactSha256: string; contributionId: string; contractId: string; contractVersion: string; contractDigest: string };
    authentication: { profileId: string; allowAnonymous: boolean }; requiredPermissions: string[];
    limits: { maxBodyBytes: number; maxResponseBytes: number; requestsPerMinute: number; timeoutMs: number };
    target: { logicalService: string; routingDomain: string };
  };
  submittedBy?: string; approvedBy?: string; publishedBy?: string; createdAt: string; updatedAt: string;
}
export interface APIExposureDraftRequest {
  baseExposureId?: string;
  contract: { pluginId: string; artifactSha256: string; contributionId: string };
  input: {
    displayName: string; portalId?: string; hosts: string[]; authentication: { profileId: string; allowAnonymous: boolean };
    requiredPermissions: string[]; limits: { maxBodyBytes: number; maxResponseBytes: number; requestsPerMinute: number; timeoutMs: number };
    target: { logicalService: string; routingDomain: string };
  };
}
export interface DataPlaneExposureRevision {
  id: number; status: APIExposureStatus;
  exposure: {
    id: string; revision: number; routeKey: string; tenantId: string; hosts: string[];
    service: { pluginId: string; artifactSha256: string; contributionId: string };
    dataPlaneServiceId: string; allowedModes: Array<"gateway-proxy" | "ticket-redirect" | "private-direct">;
    allowedEndpointOrigins: string[]; tlsIdentityPrefix: string;
    authentication: { profileId: string; allowAnonymous: boolean }; requiredPermissions: string[]; maxObjectBytes: number;
  };
  submittedBy?: string; approvedBy?: string; publishedBy?: string; createdAt: string; updatedAt: string;
}
export interface DataPlaneExposureDraftRequest {
  baseExposureId?: string;
  input: {
    hosts: string[];
    service: { pluginId: string; artifactSha256: string; contributionId: string };
    allowedModes: Array<"gateway-proxy" | "ticket-redirect" | "private-direct">;
    allowedEndpointOrigins: string[];
    tlsIdentityPrefix: string;
    authentication: { profileId: string; allowAnonymous: boolean };
    requiredPermissions: string[];
    maxObjectBytes: number;
  };
}

export type AuthorizationLifecycleState = "Draft" | "PendingApproval" | "Approved" | "Published" | "Retired";
export interface AuthorizationPermission {
  code: string; title: string; description?: string; scope: "platform" | "tenant" | "project" | "resource";
  resourceType?: string; risk: "low" | "medium" | "high" | "critical"; assignable: boolean; offlineAllowed: boolean;
  pluginId: string; pluginVersion: string; publisher: string; artifactSha256: string;
}
export interface AuthorizationStatement {
  id: string; effect: "allow" | "deny"; permissions: string[];
  resource?: { type: string; ids: string[]; labels: Record<string, string[]>; ownership: string };
  constraints: Array<{ source: string; key: string; operator: "eq" | "in" | "prefix"; values: string[] }>;
}
export interface AuthorizationRoleRevision {
  id: string; revision: number; domainId: string; title: string; description?: string; statements: AuthorizationStatement[];
  state: AuthorizationLifecycleState; createdBy: string; approvedBy?: string; createdAt: string; updatedAt: string;
}
export interface AuthorizationBindingRevision {
  id: string; revision: number; domainId: string; subject: { kind: "user" | "group" | "service" | "device"; id: string; issuer?: string };
  roleId: string; roleRevision: number; notBefore: string; expiresAt: string; state: AuthorizationLifecycleState;
  createdBy: string; approvedBy?: string; createdAt: string; updatedAt: string;
}
export interface AuthorizationAuditEvent { id: string; action: string; objectKind: string; objectId: string; revision: number; subjectId: string; reason?: string; occurredAt: string; }
export interface AuthorizationPolicyState {
  version: number; generation: number; policyRevision: number; revocationRevision: number;
  catalog: { schemaVersion: string; permissions: AuthorizationPermission[]; operations: unknown[]; digest: string };
  roles: AuthorizationRoleRevision[]; bindings: AuthorizationBindingRevision[];
  revocations: Array<{ id: string; revision: number; kind: string; targetId: string; effectiveAt: string; reasonCode: string }>;
  audit: AuthorizationAuditEvent[]; currentSnapshot?: { snapshotId: string; revision: number; audience: string[]; issuedAt: string; expiresAt: string };
}
export interface CreateAuthorizationRoleRequest { expectedGeneration: number; id: string; domainId: string; title: string; description?: string; statements: AuthorizationStatement[]; }
export interface CreateAuthorizationBindingRequest { expectedGeneration: number; id: string; domainId: string; subject: AuthorizationBindingRevision["subject"]; roleId: string; roleRevision: number; notBefore: string; expiresAt: string; }
export type UpdateAuthorizationBindingRequest = Omit<CreateAuthorizationBindingRequest, "id">;

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
  artifactReferences: ArtifactReference[];
  referencePending?: boolean;
  configurationCandidateId?: string; configurationId?: string; previousServiceRevision?: number; rollbackServiceRevision?: number;
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
	id: number; bindingId: string; receipt: ArtifactRepositoryReceipt;
  status: TestReleaseStatus; previousServiceRevisionId?: number; candidateServiceRevisionId?: number;
  rollbackServiceRevisionId?: number; rollbackRequired?: boolean; errorCode?: string; errorMessage?: string;
  requestedBy: string; createdAt: string; updatedAt: string;
}
export interface CreateTestReleaseRequest { bindingId: string; receipt: ArtifactRepositoryReceipt; }

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

  public listPluginConfigurationDefinitions(): Promise<PluginConfigurationDefinition[]> { return this.get(`${this.basePath}/plugin-configurations`); }
  public getPluginConfigurationDefinition(id: string, catalogDigest?: string, scopeSubjectId?: string): Promise<PluginConfigurationDefinition> {
    return this.get(`${this.basePath}/plugin-configurations/${segment(id)}${query({ catalogDigest, scopeSubjectId })}`);
  }
  public listPluginConfigurationCandidates(): Promise<PluginConfigurationCandidate[]> { return this.get(`${this.basePath}/plugin-configurations/candidates`); }
  public createPluginConfigurationDraft(configurationId: string, catalogDigest: string, values: Record<string, unknown>, secrets: Record<string, string> = {}, scopeSubjectId?: string): Promise<PluginConfigurationCandidate> {
	return this.mutate(`${this.basePath}/plugin-configurations/candidates`, "POST", { configurationId, catalogDigest, values, ...(Object.keys(secrets).length === 0 ? {} : { secrets }), ...(scopeSubjectId === undefined || scopeSubjectId === "" ? {} : { scopeSubjectId }) });
  }
  public discardPluginConfigurationDraft(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}`, "DELETE", { expectedRevision });
  }
  public submitPluginConfigurationDraft(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}/submit`, "POST", { expectedRevision });
  }
  public activatePluginConfigurationCandidate(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}/activate`, "POST", { expectedRevision });
  }
  public submitPlatformProfileConfigurationDraft(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}/submit-profile`, "POST", { expectedRevision });
  }
  public approvePlatformProfileConfigurationCandidate(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}/approve-profile`, "POST", { expectedRevision });
  }
  public activatePlatformProfileConfigurationCandidate(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}/activate-profile`, "POST", { expectedRevision });
  }
  public abortPlatformProfileConfigurationCandidate(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}/abort-profile`, "POST", { expectedRevision });
  }
  public submitHotServiceConfigurationDraft(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}/submit-hot`, "POST", { expectedRevision });
  }
  public approveHotServiceConfigurationCandidate(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}/approve-hot`, "POST", { expectedRevision });
  }
  public activateHotServiceConfigurationCandidate(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}/activate-hot`, "POST", { expectedRevision });
  }
  public abortHotServiceConfigurationCandidate(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}/abort-hot`, "POST", { expectedRevision });
  }
  public submitScopedConfigurationDraft(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}/submit-scoped`, "POST", { expectedRevision });
  }
  public approveScopedConfigurationCandidate(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}/approve-scoped`, "POST", { expectedRevision });
  }
  public activateScopedConfigurationCandidate(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}/activate-scoped`, "POST", { expectedRevision });
  }
  public abortScopedConfigurationCandidate(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}/abort-scoped`, "POST", { expectedRevision });
  }
  public listPluginConfigurationResources(configurationId: string, resourceCollectionId: string, catalogDigest: string, cursor?: string, limit?: number): Promise<PluginConfigurationResourcePage> {
    return this.get(`${this.basePath}/plugin-configurations/resources${query({ configurationId, resourceCollectionId, catalogDigest, cursor, limit: limit?.toString() })}`);
  }
  public getPluginConfigurationResource(configurationId: string, resourceCollectionId: string, resourceId: string, catalogDigest: string): Promise<PluginConfigurationResourceResponse> {
    return this.get(`${this.basePath}/plugin-configurations/resources/${segment(resourceId)}${query({ configurationId, resourceCollectionId, catalogDigest })}`);
  }
  public createPluginConfigurationResourceDraft(configurationId: string, resourceCollectionId: string, catalogDigest: string, values: Record<string, unknown>, secrets: Record<string, string> = {}): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/resources/candidates/create`, "POST", { configurationId, resourceCollectionId, catalogDigest, values, ...(Object.keys(secrets).length === 0 ? {} : { secrets }) });
  }
  public updatePluginConfigurationResourceDraft(configurationId: string, resourceCollectionId: string, resourceId: string, catalogDigest: string, values: Record<string, unknown>, secrets: Record<string, string> = {}): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/resources/candidates/update`, "POST", { configurationId, resourceCollectionId, resourceId, catalogDigest, values, ...(Object.keys(secrets).length === 0 ? {} : { secrets }) });
  }
  public deletePluginConfigurationResourceDraft(configurationId: string, resourceCollectionId: string, resourceId: string, catalogDigest: string): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/resources/candidates/delete`, "POST", { configurationId, resourceCollectionId, resourceId, catalogDigest });
  }
  public submitPluginConfigurationResourceDraft(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}/submit-resource`, "POST", { expectedRevision });
  }
  public approvePluginConfigurationResourceCandidate(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}/approve-resource`, "POST", { expectedRevision });
  }
  public activatePluginConfigurationResourceCandidate(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}/activate-resource`, "POST", { expectedRevision });
  }
  public abortPluginConfigurationResourceCandidate(id: string, expectedRevision: number): Promise<PluginConfigurationCandidate> {
    return this.mutate(`${this.basePath}/plugin-configurations/candidates/${segment(id)}/abort-resource`, "POST", { expectedRevision });
  }

  public listCredentials(prefix = ""): Promise<CredentialMetadata[]> { return this.get(`${this.basePath}/credentials${query({ prefix })}`); }
  public listManagedCredentialAudit(beforeId?: number, limit = 100): Promise<ManagedCredentialAuditPage> {
    if ((beforeId !== undefined && (!Number.isSafeInteger(beforeId) || beforeId < 1)) || !Number.isSafeInteger(limit) || limit < 1 || limit > 200) {
      throw new PlatformAdminError(400, "invalid_credential_audit_query");
    }
    return this.get(`${this.basePath}/credentials/managed-audit${query({ beforeId: beforeId === undefined ? undefined : String(beforeId), limit: String(limit) })}`);
  }
  public putCredential(name: string, value: string): Promise<CredentialMetadata> { return this.mutate(`${this.basePath}/credentials/${segment(name)}`, "PUT", { value }); }
  public rotateCredential(name: string): Promise<CredentialMetadata> { return this.mutate(`${this.basePath}/credentials/${segment(name)}/rotate`, "POST", {}); }
  public revokeCredential(name: string): Promise<CredentialMetadata> { return this.mutate(`${this.basePath}/credentials/${segment(name)}/revoke`, "POST", {}); }

  public listDatabaseConnections(): Promise<DatabaseConnection[]> { return this.get(`${this.basePath}/database-connections`); }
  public putDatabaseConnection(name: string, value: PutDatabaseConnectionRequest): Promise<DatabaseConnection> {
    return this.mutate(`${this.basePath}/database-connections/${segment(name)}`, "PUT", value);
  }
  public deleteDatabaseConnection(name: string): Promise<void> { return this.mutate(`${this.basePath}/database-connections/${segment(name)}`, "DELETE").then(() => undefined); }
  public probeDatabaseConnection(name: string): Promise<DatabaseProbe> { return this.mutate(`${this.basePath}/database-connections/${segment(name)}/probe`, "POST", {}); }
  public authenticationProviderState(): Promise<AuthenticationProviderManagementState> { return this.get(`${this.basePath}/authentication-providers`); }
  public createAuthenticationProviderDraft(expectedGeneration: number, profile: AuthenticationProviderProfile): Promise<AuthenticationProviderManagementState> { return this.mutate(`${this.basePath}/authentication-providers`, "POST", { expectedGeneration, profile }); }
  public validateAuthenticationProvider(id: string, expectedGeneration: number): Promise<AuthenticationProviderManagementState> { return this.authenticationProviderAction(id, "validate", { expectedGeneration }); }
  public testAuthenticationProvider(id: string, expectedGeneration: number): Promise<AuthenticationProviderManagementState> { return this.authenticationProviderAction(id, "test", { expectedGeneration }); }
  public approveAuthenticationProvider(id: string, expectedGeneration: number): Promise<AuthenticationProviderManagementState> { return this.authenticationProviderAction(id, "approve", { expectedGeneration }); }
  public retireAuthenticationProvider(id: string, expectedGeneration: number): Promise<AuthenticationProviderManagementState> { return this.authenticationProviderAction(id, "retire", { expectedGeneration }); }
  public publishAuthenticationProviders(request: { expectedGeneration: number; catalogId: string; catalogRevision: number; bindings: unknown[]; accessCatalog: unknown }): Promise<AuthenticationProviderManagementState> { return this.mutate(`${this.basePath}/authentication-providers/publish`, "POST", request); }
  public seedHandoffState(): Promise<SeedHandoffState> { return this.get(`${this.basePath}/seed-handoff`); }
  public configureSeedEnterpriseProvider(expectedGeneration: number, providerProfile: CompositionRef): Promise<SeedHandoffState> { return this.mutate(`${this.basePath}/seed-handoff/configure-provider`, "POST", { expectedGeneration, providerProfile }); }
  public verifySeedEnterpriseProvider(expectedGeneration: number, providerProfile: CompositionRef): Promise<SeedHandoffState> { return this.mutate(`${this.basePath}/seed-handoff/verify-provider`, "POST", { expectedGeneration, providerProfile }); }
  public prepareSeedHandoff(expectedGeneration: number, providerProfile: CompositionRef, recoveryReady: boolean): Promise<SeedHandoffState> { return this.mutate(`${this.basePath}/seed-handoff/prepare`, "POST", { expectedGeneration, providerProfile, recoveryReady }); }
  public completeSeedHandoff(expectedGeneration: number, sealDigest: string): Promise<SeedHandoffState> { return this.mutate(`${this.basePath}/seed-handoff/complete`, "POST", { expectedGeneration, sealDigest }); }
  public artifactRepositoryStatus(): Promise<ArtifactRepositoryStatus> { return this.get(`${this.basePath}/artifacts/status`); }
  public artifactAssessmentInventory(): Promise<ArtifactAssessmentInventory> { return this.get(`${this.basePath}/artifacts/assessment/inventory`); }
  public prepareArtifactAssessmentReport(digest: string): Promise<ArtifactAssessmentReportGrant> {
    if (!/^[a-f0-9]{64}$/.test(digest)) throw new PlatformAdminError(400, "invalid_assessment_report");
    return this.get<ArtifactAssessmentReportGrant>(`${this.basePath}/artifacts/assessment/reports/${digest}`).then((grant) => {
      if (grant.sha256 !== digest || grant.resource !== `/v1/assessment-reports/${digest}`) throw new PlatformAdminError(502, "invalid_assessment_report_grant");
      return grant;
    });
  }
  public listArtifactCatalog(value: ArtifactCatalogQuery = {}): Promise<ArtifactCatalogPage> {
    const page = value.page ?? 1, pageSize = value.pageSize ?? 20;
    if (!Number.isSafeInteger(page) || page < 1 || !Number.isSafeInteger(pageSize) || pageSize < 1 || pageSize > 100) throw new PlatformAdminError(400, "invalid_catalog_query");
    return this.get(`${this.basePath}/artifacts/catalog${query({
      pluginId: value.pluginId, pluginPrefix: value.pluginPrefix, namespace: value.namespace, publisher: value.publisher,
      version: value.version, channel: value.channel, target: value.target, lifecycle: value.lifecycle,
      page: String(page), pageSize: String(pageSize),
    })}`);
  }
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
  public listArtifactPublications(): Promise<ArtifactPublicationPage> { return this.get(`${this.basePath}/artifacts/publications`); }
  public submitArtifactPublication(request: SubmitArtifactPublicationRequest): Promise<ArtifactPublicationResult> { return this.mutate(`${this.basePath}/artifacts/publications`, "POST", request); }
  public approveArtifactPublication(id: string, expectedRevision: number): Promise<ArtifactPublicationResult> { return this.mutate(`${this.basePath}/artifacts/publications/${segment(id)}/approve`, "POST", { expectedRevision }); }
  public rejectArtifactPublication(id: string, expectedRevision: number, reason: string): Promise<ArtifactPublicationResult> { return this.mutate(`${this.basePath}/artifacts/publications/${segment(id)}/reject`, "POST", { expectedRevision, reason }); }
  public cancelArtifactPublication(id: string, expectedRevision: number, reason: string): Promise<ArtifactPublicationResult> { return this.mutate(`${this.basePath}/artifacts/publications/${segment(id)}/cancel`, "POST", { expectedRevision, reason }); }
  public artifactSupplyChainEvidence(ref: ArtifactRef): Promise<ArtifactSupplyChainEvidence> {
    return this.get(`${this.basePath}/artifacts/evidence${query({ pluginId: ref.pluginId, version: ref.version, channel: ref.channel })}`);
  }
  public issueArtifactAssessmentReportTicket(routeKey: string, digest: string): Promise<DataPlaneTicketGrant> {
    if (!/^[a-z2-7]{20}$/.test(routeKey) || !/^[a-f0-9]{64}$/.test(digest)) throw new PlatformAdminError(400, "invalid_assessment_report_ticket");
    return this.mutate<unknown>(`/api/d/${routeKey}/ticket`, "POST", { method: "GET", resource: `/v1/assessment-reports/${digest}` }).then(validateDataPlaneTicketGrant);
  }

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

  public listAPIExposures(): Promise<APIExposureRevision[]> { return this.get<{items:APIExposureRevision[]}>(`${this.basePath}/api-exposures`).then(value=>value.items); }
  public createAPIExposureDraft(request:APIExposureDraftRequest):Promise<APIExposureRevision> { return this.mutate(`${this.basePath}/api-exposures`,"POST",request); }
  public updateAPIExposureDraft(id:number,expectedRevision:number,request:APIExposureDraftRequest):Promise<APIExposureRevision> { return this.mutate(`${this.basePath}/api-exposures/${revision(id)}`,"PUT",{expectedRevision,contract:request.contract,input:request.input}); }
  public submitAPIExposure(id:number):Promise<APIExposureRevision> { return this.apiExposureAction(id,"submit"); }
  public approveAPIExposure(id:number):Promise<APIExposureRevision> { return this.apiExposureAction(id,"approve"); }
  public publishAPIExposure(id:number):Promise<APIExposureRevision> { return this.apiExposureAction(id,"publish"); }
  public retireAPIExposure(exposureId:string):Promise<void> { return this.mutate(`${this.basePath}/api-exposures/exposure/${segment(exposureId)}/retire`,"POST",{}).then(()=>undefined); }
  public listDataPlaneExposures(): Promise<DataPlaneExposureRevision[]> { return this.get<{items:DataPlaneExposureRevision[]}>(`${this.basePath}/data-plane-exposures`).then((value) => value.items); }
  public createDataPlaneExposureDraft(request: DataPlaneExposureDraftRequest): Promise<DataPlaneExposureRevision> { return this.mutate(`${this.basePath}/data-plane-exposures`, "POST", request); }
  public submitDataPlaneExposure(id: number): Promise<DataPlaneExposureRevision> { return this.dataPlaneExposureAction(id, "submit"); }
  public approveDataPlaneExposure(id: number): Promise<DataPlaneExposureRevision> { return this.dataPlaneExposureAction(id, "approve"); }
  public publishDataPlaneExposure(id: number): Promise<DataPlaneExposureRevision> { return this.dataPlaneExposureAction(id, "publish"); }
  public retireDataPlaneExposure(exposureId: string): Promise<void> { return this.mutate(`${this.basePath}/data-plane-exposures/exposure/${segment(exposureId)}/retire`, "POST", {}).then(() => undefined); }

  public getAuthorizationPolicy(): Promise<AuthorizationPolicyState> { return this.get(`${this.basePath}/authorization`); }
  public listAuthorizationAudit(): Promise<AuthorizationAuditEvent[]> { return this.get(`${this.basePath}/authorization/audit`); }
  public createAuthorizationRole(request: CreateAuthorizationRoleRequest): Promise<{role:AuthorizationRoleRevision;generation:number}> { return this.mutate(`${this.basePath}/authorization/roles`, "POST", request); }
  public updateAuthorizationRole(id:string, revisionId:number, request:Omit<CreateAuthorizationRoleRequest,"id"|"domainId">): Promise<{role:AuthorizationRoleRevision;generation:number}> { return this.mutate(`${this.basePath}/authorization/roles/${segment(id)}/${revision(revisionId)}`, "PUT", request); }
  public transitionAuthorizationRole(id:string, revisionId:number, action:"submit"|"approve"|"publish"|"retire", expectedGeneration:number, reason=""): Promise<{role:AuthorizationRoleRevision;generation:number}> { return this.mutate(`${this.basePath}/authorization/roles/${segment(id)}/${revision(revisionId)}/${action}`, "POST", {expectedGeneration,reason}); }
  public createAuthorizationBinding(request:CreateAuthorizationBindingRequest):Promise<{binding:AuthorizationBindingRevision;generation:number}> { return this.mutate(`${this.basePath}/authorization/bindings`,"POST",request); }
  public updateAuthorizationBinding(id:string, revisionId:number, request:UpdateAuthorizationBindingRequest):Promise<{binding:AuthorizationBindingRevision;generation:number}> { return this.mutate(`${this.basePath}/authorization/bindings/${segment(id)}/${revision(revisionId)}`,"PUT",request); }
  public transitionAuthorizationBinding(id:string, revisionId:number, action:"submit"|"approve"|"publish"|"retire", expectedGeneration:number, reason=""):Promise<{binding:AuthorizationBindingRevision;generation:number}> { return this.mutate(`${this.basePath}/authorization/bindings/${segment(id)}/${revision(revisionId)}/${action}`,"POST",{expectedGeneration,reason}); }
  public revokeAuthorization(request:{expectedGeneration:number;id:string;kind:"subject"|"binding"|"role";targetId:string;effectiveAt:string;reasonCode:string}):Promise<unknown> { return this.mutate(`${this.basePath}/authorization/revocations`,"POST",request); }
  public publishAuthorizationSnapshot(expectedGeneration:number,audience:string[]=[],ttlSeconds=300,reason="policy update"):Promise<unknown> { return this.mutate(`${this.basePath}/authorization/snapshots`,"POST",{expectedGeneration,audience,ttlSeconds,reason}); }

  private serviceRevisionAction(id: number, action: string): Promise<ServiceRevision> {
    return this.mutate(`${this.basePath}/deployment/service-revisions/${revision(id)}/${action}`, "POST", {});
  }

  private apiExposureAction(id:number,action:"submit"|"approve"|"publish"):Promise<APIExposureRevision> { return this.mutate(`${this.basePath}/api-exposures/${revision(id)}/${action}`,"POST",{}); }
  private dataPlaneExposureAction(id: number, action: "submit" | "approve" | "publish"): Promise<DataPlaneExposureRevision> { return this.mutate(`${this.basePath}/data-plane-exposures/${revision(id)}/${action}`, "POST", {}); }

  private artifactMigrationAction(id: string, action: string, body: Record<string, unknown> = {}): Promise<ArtifactRepositoryMigration> {
    return this.mutate(`${this.basePath}/artifacts/migrations/${segment(id)}/${action}`, "POST", body);
  }

  private authenticationProviderAction(id: string, action: string, body: Record<string, unknown>): Promise<AuthenticationProviderManagementState> {
    return this.mutate(`${this.basePath}/authentication-providers/${segment(id)}/${action}`, "POST", body);
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

function validateDataPlaneTicketGrant(value: unknown): DataPlaneTicketGrant {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new PlatformAdminError(502, "invalid_data_plane_ticket");
  const record = value as Record<string, unknown>;
  if (typeof record.endpoint !== "string" || typeof record.leaseId !== "string" || typeof record.ticket !== "string" || typeof record.expiresAt !== "string") throw new PlatformAdminError(502, "invalid_data_plane_ticket");
  let endpoint: URL;
  try { endpoint = new URL(record.endpoint); } catch { throw new PlatformAdminError(502, "invalid_data_plane_ticket"); }
  const expiresAt = Date.parse(record.expiresAt);
  const now = Date.now();
  if (endpoint.protocol !== "https:" || endpoint.username !== "" || endpoint.password !== "" || endpoint.search !== "" || endpoint.hash !== "" || !/^[A-Za-z0-9_-]{43}$/.test(record.ticket) || !Number.isFinite(expiresAt) || expiresAt <= now || expiresAt > now + 35_000) {
    throw new PlatformAdminError(502, "invalid_data_plane_ticket");
  }
  return { endpoint: endpoint.toString(), leaseId: record.leaseId, ticket: record.ticket, expiresAt: record.expiresAt };
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

function query(values: Record<string, string | undefined>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) if (value !== undefined && value !== "") params.set(key, value);
  const encoded = params.toString();
  return encoded === "" ? "" : `?${encoded}`;
}
