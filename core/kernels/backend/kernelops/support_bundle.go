package kernelops

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
)

const (
	maxActualStateBytes = 16 << 20
	maxDiagnosticsBytes = 4 << 20
)

type supportBundleManifest struct {
	SchemaVersion int               `json:"schema_version"`
	GeneratedAt   time.Time         `json:"generated_at"`
	Kernel        VersionInfo       `json:"kernel"`
	BinarySHA256  string            `json:"binary_sha256"`
	Files         map[string]string `json:"files_sha256"`
}

type actualStateSummary struct {
	SchemaVersion    int                     `json:"schema_version"`
	NodeID           string                  `json:"node_id"`
	ObservedRevision uint64                  `json:"observed_revision"`
	AppliedRevision  uint64                  `json:"applied_revision"`
	UpdatedAt        time.Time               `json:"updated_at"`
	Units            []unitSummary           `json:"units"`
	Errors           []operationErrorSummary `json:"errors,omitempty"`
}

type unitSummary struct {
	ID              string            `json:"id"`
	AppliedRevision uint64            `json:"applied_revision"`
	Phase           string            `json:"phase"`
	RestartCount    uint64            `json:"restart_count"`
	PIDs            []int             `json:"pids,omitempty"`
	HasLastError    bool              `json:"has_last_error"`
	Plugins         []pluginSummary   `json:"plugins"`
	Candidate       *candidateSummary `json:"candidate,omitempty"`
}

type candidateSummary struct {
	Phase        string          `json:"phase"`
	HasLastError bool            `json:"has_last_error"`
	Plugins      []pluginSummary `json:"plugins,omitempty"`
}

type pluginSummary struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Channel string `json:"channel"`
	SHA256  string `json:"sha256"`
}

type operationErrorSummary struct {
	UnitID string `json:"unit_id"`
	Stage  string `json:"stage"`
}

// RunSupportBundle 生成只含版本、摘要、脱敏实际态和可选实时诊断的离线支持包。
func RunSupportBundle(output io.Writer, version string, args []string) error {
	flags := flag.NewFlagSet("support-bundle", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	actualStatePath := flags.String("actual-state", "", "Node Agent 实际态 JSON")
	outputPath := flags.String("output", "", "输出 .tar.gz")
	diagnosticsPath := flags.String("diagnostics", "", "可选 kernel.diagnostics JSON")
	binaryPath := flags.String("binary", "", "Backend 二进制；默认当前进程")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *actualStatePath == "" || *outputPath == "" {
		return errors.New("用法: support-bundle -actual-state <实际态.json> -output <支持包.tar.gz> [-diagnostics <诊断.json>] [-binary <backend-kernel>]")
	}
	if *binaryPath == "" {
		path, err := os.Executable()
		if err != nil {
			return fmt.Errorf("定位 Backend 二进制: %w", err)
		}
		*binaryPath = path
	}
	if err := checkRegularFile(*actualStatePath, maxActualStateBytes); err != nil {
		return fmt.Errorf("实际态文件: %w", err)
	}
	if *diagnosticsPath != "" {
		if err := checkRegularFile(*diagnosticsPath, maxDiagnosticsBytes); err != nil {
			return fmt.Errorf("诊断文件: %w", err)
		}
	}

	state, err := (nodeagent.FileStateStore{Path: *actualStatePath}).Load()
	if err != nil {
		return err
	}
	files := map[string][]byte{}
	files["actual-state-summary.json"], err = marshalJSON(summarizeActualState(state))
	if err != nil {
		return err
	}
	if *diagnosticsPath != "" {
		files["kernel-diagnostics.json"], err = readAndRedactDiagnostics(*diagnosticsPath)
		if err != nil {
			return err
		}
	}
	binaryDigest, err := fileSHA256(*binaryPath)
	if err != nil {
		return fmt.Errorf("计算 Backend 二进制摘要: %w", err)
	}
	fileDigests := make(map[string]string, len(files))
	for name, data := range files {
		digest := sha256.Sum256(data)
		fileDigests[name] = hex.EncodeToString(digest[:])
	}
	generatedAt := time.Now().UTC()
	files["manifest.json"], err = marshalJSON(supportBundleManifest{
		SchemaVersion: 1, GeneratedAt: generatedAt, Kernel: versionInfo(version),
		BinarySHA256: binaryDigest, Files: fileDigests,
	})
	if err != nil {
		return err
	}
	if err := writeTarGzipAtomic(*outputPath, files, generatedAt); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "支持包已生成: %s（%d 个文件，未包含配置、凭证或错误正文）\n", *outputPath, len(files))
	return err
}

func checkRegularFile(path string, maxBytes int64) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("必须是普通文件")
	}
	if info.Size() > maxBytes {
		return fmt.Errorf("超过大小上限 %d bytes", maxBytes)
	}
	return nil
}

func summarizeActualState(state nodeagent.ActualState) actualStateSummary {
	result := actualStateSummary{
		SchemaVersion: state.Version, NodeID: state.NodeID,
		ObservedRevision: state.ObservedRevision, AppliedRevision: state.AppliedRevision,
		UpdatedAt: state.UpdatedAt,
	}
	ids := make([]string, 0, len(state.Units))
	for id := range state.Units {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		unit := state.Units[id]
		summary := unitSummary{
			ID: id, AppliedRevision: unit.AppliedRevision, Phase: string(unit.Phase),
			RestartCount: unit.RestartCount, PIDs: append([]int(nil), unit.PIDs...),
			HasLastError: unit.LastError != "", Plugins: summarizePlugins(unit.Plugins),
		}
		if unit.Candidate != nil {
			summary.Candidate = &candidateSummary{
				Phase: string(unit.Candidate.Phase), HasLastError: unit.Candidate.LastError != "",
				Plugins: summarizePlugins(unit.Candidate.Plugins),
			}
		}
		result.Units = append(result.Units, summary)
	}
	for _, operationError := range state.Errors {
		result.Errors = append(result.Errors, operationErrorSummary{UnitID: operationError.UnitID, Stage: operationError.Stage})
	}
	return result
}

func summarizePlugins(plugins []nodeagent.InstalledPlugin) []pluginSummary {
	result := make([]pluginSummary, 0, len(plugins))
	for _, plugin := range plugins {
		result = append(result, pluginSummary{
			ID: plugin.ID, Version: plugin.Version, Channel: plugin.Channel, SHA256: plugin.SHA256,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func readAndRedactDiagnostics(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("解析诊断 JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("诊断文件只能包含一个 JSON 值")
	}
	return marshalJSON(redactValue(value))
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			if sensitiveKey(key) {
				result[key] = "[REDACTED]"
			} else {
				result[key] = redactValue(child)
			}
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for i := range typed {
			result[i] = redactValue(typed[i])
		}
		return result
	default:
		return value
	}
}

func sensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", ""), "_", ""))
	for _, fragment := range []string{"token", "secret", "password", "credential", "authorization", "cookie", "payload", "metadata", "config"} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

func marshalJSON(value any) ([]byte, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		_ = file.Close() // 主读取错误优先；这里只做资源回收。
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func writeTarGzipAtomic(outputPath string, files map[string][]byte, modTime time.Time) (writeErr error) {
	directory := filepath.Dir(outputPath)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return fmt.Errorf("支持包输出已存在: %s", outputPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".support-bundle-*.tar.gz")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if writeErr != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	gzipWriter := gzip.NewWriter(temporary)
	gzipWriter.ModTime = modTime
	tarWriter := tar.NewWriter(gzipWriter)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := files[name]
		header := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: modTime}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if _, err := tarWriter.Write(data); err != nil {
			return err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	if err := gzipWriter.Close(); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	// Hard link 只会在目标不存在时原子发布完整文件，避免高权限诊断命令在
	// Lstat 与 Rename 之间被并发创建同名路径后覆盖其他数据。
	if err := os.Link(temporaryPath, outputPath); err != nil {
		return err
	}
	_ = os.Remove(temporaryPath)
	return nil
}
