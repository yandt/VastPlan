import type { BackendResolutionReport, ResolutionStatus } from "@vastplan/composition-planning";
import type { ServiceRevision } from "@vastplan/platform-admin";
import type { JSONValue, WorkbenchOverlayContent } from "@vastplan/workbench-sdk";
import { message } from "./localization.js";

export type RevisionKind = "Intent" | "Legacy";
export type PlanStatus = ResolutionStatus | "Legacy";

export type DeploymentRow = ServiceRevision & Record<string, unknown> & {
  revisionKind: RevisionKind;
  planStatus: PlanStatus;
  planningStale: boolean;
};

export function deploymentRow(revision: ServiceRevision): DeploymentRow {
  return {
    ...revision,
    revisionKind: revision.intent === undefined ? "Legacy" : "Intent",
    planStatus: revision.resolutionReport?.status ?? "Legacy",
    planningStale: revision.planningStale === true,
  };
}

export function resolutionContent(revision: ServiceRevision | undefined): WorkbenchOverlayContent {
  const report = revision?.resolutionReport;
  if (report === undefined) return { kind: "json", documents: [{ value: {} }] };
  return {
    kind: "json",
    documents: [
      { title: message("document.summary", "计划摘要"), value: summary(report) as JSONValue },
      { title: message("document.features", "已启用 Feature"), value: report.features as unknown as JSONValue },
      { title: message("document.providers", "Provider Binding"), value: report.providerBindings as unknown as JSONValue },
      { title: message("document.configuration", "配置计划"), value: report.configurationPlan as unknown as JSONValue },
      { title: message("document.diagnostics", "诊断"), value: report.diagnostics as unknown as JSONValue },
      { title: message("document.artifacts", "精确制品锁"), value: (report.artifactLock ?? {}) as unknown as JSONValue },
    ],
  };
}

export function dependencyGraphContent(revision: ServiceRevision | undefined): WorkbenchOverlayContent {
  const graph = revision?.resolutionReport?.serviceGraph;
  const rows = (graph?.nodes ?? []).map((node) => {
    const edges = (graph?.edges ?? []).filter((edge) => edge.fromUnitId === node.unitId);
    return {
      unitId: node.unitId,
      serviceClass: node.serviceClass,
      dependencies: edges.map((edge) => `${edge.toUnitId} (${edge.kind}/${edge.failurePolicy})`).join(", "),
      capabilities: edges.map((edge) => edge.capability).join(", "),
    };
  });
  return {
    kind: "table", rowKey: "unitId", rows,
    columns: [
      { key: "unitId", label: message("column.unit", "服务单元"), defaultVisible: true, minWidth: 140 },
      { key: "serviceClass", label: message("column.serviceClass", "服务分类"), defaultVisible: true, minWidth: 180 },
      { key: "dependencies", label: message("column.dependencies", "依赖服务"), defaultVisible: true, minWidth: 240 },
      { key: "capabilities", label: message("column.capabilities", "依赖能力"), defaultVisible: true, minWidth: 240 },
    ],
  };
}

function summary(report: BackendResolutionReport): Record<string, unknown> {
  return {
    status: report.status,
    planDigest: report.planDigest,
    intent: report.intent,
    platformProfile: report.platformProfile,
    planner: report.planner,
    applicationCompositionDigest: report.applicationCompositionDigest,
    artifactLockDigest: report.artifactLock?.digest,
    configurationPlanDigest: report.configurationPlan.digest,
  };
}
