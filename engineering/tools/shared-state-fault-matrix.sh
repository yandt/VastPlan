#!/usr/bin/env bash
# A3 有界 Shared State / Vault 故障矩阵；不执行 soak，也不访问已有外部环境。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

started_at="$(date +%s)"

go test -count=1 -timeout=90s -tags=e2e \
  -run '^TestSharedStateThreeNodeFaultMatrix$' \
  -v ./engineering/e2e

go test -count=1 -timeout=60s \
  -run '^Test(VaultTransitFaultMatrixFailsClosedAndRecovers|MaterialLeaseVaultOutageDeniesThenRecovers|MaterialLeaseReloadsRootAfterDecrypt)$' \
  -v ./extensions/plugins/cn.vastplan.platform.security.credentials/credentials

elapsed="$(( $(date +%s) - started_at ))"
echo "A3 有界故障矩阵通过：三节点 JetStream 仲裁、CAS fencing、重连和 Vault fail-closed/recovery（${elapsed}s）"
