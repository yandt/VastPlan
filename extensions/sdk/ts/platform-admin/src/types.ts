import type { BackendApplicationIntent, BackendResolutionReport } from "@vastplan/composition-planning";

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
  serviceBaselineId?: string;
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
export type AuthorizationPermissionSelectorKind = "exact" | "glob";
export interface AuthorizationPermissionSelector { kind: AuthorizationPermissionSelectorKind; value: string; }
export interface AuthorizationStatementInput {
  id: string; effect: "allow" | "deny"; permissionSelectors: AuthorizationPermissionSelector[];
  resource?: AuthorizationStatement["resource"]; constraints: AuthorizationStatement["constraints"];
}
export interface AuthorizationStatementPermissionSelectors {
  statementId: string; selectors: AuthorizationPermissionSelector[];
}
export interface AuthorizationRoleRevision {
  id: string; revision: number; domainId: string; title: string; description?: string; statements: AuthorizationStatement[];
  selectorCatalogDigest: string; permissionSelectors: AuthorizationStatementPermissionSelectors[];
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
export interface CreateAuthorizationRoleRequest { expectedGeneration: number; id: string; domainId: string; title: string; description?: string; statements: AuthorizationStatementInput[]; }
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
  intent?: BackendApplicationIntent; resolutionReport?: BackendResolutionReport;
  planningStale?: boolean; planningStaleReason?: string; observedPlanDigest?: string;
  submittedPlanDigest?: string; approvedPlanDigest?: string;
  composition: BackendApplicationComposition; preview: Record<string, unknown>; previewDigest: string; kvRevision?: number;
  artifactReferences: ArtifactReference[];
  referencePending?: boolean;
  configurationCandidateId?: string; configurationId?: string; previousServiceRevision?: number; rollbackServiceRevision?: number;
  submittedBy?: string; approvedBy?: string; publishedBy?: string; createdAt: string; updatedAt: string;
}
export interface ServiceAuditEvent { id: number; revisionId: number; deployment: string; action: string; actorId: string; intentDigest?: string; planDigest?: string; previewDigest?: string; at: string; }
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
