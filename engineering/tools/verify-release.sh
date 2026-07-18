#!/usr/bin/env bash
# 发布工程自检：可复现构建、内置运维入口和确定性 SBOM 必须一起通过。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
cd "$ROOT"

./engineering/tools/verify-reproducible-build.sh
host_goos="$(go env GOHOSTOS)"
host_goarch="$(go env GOHOSTARCH)"
GOOS="$host_goos" GOARCH="$host_goarch" OUT_DIR="$TMP/bin" ./engineering/tools/build.sh >/dev/null
backend="$TMP/bin/backend-kernel"
version="$(tr -d '[:space:]' < core/kernels/backend/VERSION)"

"$backend" version --json > "$TMP/version.json"
grep -Fq '"kernel":"backend"' "$TMP/version.json"
grep -Fq "\"version\":\"${version}\"" "$TMP/version.json"
"$backend" validate -kind desired-v1 -file engineering/deploy/local.desired-state.json >/dev/null
"$backend" validate -kind platform-profile-v1 -file engineering/deploy/platform-profile.json >/dev/null
"$backend" validate -kind application-composition-v1 -file engineering/deploy/application-composition.json >/dev/null
"$backend" validate -kind portal-platform-profile-v1 -file engineering/deploy/portal-platform-profile.json >/dev/null
"$backend" validate -kind portal-application-composition-v1 -file engineering/deploy/portal-application-composition.json >/dev/null
"$backend" validate -kind deployment-v2 -file engineering/deploy/cluster.deployment.json >/dev/null
"$backend" controlplane -help >/dev/null 2>&1
"$backend" artifact-server -help >/dev/null 2>&1

go run ./engineering/tools/sbom -binary "$backend" -output "$TMP/first.cdx.json" -version "$version"
go run ./engineering/tools/sbom -binary "$backend" -output "$TMP/second.cdx.json" -version "$version"
cmp "$TMP/first.cdx.json" "$TMP/second.cdx.json"
grep -Fq '"bomFormat": "CycloneDX"' "$TMP/first.cdx.json"
grep -Fq '"specVersion": "1.5"' "$TMP/first.cdx.json"

echo "发布工程自检通过：可复现构建、内置预检与确定性 CycloneDX SBOM"
