package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

// stagePackage creates a temporary package only when build outputs or legal
// files must be injected. Destination paths always come from the validated manifest.
func stagePackage(source, backendBin, frontendBundle, frontendGraph, frontendGraphRoot, dynamicGoBin, dynamicGoFingerprint, licenseSource, noticeSource string) (string, func()) {
	return stagePackageWithGraphs(source, backendBin, frontendBundle, frontendGraph, "", frontendGraphRoot, dynamicGoBin, dynamicGoFingerprint, licenseSource, noticeSource)
}

func stagePackageWithGraphs(source, backendBin, frontendBundle, frontendGraph, frontendServerGraph, frontendGraphRoot, dynamicGoBin, dynamicGoFingerprint, licenseSource, noticeSource string) (string, func()) {
	return stagePackageWithBackendModuleAndGraphs(source, backendBin, "", frontendBundle, frontendGraph, frontendServerGraph, frontendGraphRoot, dynamicGoBin, dynamicGoFingerprint, licenseSource, noticeSource)
}

func stagePackageWithBackendModuleAndGraphs(source, backendBin, backendModule, frontendBundle, frontendGraph, frontendServerGraph, frontendGraphRoot, dynamicGoBin, dynamicGoFingerprint, licenseSource, noticeSource string) (string, func()) {
	manifestRaw, err := os.ReadFile(filepath.Join(source, "vastplan.plugin.json"))
	if err != nil {
		fatalf("读取插件清单失败: %v", err)
	}
	manifest, err := pluginv1.ParseManifest(manifestRaw)
	if err != nil {
		fatalf("插件清单无效: %v", err)
	}
	licensePresent := declaredFilePresent(source, manifest.LicenseFile, manifest.License != "", "许可证")
	noticePresent := declaredFilePresent(source, manifest.NoticeFile, manifest.NoticeFile != "", "归属告示")
	validateFrontendInputs(frontendBundle, frontendGraph, frontendServerGraph, frontendGraphRoot)
	if backendBin == "" && backendModule == "" && frontendBundle == "" && frontendGraph == "" && frontendServerGraph == "" && dynamicGoBin == "" && licensePresent && noticePresent {
		return source, func() {}
	}

	manifestChanged := false
	dynamicGoEntry := validateDynamicGoInput(&manifest, dynamicGoBin, dynamicGoFingerprint)
	manifestChanged = dynamicGoEntry != ""
	backendEntry := validateBackendInput(manifest, backendBin)
	backendModuleEntry := validateBackendModuleInput(manifest, backendModule)
	frontendEntry := validateLegacyFrontendInput(manifest, frontendBundle)
	var frontendGraphContract *verifiedFrontendGraph
	if frontendGraph != "" {
		frontendGraphContract = loadVerifiedFrontendGraphs(frontendGraph, frontendServerGraph, frontendGraphRoot, manifest)
		manifest.FrontendModuleGraphs = &pluginv1.FrontendModuleGraphs{Browser: &frontendGraphContract.Browser, Server: frontendGraphContract.Server}
		manifestChanged = true
	}

	staging, err := os.MkdirTemp("", "vastplan-package-*")
	if err != nil {
		fatalf("创建打包临时目录失败: %v", err)
	}
	cleanup := func() { _ = os.RemoveAll(staging) }
	if err := copyTree(source, staging); err != nil {
		cleanup()
		fatalf("复制插件目录失败: %v", err)
	}
	copyBuildInput(staging, backendEntry, backendBin, 0o755, "backend 入口")
	copyBuildInput(staging, backendModuleEntry, backendModule, 0o644, "node-worker backend bundle")
	copyBuildInput(staging, frontendEntry, frontendBundle, 0o644, "frontend bundle")
	copyBuildInput(staging, dynamicGoEntry, dynamicGoBin, 0o644, "dynamic-go 模块")
	if frontendGraphContract != nil {
		if err := frontendGraphContract.CopyTo(staging); err != nil {
			cleanup()
			fatalf("写入 frontend Module Graph 节点失败: %v", err)
		}
	}
	injectDeclaredFile(staging, manifest.LicenseFile, licensePresent, licenseSource, "许可证")
	injectDeclaredFile(staging, manifest.NoticeFile, noticePresent, noticeSource, "归属告示")
	if manifestChanged {
		writeStagedManifest(staging, manifest, cleanup)
	}
	return staging, cleanup
}

func validateBackendModuleInput(manifest pluginv1.Manifest, filename string) string {
	if filename == "" {
		return ""
	}
	if manifest.Execution == nil || manifest.Execution.Backend == nil || manifest.Execution.Backend.Driver != "node-worker" {
		fatalf("-backend-module 只允许 node-worker 插件")
	}
	entry := manifest.Entry["backend"]
	if entry == "" || (!strings.HasSuffix(entry, ".js") && !strings.HasSuffix(entry, ".mjs")) {
		fatalf("node-worker 清单未声明 JavaScript entry.backend")
	}
	validateRegularInput(filename, false, "node-worker backend bundle")
	return entry
}

func validateFrontendInputs(bundle, graph, serverGraph, root string) {
	if bundle != "" && graph != "" {
		fatalf("-frontend-bundle 与 -frontend-graph 不能同时使用")
	}
	if serverGraph != "" && graph == "" {
		fatalf("-frontend-server-graph 必须与 -frontend-graph 同时配置")
	}
	if (graph == "") != (root == "") {
		fatalf("-frontend-graph 与 -frontend-graph-root 必须同时配置")
	}
}

func validateDynamicGoInput(manifest *pluginv1.Manifest, filename, fingerprint string) string {
	if filename == "" {
		return ""
	}
	if manifest.Execution == nil || manifest.Execution.Backend == nil || manifest.Execution.Backend.DynamicGo == nil {
		fatalf("清单未声明 execution.backend.dynamicGo")
	}
	fingerprint = strings.TrimSpace(fingerprint)
	decoded, err := hex.DecodeString(fingerprint)
	if err != nil || len(decoded) != sha256.Size || fingerprint != strings.ToLower(fingerprint) {
		fatalf("-dynamic-go-fingerprint 必须是 64 位小写 SHA-256 十六进制值")
	}
	validateRegularInput(filename, false, "dynamic-go 模块")
	manifest.Execution.Backend.DynamicGo.Fingerprint = fingerprint
	return manifest.Execution.Backend.DynamicGo.Entry
}

func validateBackendInput(manifest pluginv1.Manifest, filename string) string {
	if filename == "" {
		return ""
	}
	entry := manifest.Entry["backend"]
	if entry == "" {
		fatalf("清单未声明 entry.backend")
	}
	validateRegularInput(filename, true, "backend 二进制")
	return entry
}

func validateLegacyFrontendInput(manifest pluginv1.Manifest, filename string) string {
	if filename == "" {
		return ""
	}
	entry := manifest.Entry["frontend"]
	if entry == "" || (!strings.HasSuffix(entry, ".js") && !strings.HasSuffix(entry, ".mjs")) {
		fatalf("清单未声明已构建的 JavaScript entry.frontend")
	}
	validateRegularInput(filename, false, "frontend bundle")
	return entry
}

func validateRegularInput(filename string, executable bool, label string) {
	info, err := os.Stat(filename)
	if err != nil {
		fatalf("读取%s失败: %v", label, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || (executable && info.Mode().Perm()&0o111 == 0) {
		fatalf("%s不是符合要求的非空普通文件: %s", label, filename)
	}
}

func copyBuildInput(staging, entry, source string, mode os.FileMode, label string) {
	if source == "" {
		return
	}
	target := filepath.Join(staging, filepath.FromSlash(entry))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		fatalf("创建%s目录失败: %v", label, err)
	}
	if err := copyFile(source, target, mode); err != nil {
		fatalf("写入%s失败: %v", label, err)
	}
}

func declaredFilePresent(source, name string, declared bool, label string) bool {
	if !declared {
		return true
	}
	present, err := regularNonempty(filepath.Join(source, filepath.FromSlash(name)))
	if err != nil {
		fatalf("读取插件%s文件失败: %v", label, err)
	}
	return present
}

func injectDeclaredFile(staging, destination string, present bool, source, label string) {
	if present {
		return
	}
	if source == "" {
		fatalf("清单声明了%s，但未提供来源文件", label)
	}
	target := filepath.Join(staging, filepath.FromSlash(destination))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		fatalf("创建%s文件目录失败: %v", label, err)
	}
	if err := copyFile(source, target, 0o644); err != nil {
		fatalf("注入%s文件失败: %v", label, err)
	}
}

func writeStagedManifest(staging string, manifest pluginv1.Manifest, cleanup func()) {
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		cleanup()
		fatalf("编码签名清单失败: %v", err)
	}
	raw = append(raw, '\n')
	if _, err := pluginv1.ParseManifest(raw); err != nil {
		cleanup()
		fatalf("注入构建事实后的签名清单无效: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staging, "vastplan.plugin.json"), raw, 0o644); err != nil {
		cleanup()
		fatalf("写入签名清单失败: %v", err)
	}
}

func regularNonempty(filename string) (bool, error) {
	info, err := os.Stat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.Mode().IsRegular() && info.Size() > 0, nil
}

func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, filename)
		if err != nil || rel == "." {
			return err
		}
		if entry.IsDir() && (entry.Name() == "__pycache__" || entry.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".pyc") || strings.HasSuffix(entry.Name(), ".pyo")) {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("不允许符号链接: %s", rel)
		}
		destination := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("只允许普通文件: %s", rel)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyFile(filename, destination, info.Mode().Perm())
	})
}

func copyFile(source, target string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	return errors.Join(copyErr, out.Close())
}
