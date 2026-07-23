#!/usr/bin/env bash
set -euo pipefail

# Bounded plugin-artifact security fault matrix. It uses temporary test data,
# does not contact a production repository and is intentionally not a soak.
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
export GOCACHE=${GOCACHE:-/tmp/vastplan-go-cache}
cd "$ROOT"

go test ./core/shared/go/artifactassessment -run 'Test(AdmissionAndAppendOnlyStatus|FailedRescan|FreshRescan|PolicyRejects)'
go test ./core/shared/go/artifactreport -run 'TestArchive'
go test ./core/kernels/backend/pluginservice -run 'TestSignedRepositoryRequiresAndReverifiesSecurityAdmission'
go test ./core/kernels/backend/nodeagent -run 'TestSecurityWatermarkRejectsRepositoryRollback'
go test ./extensions/plugins/cn.vastplan.platform.artifacts.assessment.database.file/snapshot -run 'TestMaterializer'
go test ./extensions/plugins/cn.vastplan.platform.artifacts.assessment.provider/assessmentprovider -run 'Test(ConfigRequiresSnapshotPathBoundToDatabaseRevision|ServiceUsesScanLeaseAndMaterialLeaseWithoutReturningSecrets)'
go test ./extensions/plugins/cn.vastplan.platform.artifacts.assessment.controller/controller -run 'Test(Reconcile|ProviderDatabaseRevisionChange|ScheduleJitter|RetryBackoff)'
go test ./extensions/plugins/cn.vastplan.platform.artifacts.repository/backend -run 'Test(AssessmentLease|AppendAssessmentStatus|DataPlaneTicket|AssessmentReportTicket)'
go test ./extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog -run 'Test(SecurityStatusHTTP|StablePublicationBindsTestingSupplyChainSidecars|OfflineBundle)'
go test ./extensions/plugins/cn.vastplan.platform.artifacts.repository/repositoryruntime -run 'Test(SecurityAssessmentStatsRemainLowCardinality|RepositoryRequiresArchivedReports|AssessmentInventory)'
go test -tags=e2e ./engineering/e2e -run 'TestArtifactAssessment(DatabaseFileRealProcessMaterializesBeforeReady|ControllerRealProcessRunsAutonomousWorkflow)'
pnpm --filter @vastplan/platform-admin typecheck
pnpm --filter @vastplan/portal-host test -- platform-artifact-routes.test.ts
pnpm --filter @vastplan/artifact-repository-ui typecheck
pnpm --filter @vastplan/artifact-repository-ui test

echo "插件制品安全有界故障矩阵通过：不可变数据库快照、报告归档/独立 Ticket、准入、复扫、失败恢复、token 分权、离线证据、Node 防回滚与 Portal 状态（非 soak）"
