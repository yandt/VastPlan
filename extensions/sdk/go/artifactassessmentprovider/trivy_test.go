package artifactassessmentprovider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTrivyDatabaseRevisionBindsMetadataAndDatabaseBytes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	if err := os.MkdirAll(filepath.Join(root, "db"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "db", "metadata.json"), []byte(`{"Version":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "db", "trivy.db"), []byte("database-a"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := TrivyDatabaseRevision(root)
	if err != nil || len(first) != 64 {
		t.Fatalf("数据库 revision 无效: %q %v", first, err)
	}
	if err := os.WriteFile(filepath.Join(root, "db", "trivy.db"), []byte("database-b"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := TrivyDatabaseRevision(root)
	if err != nil || second == first {
		t.Fatal("数据库字节变化必须改变 revision")
	}
}

func TestTrivyScanUsesPinnedOfflineArgumentsAndBoundDatabase(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	if err := os.MkdirAll(filepath.Join(cache, "db"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"metadata.json": `{"Version":2}`, "trivy.db": "database"} {
		if err := os.WriteFile(filepath.Join(cache, "db", name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	revision, err := TrivyDatabaseRevision(cache)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "trivy")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "Version: 0.72.0"
  exit 0
fi
seen_offline=false
seen_skip=false
while [ "$#" -gt 0 ]; do
  [ "$1" = "--offline-scan" ] && seen_offline=true
  [ "$1" = "--skip-db-update" ] && seen_skip=true
  if [ "$1" = "--output" ]; then shift; output="$1"; fi
  shift
done
[ "$seen_offline" = true ] && [ "$seen_skip" = true ] || exit 9
printf '%s' '{"SchemaVersion":2,"Results":[{"Target":"go.mod","Packages":[{"Name":"demo"}],"Vulnerabilities":[],"Licenses":[{"Name":"MIT","PkgName":"demo","Severity":"LOW"}]}]}' > "$output"
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "work", "package")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	engine, err := NewTrivy(TrivyConfig{Binary: binary, CacheDirectory: cache, ScannerVersion: "0.72.0", DatabaseRevision: revision, Timeout: time.Minute, AllowedLicenses: []string{"MIT"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Scan(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanner.DatabaseRevision != revision || len(result.Licenses) != 1 || result.Licenses[0].Disposition != LicenseAllowed {
		t.Fatalf("真实命令适配结果无效: %+v", result)
	}
}

func TestTrivyNormalizeDeduplicatesAndAppliesLicenseAllowlist(t *testing.T) {
	engine, err := NewTrivy(TrivyConfig{
		Binary: "/usr/local/bin/trivy", CacheDirectory: "/var/lib/vastplan/trivy", ScannerVersion: "0.72.0",
		DatabaseRevision: strings.Repeat("a", 64), Timeout: time.Minute, AllowedLicenses: []string{"Apache-2.0", "MIT"},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"SchemaVersion":2,"Results":[{"Target":"go.mod","Packages":[{"Name":"demo"}],"Vulnerabilities":[{"VulnerabilityID":"CVE-1","PkgName":"demo","InstalledVersion":"1.0.0","Severity":"HIGH"},{"VulnerabilityID":"CVE-1","PkgName":"demo","InstalledVersion":"1.0.0","Severity":"HIGH"},{"VulnerabilityID":"CVE-2","PkgName":"other","InstalledVersion":"2.0.0","Severity":""}],"Licenses":[{"Name":"MIT","PkgName":"demo","Severity":"LOW"},{"Name":"AGPL-3.0","PkgName":"other","Severity":"CRITICAL"},{"Name":"UNKNOWN","PkgName":"mystery","Severity":"UNKNOWN"}]}]}`)
	result, err := engine.normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Vulnerabilities) != 2 || result.Vulnerabilities[0].Severity != SeverityHigh || result.Vulnerabilities[1].Severity != SeverityUnknown {
		t.Fatalf("漏洞规范化不正确: %+v", result.Vulnerabilities)
	}
	if len(result.Licenses) != 3 || result.Licenses[0].Disposition != LicenseAllowed || result.Licenses[1].Disposition != LicenseDenied || result.Licenses[2].Disposition != LicenseUnknown {
		t.Fatalf("许可证规范化不正确: %+v", result.Licenses)
	}
}

func TestTrivyRejectsUnknownReportSchema(t *testing.T) {
	engine, _ := NewTrivy(TrivyConfig{Binary: "/trivy", CacheDirectory: "/cache", ScannerVersion: "1", DatabaseRevision: strings.Repeat("a", 64), Timeout: time.Minute})
	if _, err := engine.normalize([]byte(`{"SchemaVersion":99,"Results":[]}`)); err == nil {
		t.Fatal("未知 Trivy schema 必须 fail-closed")
	}
}

func TestTrivyRejectsReportWithoutPackagesOrLicenses(t *testing.T) {
	engine, _ := NewTrivy(TrivyConfig{Binary: "/trivy", CacheDirectory: "/cache", ScannerVersion: "1", DatabaseRevision: strings.Repeat("a", 64), Timeout: time.Minute})
	if _, err := engine.normalize([]byte(`{"SchemaVersion":2,"Results":[]}`)); err == nil {
		t.Fatal("没有识别到包或许可证的扫描必须 fail-closed")
	}
}
