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
    apis?: Array<{ id: string; contractId: string; contractVersion: string; contractDigest: string }>;
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

export interface PortalPlatformProfile {
  version: 1;
  revision: number;
  id: string;
  target: { kernel: "frontend" };
  runtimeEngine: PortalPluginRef & { engineContract: string; family: string };
  renderAdapter: PortalPluginRef & { uiContract: string; config: { defaultRenderer: string; allowedRenderers: string[]; userSelectable: boolean; rendererOptions?: Record<string, { themeTemplate?: string; allowedThemeTemplates?: string[]; themeUserSelectable?: boolean; iconTheme?: string; allowedIconThemes?: string[]; iconUserSelectable?: boolean }> } };
  shell: PortalPluginRef & { uiContract: string; config: { navigationGroups?: JSONValue; navigationPlacements?: JSONValue; defaultTemplate: string; allowedTemplates: string[]; userSelectable: boolean; templateOptions?: Record<string, Record<string, JSONValue>> } };
  workbench: PortalPluginRef & { uiContract: string };
  accountCenter: PortalPluginRef;
  localization?: { defaultLocale: string; supportedLocales: string[] };
  plugins: PortalPluginRef[];
  security: { firstPartyOnly: true; requireIntegrity: true };
}

export interface PortalConfiguration {
  platform: PortalPlatformProfile;
  application: PortalApplicationComposition;
  services: PortalManagementBinding["services"];
}

export type PortalRevisionStatus = "Draft" | "PendingApproval" | "Approved" | "Published";

export interface PortalWorkingCopy {
  tenantId: string;
  portalId: string;
  revision: number;
  configuration: PortalConfiguration;
  digest: string;
  updatedBy?: string;
  createdAt: string;
  updatedAt: string;
}

export interface PortalVersionRef {
  stream: { namespace: string; streamId: string };
  versionId: string;
  sequence: number;
  contentDigest: string;
}

export interface PortalPublicationSource {
  kind: "inline" | "workspace";
  configuration?: PortalConfiguration;
  environmentId?: string;
  environmentDigest?: string;
  versionRef?: PortalVersionRef;
}

export interface PortalPublication {
  id: number;
  tenantId: string;
  portalId: string;
  workingRevision: number;
  status: PortalRevisionStatus;
  digest: string;
  source: PortalPublicationSource;
  resolved: PortalResolvedSpec;
  submittedBy: string;
  approvedBy?: string;
  publishedBy?: string;
  createdAt: string;
  updatedAt: string;
  approval?: PortalApprovalDecision;
}

export type PortalApprovalDecisionStatus = "allowed" | "review-required" | "denied";
export interface PortalApprovalProfileRef {
  id: string;
  revision: number;
  digest: string;
}
export type PortalApprovalEvidenceKind = "exact-fact-match" | "boolean-true" | "text-length";
export interface PortalApprovalEvidenceRequirement {
  id: string;
  field: string;
  kind: PortalApprovalEvidenceKind;
  fact?: string;
  minLength?: number;
  maxLength?: number;
  title?: string;
  audit?: boolean;
}
export interface PortalApprovalDecision {
  status: PortalApprovalDecisionStatus;
  profile: PortalApprovalProfileRef;
  matchedRuleId?: string;
  code?: string;
  message?: string;
  requirements?: PortalApprovalEvidenceRequirement[];
}
export interface PortalApprovalReview {
  expectedDigest: string;
  acknowledged: boolean;
  reason: string;
}

export interface PortalResolvedSpec {
    revision: number;
    id: string;
    tenantId: string;
    route: string;
    domains?: string[];
    audience?: string[];
    branding?: Record<string, JSONValue>;
    runtimeEngine: PortalPluginRef & { engineContract: string; family: string };
    renderAdapter: PortalPluginRef & { uiContract: string; config: { defaultRenderer: string; allowedRenderers: string[]; userSelectable: boolean; rendererOptions?: Record<string, { themeTemplate?: string; allowedThemeTemplates?: string[]; themeUserSelectable?: boolean; iconTheme?: string; allowedIconThemes?: string[]; iconUserSelectable?: boolean }> } };
    shell: PortalPluginRef & { uiContract: string; config: { navigationGroups?: JSONValue; navigationPlacements?: JSONValue; defaultTemplate: string; allowedTemplates: string[]; userSelectable: boolean; templateOptions?: Record<string, Record<string, JSONValue>> } };
    workbench: PortalPluginRef & { uiContract: string };
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
}

export type PortalReleaseStatus = "Preparing" | "Activating" | "Current" | "Superseded" | "Failed";
export interface PortalReleasePhase { name: string; status: "Succeeded" | "Failed"; message?: string; startedAt: string; endedAt?: string; }
export interface PortalArtifactReference {
  ref: { pluginId: string; version: string; channel: string };
  sha256: string;
  purpose: string;
}
export interface PortalRelease {
  id: number;
  tenantId: string;
  portalId: string;
  publicationId: number;
  status: PortalReleaseStatus;
  previousReleaseId?: number;
  resolved: PortalResolvedSpec;
  artifactReferences?: PortalArtifactReference[];
  referencePending?: boolean;
  phases: PortalReleasePhase[];
  actorId: string;
  reason?: string;
  createdAt: string;
}

export interface Portal {
  id: string;
  tenantId: string;
  workingCopy?: PortalWorkingCopy;
  pendingPublication?: PortalPublication;
  publishedPublication?: PortalPublication;
  versionControl: { enabled: boolean; availability: "disabled" | "available" | "unavailable"; capabilities: string[] };
  releases: PortalRelease[];
  currentReleaseId?: number;
  createdAt: string;
  updatedAt: string;
}

export interface PortalGovernance {
  portals: Portal[];
  creationTemplate?: PortalConfiguration;
}

export interface PortalReleaseRequest {
  publicationId: number;
  expectedCurrentReleaseId: number;
  reason?: string;
}

export interface PortalVersionHistoryEntry {
  publicationId: number;
  environmentId: string;
  environmentDigest: string;
  versionRef: PortalVersionRef;
  configurationDigest: string;
  actorId: string;
  createdAt: string;
}

export interface PortalVersionHistory { portalId: string; entries: PortalVersionHistoryEntry[]; }
export interface PortalVersionSnapshot { entry: PortalVersionHistoryEntry; configuration: PortalConfiguration; }
export interface PortalVersionComparison {
  left: PortalVersionHistoryEntry;
  right: PortalVersionHistoryEntry;
  dirty: boolean;
  diffAvailable: boolean;
  changedPaths?: string[];
  summary: { added: number; modified: number; removed: number; total: number };
}

export type PortalTestTargetScope = "application-plugin" | "platform-profile-plugin";
export interface PortalTestTargetBinding {
  id: string;
  tenantId: string;
  scope: PortalTestTargetScope;
  portalId: string;
  pluginId: string;
  allowedPublishers: string[];
  enabled: boolean;
  version: number;
  createdAt: string;
  updatedAt: string;
}
export interface PortalPutTestTargetBindingRequest {
  scope: PortalTestTargetScope;
  portalId: string;
  pluginId: string;
  allowedPublishers: string[];
  enabled: boolean;
  ifVersion?: number;
}
export type PortalTestReleaseStatus = "Queued" | "Resolving" | "Preparing" | "Validating" | "Activating" | "Ready" | "RollingBack" | "RolledBack" | "Failed" | "Superseded";
export interface PortalArtifactRepositoryReceipt {
  schemaVersion: 1;
  repositoryId: string;
  protocol: "artifact.repository.local-test.v1" | "artifact.repository.remote.v1";
  profileDigest: string;
  ref: { pluginId: string; version: string; channel: string };
  sha256: string;
  revision: number;
  workspaceLease?: string;
  expiresAt?: string;
}
export interface PortalTestReleaseRequest {
  bindingId: string;
	receipt: PortalArtifactRepositoryReceipt;
}
export interface PortalTestRelease extends PortalTestReleaseRequest {
  id: number;
  tenantId: string;
  status: PortalTestReleaseStatus;
  previousReleaseId?: number;
  candidatePortalVersionId?: number;
  candidateReleaseId?: number;
  rollbackReleaseId?: number;
  rollbackRequired?: boolean;
  errorCode?: string;
  errorMessage?: string;
  requestedBy: string;
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
  private readonly testingPath: string;

  public constructor(private readonly options: PortalControlClientOptions) {
    this.basePath = options.basePath ?? "/v1/portals";
    this.csrfPath = options.csrfPath ?? "/v1/csrf";
    this.testingPath = "/v1/portal-governance";
  }

  public governance(): Promise<PortalGovernance> {
    return this.call<PortalGovernance>(this.basePath, { method: "GET" });
  }

  public createPortal(portalId: string, configuration: PortalConfiguration): Promise<Portal> {
    return this.mutate<Portal>(this.basePath, "POST", { portalId: this.validResourceID(portalId), configuration });
  }

  public createPortalWorkingCopy(portalId: string, configuration: PortalConfiguration): Promise<PortalWorkingCopy> {
    return this.mutate<PortalWorkingCopy>(`${this.portalPath(portalId)}/working-copy`, "POST", { configuration });
  }

  public savePortalWorkingCopy(portalId: string, expectedRevision: number, configuration: PortalConfiguration): Promise<PortalWorkingCopy> {
    return this.mutate<PortalWorkingCopy>(`${this.portalPath(portalId)}/working-copy`, "PUT", { expectedRevision: this.validID(expectedRevision), configuration });
  }

  public submitPortalPublication(portalId: string, expectedWorkingRevision: number): Promise<PortalPublication> {
    return this.mutate<PortalPublication>(`${this.portalPath(portalId)}/publications`, "POST", { expectedWorkingRevision: this.validID(expectedWorkingRevision) });
  }

  public approvePortalPublication(portalId: string, publicationId: number, review?: PortalApprovalReview): Promise<PortalPublication> {
		return this.mutate<PortalPublication>(`${this.portalPath(portalId)}/publications/${this.validID(publicationId)}/approve`, "POST", review === undefined ? {} : { review });
  }

  public publishPortalPublication(portalId: string, publicationId: number): Promise<PortalPublication> {
    return this.mutate<PortalPublication>(`${this.portalPath(portalId)}/publications/${this.validID(publicationId)}/publish`, "POST", {});
  }

  public releasePortalPublication(portalId: string, request: PortalReleaseRequest): Promise<PortalRelease> {
    return this.mutate<PortalRelease>(`${this.portalPath(portalId)}/releases`, "POST", request);
  }

  public rollbackPortalRelease(portalId: string, sourceId: number, expectedCurrentReleaseId: number, reason: string): Promise<PortalRelease> {
    return this.mutate<PortalRelease>(`${this.portalPath(portalId)}/releases/${this.validID(sourceId)}/rollback`, "POST", { expectedCurrentReleaseId, reason });
  }

  public auditPortalPublication(portalId: string, id: number): Promise<PortalAuditEvent[]> {
    return this.call<PortalAuditEvent[]>(`${this.portalPath(portalId)}/publications/${this.validID(id)}/audit`, { method: "GET" });
  }
  public portalVersionHistory(portalId: string): Promise<PortalVersionHistory> {
    return this.call<PortalVersionHistory>(`${this.portalPath(portalId)}/history`, { method: "GET" });
  }
  public readPortalVersion(portalId: string, versionId: string): Promise<PortalVersionSnapshot> {
    return this.call<PortalVersionSnapshot>(`${this.portalPath(portalId)}/history/${encodeURIComponent(this.validVersionID(versionId))}`, { method: "GET" });
  }
  public comparePortalVersions(portalId: string, leftVersionId: string, rightVersionId: string): Promise<PortalVersionComparison> {
    const query = new URLSearchParams({ left: this.validVersionID(leftVersionId), right: this.validVersionID(rightVersionId) });
    return this.call<PortalVersionComparison>(`${this.portalPath(portalId)}/compare?${query.toString()}`, { method: "GET" });
  }
  public restorePortalVersion(portalId: string, versionId: string, expectedWorkingRevision: number): Promise<PortalWorkingCopy> {
    return this.mutate<PortalWorkingCopy>(`${this.portalPath(portalId)}/history/${encodeURIComponent(this.validVersionID(versionId))}/restore`, "POST", {
      expectedWorkingRevision: this.validID(expectedWorkingRevision),
    });
  }
  public listTestTargetBindings(): Promise<PortalTestTargetBinding[]> {
    return this.call<PortalTestTargetBinding[]>(`${this.testingPath}/test-target-bindings`, { method: "GET" });
  }
  public putTestTargetBinding(id: string, request: PortalPutTestTargetBindingRequest): Promise<PortalTestTargetBinding> {
    return this.mutate<PortalTestTargetBinding>(`${this.testingPath}/test-target-bindings/${this.validResourceID(id)}`, "PUT", request);
  }
  public listTestReleases(): Promise<PortalTestRelease[]> {
    return this.call<PortalTestRelease[]>(`${this.testingPath}/test-releases`, { method: "GET" });
  }
  public createTestRelease(request: PortalTestReleaseRequest): Promise<PortalTestRelease> {
    return this.mutate<PortalTestRelease>(`${this.testingPath}/test-releases`, "POST", request);
  }
  public rollbackTestRelease(id: number): Promise<PortalTestRelease> {
    return this.mutate<PortalTestRelease>(`${this.testingPath}/test-releases/${this.validID(id)}/rollback`, "POST", {});
  }

  private portalPath(portalId: string): string {
	return `${this.basePath}/${this.validResourceID(portalId)}`;
  }

  private validID(id: number): number {
    if (!Number.isSafeInteger(id) || id <= 0) throw new PortalControlError(400, "invalid_revision");
    return id;
  }

  private validResourceID(id: string): string {
    if (!/^[a-z0-9][a-z0-9._-]{0,127}$/.test(id)) throw new PortalControlError(400, "invalid_resource_id");
    return id;
  }

  private validVersionID(id: string): string {
    if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,159}$/.test(id)) throw new PortalControlError(400, "invalid_version_id");
    return id;
  }

  private async mutate<T>(path: string, method: "POST" | "PUT" | "DELETE", body: unknown): Promise<T> {
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
    super(portalControlErrorMessage(code));
    this.name = "PortalControlError";
  }
}

function portalControlErrorMessage(code: string): string {
  const message: Record<string, string> = {
    approval_separation_required: "该内容由当前账号提交，需要其他审批人批准。",
    approval_review_required: "当前审批需要先完成冻结内容复验。",
    approval_evidence_mismatch: "审批证据与当前冻结内容不一致，请刷新后重试。",
    approval_provider_unavailable: "审批服务暂时不可用，请稍后重试。",
    approval_digest_mismatch: "待审批内容已经变化，请刷新页面后重新复验。",
    approval_reason_required: "请填写至少 4 个字符的审批原因。",
    portal_catalog_rejected: "待发布内容引用的插件制品已缺失、失效或不再满足可信目录要求，请刷新配置后重新提交审批。",
  };
  return message[code] ?? `Portal control request failed: ${code}`;
}
