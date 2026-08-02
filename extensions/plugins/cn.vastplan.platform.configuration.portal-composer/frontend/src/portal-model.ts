import type { Portal, PortalConfiguration, PortalPublication, PortalRevisionStatus } from "@vastplan/ui-primitives";
import { profileSummary } from "./portal-form";

export type PortalRow = Record<string, unknown> & {
  id: string;
  portal: Portal;
  configuration: PortalConfiguration;
  status: PortalRevisionStatus;
  statusLabel: string;
  workingRevision: number;
  publicationId: number;
  releasePublicationId: number;
  currentReleaseId: number;
  route: string;
  renderer: string;
  layout: string;
  releaseAvailable: boolean;
  hasRollback: boolean;
  canEdit: boolean;
  canSubmit: boolean;
  canApproveDirect: boolean;
  canApproveWithReview: boolean;
  approvalLabel: string;
  approvalReason: string;
  canPublish: boolean;
  canCreateWorkingCopy: boolean;
  auditAvailable: boolean;
  historyAvailable: boolean;
  diffAvailable: boolean;
  restoreAvailable: boolean;
  versionControlAvailability: "disabled" | "available" | "unavailable";
  updatedAt: string;
};

export const statusLabels: Record<PortalRevisionStatus, string> = {
  Draft: "工作副本",
  PendingApproval: "待审批",
  Approved: "已批准",
  Published: "已发布",
};

export const versionControlLabels = { disabled: "未启用", available: "可用", unavailable: "不可用" } as const;

export function toPortalRow(portal: Portal): PortalRow[] {
  const active = activeConfiguration(portal);
  if (active === undefined) return [];
  const summary = profileSummary(active.configuration.platform);
  const releases = portal.releases ?? [];
  const published = portal.publishedPublication;
  const releasedPublicationIDs = new Set(releases.filter((release) => release.status === "Current" || release.status === "Superseded").map((release) => release.publicationId));
  const releaseAvailable = published !== undefined && !releasedPublicationIDs.has(published.id);
  const capabilities = new Set(portal.versionControl.capabilities ?? []);
  const versionControlAvailable = portal.versionControl.enabled && portal.versionControl.availability === "available";
  const approval = portal.pendingPublication?.approval;
  return [{
    id: portal.id,
    portal,
    configuration: active.configuration,
    status: active.status,
    statusLabel: statusLabels[active.status],
    workingRevision: portal.workingCopy?.revision ?? 0,
    publicationId: active.publication?.id ?? 0,
    releasePublicationId: releaseAvailable ? published.id : 0,
    currentReleaseId: portal.currentReleaseId ?? 0,
    route: active.configuration.application.route,
    renderer: summary.renderer,
    layout: summary.layout,
    releaseAvailable,
    hasRollback: releases.some((release) => release.status === "Superseded"),
    canEdit: portal.workingCopy !== undefined,
    canSubmit: portal.workingCopy !== undefined,
    canApproveDirect: portal.pendingPublication?.status === "PendingApproval" && approval?.status === "allowed",
    canApproveWithReview: portal.pendingPublication?.status === "PendingApproval" && approval?.status === "review-required",
    approvalLabel: approvalLabel(portal.pendingPublication?.status, approval?.status),
    approvalReason: approval?.message ?? "",
    canPublish: portal.pendingPublication?.status === "Approved",
    canCreateWorkingCopy: portal.workingCopy === undefined && portal.pendingPublication === undefined && published !== undefined,
    auditAvailable: active.publication !== undefined,
    historyAvailable: versionControlAvailable && capabilities.has("history"),
    diffAvailable: versionControlAvailable && capabilities.has("diff"),
    restoreAvailable: versionControlAvailable && capabilities.has("restore") && portal.workingCopy !== undefined,
    versionControlAvailability: portal.versionControl.availability,
    updatedAt: portal.updatedAt,
  }];
}

function approvalLabel(status: PortalRevisionStatus | undefined, decision: "allowed" | "review-required" | "denied" | undefined): string {
  if (status !== "PendingApproval") return "-";
  if (decision === "allowed") return "策略允许";
  if (decision === "review-required") return "需单人复验";
  return "需其他审批人";
}

function activeConfiguration(portal: Portal): { configuration: PortalConfiguration; status: PortalRevisionStatus; publication?: PortalPublication } | undefined {
  if (portal.workingCopy !== undefined) return { configuration: portal.workingCopy.configuration, status: "Draft" };
  if (portal.pendingPublication?.source.configuration !== undefined) {
    return { configuration: portal.pendingPublication.source.configuration, status: portal.pendingPublication.status, publication: portal.pendingPublication };
  }
  if (portal.publishedPublication?.source.configuration !== undefined) {
    return { configuration: portal.publishedPublication.source.configuration, status: portal.publishedPublication.status, publication: portal.publishedPublication };
  }
  return undefined;
}
