import { describe, expect, it, vi } from "vitest";
import type { PlatformAdminClient } from "@vastplan/platform-admin";
import { AssessmentReportDownloader } from "./assessment-report-downloader.js";

describe("AssessmentReportDownloader", () => {
  it("preauthorizes a referenced digest before requesting a one-use ticket", async () => {
    const digest = "a".repeat(64);
    const repository = {
      artifactSupplyChainEvidence: vi.fn(async () => ({ securityStatus: { vulnerabilityReportSha256: digest } })),
      prepareArtifactAssessmentReport: vi.fn(async () => ({ sha256: digest, resource: `/v1/assessment-reports/${digest}` })),
    } as unknown as PlatformAdminClient;
    const exposure = {
      listDataPlaneExposures: vi.fn(async () => [{ id: 8, status: "Published", exposure: {
        routeKey: "a234567a234567a23456", service: { pluginId: "cn.vastplan.platform.artifacts.repository" }, dataPlaneServiceId: "assessment-reports",
        allowedModes: ["ticket-redirect"], requiredPermissions: ["platform.artifacts.assessment.report.read"],
      } }]),
      issueArtifactAssessmentReportTicket: vi.fn(async () => ({ endpoint: "https://repo.example/", leaseId: "lease", ticket: "b234567890123456789012345678901234567890123", expiresAt: new Date(Date.now() + 30_000).toISOString() })),
    } as unknown as PlatformAdminClient;
    const navigate = vi.fn();
    await new AssessmentReportDownloader(repository, [exposure], navigate).download({ pluginId: "cn.example.demo", version: "1.0.0", channel: "stable" }, "vulnerability");
    expect(repository.prepareArtifactAssessmentReport).toHaveBeenCalledWith(digest);
    expect(exposure.issueArtifactAssessmentReportTicket).toHaveBeenCalledWith("a234567a234567a23456", digest);
    expect(navigate).toHaveBeenCalledWith(`https://repo.example/v1/assessment-reports/${digest}?vp_ticket=b234567890123456789012345678901234567890123`);
  });
});
