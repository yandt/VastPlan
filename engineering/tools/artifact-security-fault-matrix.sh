#!/usr/bin/env bash
set -euo pipefail

# Bounded plugin-artifact security fault matrix. It uses temporary test data,
# does not contact a production repository and is intentionally not a soak.
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
export GOCACHE=${GOCACHE:-/tmp/vastplan-go-cache}
cd "$ROOT"

go test ./core/shared/go/artifactassessment -run 'Test(AdmissionAndAppendOnlyStatus|FailedRescan|FreshRescan|PolicyRejects)'
go test ./core/kernels/backend/pluginservice -run 'TestSignedRepositoryRequiresAndReverifiesSecurityAdmission'
go test ./core/kernels/backend/nodeagent -run 'TestSecurityWatermarkRejectsRepositoryRollback'
go test ./extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog -run 'Test(SecurityStatusHTTP|StablePublicationBindsTestingSupplyChainSidecars|OfflineBundle)'
go test ./extensions/plugins/cn.vastplan.platform.artifacts.repository/repositoryruntime -run 'TestSecurityAssessmentStatsRemainLowCardinality'
pnpm --filter @vastplan/platform-admin typecheck
pnpm --filter @vastplan/artifact-repository-ui typecheck
pnpm --filter @vastplan/artifact-repository-ui test

echo "插件制品安全有界故障矩阵通过：准入、复扫、失败恢复、token 分权、离线证据、Node 防回滚与 Portal 状态（非 soak）"
