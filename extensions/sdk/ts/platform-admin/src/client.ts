import type { BackendApplicationIntent } from "@vastplan/composition-planning";
import type {
	APIExposureDraftRequest, APIExposureRevision, APIExposureStatus, ArtifactAssessmentInventory, ArtifactAssessmentReportGrant, ArtifactAssessmentRevisionStatus,
	ArtifactCapacity, ArtifactCapacityBucket, ArtifactCatalogEntry, ArtifactCatalogPage, ArtifactCatalogQuery, ArtifactGCBlocker, PluginMarketplaceCatalogPage, PluginMarketplaceCatalogQuery, PluginMarketplaceSources,
	ArtifactGCCandidate, ArtifactGCPlan, ArtifactGCRecord, ArtifactGCStatus, ArtifactLifecycleRequest, ArtifactLifecycleResult,
	ArtifactProvenanceDeclaration, ArtifactPublication, ArtifactPublicationPage, ArtifactPublicationResult, ArtifactPublicationStatus, ArtifactPythonLockDeclaration,
	ArtifactQuotaUsage, ArtifactRef, ArtifactReference, ArtifactReferencePage, ArtifactReferenceSnapshot, ArtifactReferenceSnapshotValue,
	ArtifactRepositoryMigration, ArtifactRepositoryReceipt, ArtifactRepositoryStatus, ArtifactRequirement, ArtifactSecurityAdmissionDeclaration, ArtifactSecurityStatusEvidence,
	ArtifactSupplyChainEvidence, AuthenticationProviderManagementState, AuthenticationProviderProfile, AuthenticationProviderReadiness, AuthenticationProviderState, AuthorizationAuditEvent,
	AuthorizationBindingRevision, AuthorizationLifecycleState, AuthorizationPermission, AuthorizationPolicyState, AuthorizationRoleRevision,
	BackendApplicationComposition, BackendPluginRef, BackendServiceUnit, BootstrapJob, BootstrapJobState, CompositionRef,
	CreateAuthorizationBindingRequest, CreateAuthorizationRoleRequest, CreateTestReleaseRequest, CredentialMetadata, DataPlaneExposureDraftRequest, DataPlaneExposureRevision,
	DataPlaneTicketGrant, DatabaseConnection, DatabasePoolPolicy, DatabaseProbe, DeploymentTarget, ManagedAuthenticationProvider,
	ManagedCredentialAuditEvent, ManagedCredentialAuditPage, ManagedCredentialMaintenanceStatus, ManagedNode, NodeBootstrapPlan, PlatformFetch,
	PlatformControlChangeRequest, PlatformControlStatus, PlatformFetchResponse, PluginConfigurationCandidate, PluginConfigurationCandidateStatus, PluginConfigurationDefinition, PluginConfigurationResourceCollection, PluginConfigurationResourceItem,
	PluginConfigurationResourcePage, PluginConfigurationResourceResponse, PrepareArtifactMigrationRequest, PutDatabaseConnectionRequest, PutTestTargetBindingRequest, SeedHandoffState,
	PluginInstallationCandidate, PluginInstallationPreview, PluginInstallationPreviewRequest, PluginInstallationTargetOption, SelfServicePluginInstallationRequest, ServiceAuditEvent, ServiceRevision, ServiceRevisionStatus, Setting, SubmitArtifactPublicationRequest, TestRelease,
	TestReleaseStatus, TestTargetBinding, UpdateAuthorizationBindingRequest,
} from "./types.js";
import { CSRFTokenCache, withCSRF } from "./csrf-token-cache.js";

export class PlatformAdminClient {
	private readonly basePath: string;
	private readonly csrf: CSRFTokenCache;
	public constructor(private readonly fetcher: PlatformFetch, portalID: string, serviceID: string, private readonly csrfPath = "/v1/csrf") {
		this.basePath = `/v1/portals/${segment(portalID)}/platform/services/${segment(serviceID)}`;
		this.csrf = new CSRFTokenCache(async () => {
			const value = await this.get<{ token: string }>(this.csrfPath);
			if (!value.token) throw new PlatformAdminError(403, "csrf_required");
			return value.token;
		});
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
  public testDatabaseConnection(name: string, value: PutDatabaseConnectionRequest): Promise<DatabaseProbe> {
    return this.mutate(`${this.basePath}/database-connections/${segment(name)}/test`, "POST", value);
  }
  public platformControlStatus(): Promise<PlatformControlStatus> { return this.get(`${this.basePath}/platform-control`); }
  public testPlatformControl(value: PlatformControlChangeRequest): Promise<PlatformControlStatus> { return this.mutate(`${this.basePath}/platform-control/test`, "POST", value); }
  public configurePlatformControl(value: PlatformControlChangeRequest): Promise<PlatformControlStatus> { return this.mutate(`${this.basePath}/platform-control`, "PUT", value); }
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
  public listPluginMarketplaceSources(): Promise<PluginMarketplaceSources> { return this.get(`${this.basePath}/marketplace/sources`); }
  public listPluginMarketplaceCatalog(value: PluginMarketplaceCatalogQuery): Promise<PluginMarketplaceCatalogPage> {
    const page = value.page ?? 1, pageSize = value.pageSize ?? 20;
    if (!/^[a-z][a-z0-9._-]{0,127}$/.test(value.sourceId) || !Number.isSafeInteger(page) || page < 1 || !Number.isSafeInteger(pageSize) || pageSize < 1 || pageSize > 100) throw new PlatformAdminError(400, "invalid_marketplace_query");
    return this.get(`${this.basePath}/marketplace/catalog${query({
      sourceId: value.sourceId, pluginId: value.pluginId, pluginPrefix: value.pluginPrefix, namespace: value.namespace, publisher: value.publisher,
      version: value.version, channel: value.channel, target: value.target, lifecycle: value.lifecycle, page: String(page), pageSize: String(pageSize),
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
  public previewPluginInstallation(request: PluginInstallationPreviewRequest): Promise<PluginInstallationPreview> {
    return this.mutate(`${this.basePath}/deployment/plugin-installations/preview`, "POST", request);
  }
  public listPluginInstallationCandidates(): Promise<PluginInstallationCandidate[]> {
    return this.get(`${this.basePath}/deployment/plugin-installations`);
  }
  public listPluginInstallationTargets(): Promise<PluginInstallationTargetOption[]> {
    return this.get(`${this.basePath}/deployment/plugin-installations/targets`);
  }
  public getPluginInstallationCandidate(id: string): Promise<PluginInstallationCandidate> {
    return this.get(`${this.basePath}/deployment/plugin-installations/${segment(id)}`);
  }
  public createPluginInstallationCandidate(request: PluginInstallationPreviewRequest): Promise<PluginInstallationCandidate> {
    return this.mutate(`${this.basePath}/deployment/plugin-installations`, "POST", request);
  }
  public submitPluginInstallationCandidate(id: string): Promise<PluginInstallationCandidate> { return this.pluginInstallationAction(id, "submit"); }
  public approvePluginInstallationCandidate(id: string, evidence: Readonly<Record<string, unknown>> = {}): Promise<PluginInstallationCandidate> { return this.pluginInstallationAction(id, "approve", { evidence }); }
  public activatePluginInstallationCandidate(id: string): Promise<PluginInstallationCandidate> { return this.pluginInstallationAction(id, "activate"); }
  public cancelPluginInstallationCandidate(id: string): Promise<PluginInstallationCandidate> { return this.pluginInstallationAction(id, "cancel"); }
  public rollbackPluginInstallationCandidate(id: string): Promise<PluginInstallationCandidate> { return this.pluginInstallationAction(id, "rollback"); }
  public previewSelfServicePluginInstallation(request: SelfServicePluginInstallationRequest): Promise<PluginInstallationPreview> { return this.mutate(`${this.basePath}/deployment/service-plugin-installations/preview`, "POST", request); }
  public listSelfServicePluginInstallationCandidates(): Promise<PluginInstallationCandidate[]> { return this.get(`${this.basePath}/deployment/service-plugin-installations`); }
  public getSelfServicePluginInstallationCandidate(id: string): Promise<PluginInstallationCandidate> { return this.get(`${this.basePath}/deployment/service-plugin-installations/${segment(id)}`); }
  public createSelfServicePluginInstallationCandidate(request: SelfServicePluginInstallationRequest): Promise<PluginInstallationCandidate> { return this.mutate(`${this.basePath}/deployment/service-plugin-installations`, "POST", request); }
  public submitSelfServicePluginInstallationCandidate(id: string): Promise<PluginInstallationCandidate> { return this.selfServicePluginInstallationAction(id, "submit"); }
  public approveSelfServicePluginInstallationCandidate(id: string, evidence: Readonly<Record<string, unknown>> = {}): Promise<PluginInstallationCandidate> { return this.selfServicePluginInstallationAction(id, "approve", { evidence }); }
  public activateSelfServicePluginInstallationCandidate(id: string): Promise<PluginInstallationCandidate> { return this.selfServicePluginInstallationAction(id, "activate"); }
  public cancelSelfServicePluginInstallationCandidate(id: string): Promise<PluginInstallationCandidate> { return this.selfServicePluginInstallationAction(id, "cancel"); }
  public rollbackSelfServicePluginInstallationCandidate(id: string): Promise<PluginInstallationCandidate> { return this.selfServicePluginInstallationAction(id, "rollback"); }
  public createIntentDraft(intent: BackendApplicationIntent): Promise<ServiceRevision> {
    return this.mutate(`${this.basePath}/deployment/service-revisions`, "POST", { intent });
  }
  public updateIntentDraft(id: number, intent: BackendApplicationIntent): Promise<ServiceRevision> {
    return this.mutate(`${this.basePath}/deployment/service-revisions/${revision(id)}`, "PUT", { intent });
  }
  public refreshIntentDraft(id: number): Promise<ServiceRevision> {
    return this.serviceRevisionAction(id, "refresh-plan");
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

  private pluginInstallationAction(id: string, action: "submit" | "approve" | "activate" | "cancel" | "rollback", body: Readonly<Record<string, unknown>> = {}): Promise<PluginInstallationCandidate> {
    return this.mutate(`${this.basePath}/deployment/plugin-installations/${segment(id)}/${action}`, "POST", body);
  }
  private selfServicePluginInstallationAction(id: string, action: "submit" | "approve" | "activate" | "cancel" | "rollback", body: Readonly<Record<string, unknown>> = {}): Promise<PluginInstallationCandidate> {
    return this.mutate(`${this.basePath}/deployment/service-plugin-installations/${segment(id)}/${action}`, "POST", body);
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
    return withCSRF(this.csrf, (token) => this.call<T>(path, {
      method,
      headers: { "Content-Type": "application/json", "X-VastPlan-CSRF": token },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    }), isCSRFRejected);
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
    super(platformAdminErrorMessage(code));
    this.name = "PlatformAdminError";
  }
}

function platformAdminErrorMessage(code: string): string {
  if (code === "database_connection_invalid") return "数据库连接配置无效，请检查地址、端口、用户名和传输加密设置。";
  if (code === "database_connection_failed") return "数据库连接未能建立，请检查网络、账户密码和服务端状态。";
  if (code === "database_credential_unavailable") return "数据库密码不可用或已经失效，请重新输入密码后重试。";
  if (code === "database_credential_service_unavailable") return "数据库凭证服务暂时不可用，请稍后重试。";
  if (code === "database_tls_policy_forbidden") return "当前部署策略不允许关闭数据库传输加密校验。";
  if (code === "database_name_resolution_failed") return "数据库地址无法解析，请检查主机名或 DNS 配置。";
  if (code === "database_connection_refused") return "数据库服务器拒绝了连接，请检查地址、端口和服务监听状态。";
  if (code === "database_connection_timeout") return "连接数据库超时，请检查网络和连接超时设置。";
  if (code === "database_tls_verification_failed") return "数据库传输加密或证书校验失败，请检查证书与服务器名称。";
  if (code === "database_authentication_failed") return "数据库用户名或密码验证失败。";
  if (code === "database_not_found") return "指定的数据库不存在。";
  if (code === "database_permission_denied") return "数据库账户没有所需权限。";
  if (code === "database_pool_exhausted") return "数据库连接资源暂时不足，请稍后重试。";
  if (code === "database_runtime_unavailable") return "数据库运行服务暂时不可用，请稍后重试。";
  if (code === "platform_control_invalid") return "平台控制数据库配置无效，请检查数据库地址、凭证引用和契约范围。";
  if (code === "platform_control_secret_unavailable") return "平台控制数据库密码引用不可用，请检查 systemd credential 或受保护文件。";
  if (code === "platform_control_database_unavailable") return "平台控制数据库连接失败，请检查网络、账户、传输加密和数据库权限。";
  if (code === "platform_control_initialization_failed") return "平台控制数据库初始化失败，请检查建表权限和迁移日志。";
  if (code === "platform_control_conflict") return "平台控制数据库配置已被其他节点更新，请刷新后重试。";
  return `Platform administration request failed: ${code}`;
}

function isCSRFRejected(error: unknown): boolean {
  return error instanceof PlatformAdminError && error.status === 403 && error.code === "csrf_rejected";
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
