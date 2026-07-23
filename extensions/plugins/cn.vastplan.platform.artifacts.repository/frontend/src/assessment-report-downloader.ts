import type { ArtifactRef, PlatformAdminClient } from "@vastplan/platform-admin";

const repositoryPluginID = "cn.vastplan.platform.artifacts.repository";
const reportPermission = "platform.artifacts.assessment.report.read";

export type AssessmentReportKind = "vulnerability" | "license";

export class AssessmentReportDownloader {
  public constructor(
    private readonly repository: PlatformAdminClient,
    private readonly exposures: readonly PlatformAdminClient[],
    private readonly navigate: (url: string) => void = (url) => globalThis.location.assign(url),
  ) {}

  public async download(ref: ArtifactRef, kind: AssessmentReportKind): Promise<void> {
    const evidence = await this.repository.artifactSupplyChainEvidence(ref);
    const digest = kind === "vulnerability"
      ? evidence.securityStatus?.vulnerabilityReportSha256 ?? evidence.securityAdmission?.vulnerabilityReportSha256
      : evidence.securityStatus?.licenseReportSha256 ?? evidence.securityAdmission?.licenseReportSha256;
    if (digest === undefined) throw new Error("该评估记录没有可归档的原始报告");
    const report = await this.repository.prepareArtifactAssessmentReport(digest);
    const targetExposure = await this.resolveExposure();
    const grant = await targetExposure.client.issueArtifactAssessmentReportTicket(targetExposure.routeKey, digest);
    const target = new URL(report.resource, grant.endpoint);
    target.searchParams.set("vp_ticket", grant.ticket);
    this.navigate(target.toString());
  }

  private async resolveExposure(): Promise<{ client: PlatformAdminClient; routeKey: string }> {
    for (const client of this.exposures) {
      const revisions = await client.listDataPlaneExposures();
      const candidates = revisions.filter((item) => item.status === "Published"
        && item.exposure.service.pluginId === repositoryPluginID
        && item.exposure.dataPlaneServiceId === "assessment-reports"
        && item.exposure.allowedModes.includes("ticket-redirect")
        && item.exposure.requiredPermissions.includes(reportPermission));
      candidates.sort((left, right) => right.id - left.id);
      if (candidates[0] !== undefined) return { client, routeKey: candidates[0].exposure.routeKey };
    }
    throw new Error("未发布允许评估报告下载的 Data Plane Exposure");
  }
}
