package kernelops

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/kernels/backend/nodeagent"
)

func TestPrintVersionJSON(t *testing.T) {
	var output bytes.Buffer
	if err := PrintVersion(&output, "1.0.0", []string{"--json"}); err != nil {
		t.Fatal(err)
	}
	var info VersionInfo
	if err := json.Unmarshal(output.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Kernel != "backend" || info.Version != "1.0.0" || info.GoVersion == "" {
		t.Fatalf("版本信息不完整: %+v", info)
	}
}

func TestRunValidateMigratesActualStateV1WithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actual-state-v1.json")
	raw := []byte(`{
  "version": 1,
  "node_id": "node-a",
  "observed_revision": 7,
  "observed_digest": "digest",
  "applied_revision": 6,
  "units": {
    "api": {"fingerprint":"fp","applied_revision":6,"status":"running","plugins":[],"restart_count":0}
  },
  "updated_at": "2026-07-16T00:00:00Z"
}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RunValidate(&output, []string{"-kind", ConfigKindActualState, "-file", path}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"schema_version": 2`) || !strings.Contains(output.String(), `"revision": 6`) {
		t.Fatalf("未报告迁移后的实际态: %s", output.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, after) {
		t.Fatal("预检不应写回实际态文件")
	}
}

func TestRunValidateRejectsMissingActualState(t *testing.T) {
	var output bytes.Buffer
	err := RunValidate(&output, []string{
		"-kind", ConfigKindActualState, "-file", filepath.Join(t.TempDir(), "missing.json"),
	})
	if err == nil {
		t.Fatal("实际态路径拼错时不能把空状态误报为有效")
	}
}

func TestRunSupportBundleRedactsSensitiveData(t *testing.T) {
	directory := t.TempDir()
	actualPath := filepath.Join(directory, "actual-state.json")
	now := time.Now().UTC().Truncate(time.Second)
	state := nodeagent.ActualState{
		Version: 2, NodeID: "node-a", ObservedRevision: 8, AppliedRevision: 7,
		Units: map[string]nodeagent.UnitState{
			"api": {
				AppliedRevision: 7, Phase: nodeagent.PhaseActive, PhaseChangedAt: now,
				Plugins: []nodeagent.InstalledPlugin{{
					ID: "com.example.api", Version: "1.0.0", Channel: "stable",
					SHA256: "abc", Root: "/secret/runtime", EntryPath: "/secret/runtime/backend",
				}},
				PIDs: []int{42}, LastError: "Bearer super-secret-token",
			},
		},
		Errors:    []nodeagent.OperationError{{UnitID: "api", Stage: "launch", Message: "password=hunter2"}},
		UpdatedAt: now,
	}
	if err := (nodeagent.FileStateStore{Path: actualPath}).Save(state); err != nil {
		t.Fatal(err)
	}
	diagnosticsPath := filepath.Join(directory, "diagnostics.json")
	if err := os.WriteFile(diagnosticsPath, []byte(`{"healthy":true,"counter":9007199254740993,"token":"abc","nested":{"config":{"password":"hunter2"}},"payload":"private"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	binaryPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(directory, "support", "support.tar.gz")
	var output bytes.Buffer
	if err := RunSupportBundle(&output, "1.0.0", []string{
		"-actual-state", actualPath, "-diagnostics", diagnosticsPath,
		"-binary", binaryPath, "-output", bundlePath,
	}); err != nil {
		t.Fatal(err)
	}
	contents := readBundle(t, bundlePath)
	joined := string(contents["actual-state-summary.json"]) + string(contents["kernel-diagnostics.json"])
	for _, forbidden := range []string{"super-secret-token", "hunter2", "/secret/runtime", `"token":"abc"`, `"payload":"private"`} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("支持包泄露敏感内容 %q: %s", forbidden, joined)
		}
	}
	if !strings.Contains(string(contents["kernel-diagnostics.json"]), "[REDACTED]") {
		t.Fatalf("诊断字段未脱敏: %s", contents["kernel-diagnostics.json"])
	}
	if !strings.Contains(string(contents["kernel-diagnostics.json"]), "9007199254740993") {
		t.Fatalf("诊断大整数在脱敏重编码时被修改: %s", contents["kernel-diagnostics.json"])
	}
	if len(contents["manifest.json"]) == 0 || len(contents["actual-state-summary.json"]) == 0 {
		t.Fatalf("支持包内容不完整: %v", contents)
	}
	info, err := os.Stat(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("支持包权限过宽: %o", info.Mode().Perm())
	}
	directoryInfo, err := os.Stat(filepath.Dir(bundlePath))
	if err != nil {
		t.Fatal(err)
	}
	if directoryInfo.Mode().Perm() != 0o700 {
		t.Fatalf("支持包目录权限过宽: %o", directoryInfo.Mode().Perm())
	}
	if err := RunSupportBundle(io.Discard, "1.0.0", []string{
		"-actual-state", actualPath, "-binary", binaryPath, "-output", bundlePath,
	}); err == nil {
		t.Fatal("支持包不应覆盖已有文件")
	}
}

func readBundle(t *testing.T, path string) map[string][]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	result := map[string][]byte{}
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tarReader)
		if err != nil {
			t.Fatal(err)
		}
		result[header.Name] = data
	}
	return result
}
