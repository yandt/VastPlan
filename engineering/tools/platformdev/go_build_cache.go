package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type goBinaryBuild struct {
	ID, Package, Version, Category, Digest string
}

type goBuildPlan struct {
	Kernel    goBinaryBuild
	Plugins   []goBinaryBuild
	Aggregate string
}

type listedGoPackage struct {
	Dir        string
	GoFiles    []string
	CgoFiles   []string
	CFiles     []string
	CXXFiles   []string
	MFiles     []string
	HFiles     []string
	FFiles     []string
	SFiles     []string
	SwigFiles  []string
	EmbedFiles []string
}

func (r *runtime) computeGoBuildPlan(ctx context.Context, goIdentity, goCache string) (goBuildPlan, error) {
	selection, err := r.seedSelection()
	if err != nil {
		return goBuildPlan{}, err
	}
	kernel, err := r.computeBackendKernelBuild(ctx, goIdentity, goCache)
	if err != nil {
		return goBuildPlan{}, err
	}
	plan := goBuildPlan{Kernel: kernel}
	specs, err := discoverPackageSpecs(r.options.root)
	if err != nil {
		return goBuildPlan{}, err
	}
	for _, spec := range specs {
		if !spec.backend || !selection.contains(spec.id) {
			continue
		}
		version, err := pluginManifestVersion(r.options.root, spec.id)
		if err != nil {
			return goBuildPlan{}, err
		}
		packagePath := "./" + filepath.ToSlash(filepath.Join("extensions", "plugins", spec.id, "backend"))
		digest, err := r.digestGoBinary(ctx, goCache, packagePath, []string{
			filepath.ToSlash(filepath.Join("extensions", "plugins", spec.id, "vastplan.plugin.json")), "go.mod", "go.sum",
		}, goIdentity, "backend-plugin-v2", spec.id, version)
		if err != nil {
			return goBuildPlan{}, fmt.Errorf("计算插件 %s 构建摘要: %w", spec.id, err)
		}
		plan.Plugins = append(plan.Plugins, goBinaryBuild{
			ID: spec.id, Package: packagePath, Version: version,
			Category: "backend-plugin-" + spec.id, Digest: digest,
		})
	}
	sort.Slice(plan.Plugins, func(i, j int) bool { return plan.Plugins[i].ID < plan.Plugins[j].ID })
	parts := []string{plan.Kernel.ID, plan.Kernel.Digest}
	for _, plugin := range plan.Plugins {
		parts = append(parts, plugin.ID, plugin.Digest)
	}
	plan.Aggregate = digestStrings(parts...)
	return plan, nil
}

func (r *runtime) computeBackendKernelBuild(ctx context.Context, goIdentity, goCache string) (goBinaryBuild, error) {
	kernelVersionRaw, err := os.ReadFile(filepath.Join(r.options.root, "core", "kernels", "backend", "VERSION"))
	if err != nil {
		return goBinaryBuild{}, err
	}
	kernelVersion := strings.TrimSpace(string(kernelVersionRaw))
	kernelDigest, err := r.digestGoBinary(ctx, goCache, "./core/kernels/backend", []string{
		"core/kernels/backend/VERSION", "go.mod", "go.sum",
	}, goIdentity, "backend-kernel-v2", kernelVersion)
	if err != nil {
		return goBinaryBuild{}, fmt.Errorf("计算 Backend Kernel 构建摘要: %w", err)
	}
	return goBinaryBuild{
		ID: "backend-kernel", Package: "./core/kernels/backend", Version: kernelVersion,
		Category: "backend-kernel", Digest: kernelDigest,
	}, nil
}

func (r *runtime) digestGoBinary(ctx context.Context, goCache, packagePath string, extraFiles []string, salts ...string) (string, error) {
	ordered, err := r.listGoBinaryInputs(ctx, goCache, packagePath, extraFiles)
	if err != nil {
		return "", err
	}
	return digestExplicitFiles(r.options.root, ordered, salts)
}

func (r *runtime) listGoBinaryInputs(ctx context.Context, goCache, packagePath string, extraFiles []string) ([]string, error) {
	command := exec.CommandContext(ctx, "go", "list", "-deps", "-json", packagePath)
	command.Dir = r.options.root
	command.Env = mergedEnv(map[string]string{"CGO_ENABLED": "1", "GOCACHE": goCache})
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("go list %s: %s", packagePath, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, err
	}
	files := map[string]struct{}{}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var pkg listedGoPackage
		if err := decoder.Decode(&pkg); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
		relativeDir, local := pathWithinRoot(r.options.root, pkg.Dir)
		if !local {
			continue
		}
		for _, name := range goPackageFiles(pkg) {
			files[filepath.ToSlash(filepath.Join(relativeDir, name))] = struct{}{}
		}
	}
	for _, relative := range extraFiles {
		files[filepath.ToSlash(relative)] = struct{}{}
	}
	ordered := make([]string, 0, len(files))
	for relative := range files {
		ordered = append(ordered, relative)
	}
	sort.Strings(ordered)
	return ordered, nil
}

func pathWithinRoot(root, path string) (string, bool) {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return relative, true
}

func goPackageFiles(pkg listedGoPackage) []string {
	files := make([]string, 0, len(pkg.GoFiles)+len(pkg.CgoFiles)+len(pkg.EmbedFiles))
	for _, group := range [][]string{pkg.GoFiles, pkg.CgoFiles, pkg.CFiles, pkg.CXXFiles, pkg.MFiles, pkg.HFiles, pkg.FFiles, pkg.SFiles, pkg.SwigFiles, pkg.EmbedFiles} {
		files = append(files, group...)
	}
	return files
}

func digestExplicitFiles(root string, paths, salts []string) (string, error) {
	hash := sha256.New()
	for _, salt := range salts {
		_, _ = io.WriteString(hash, "salt\x00"+salt+"\x00")
	}
	for _, relative := range paths {
		absolute := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Lstat(absolute)
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("Go 构建输入必须是普通文件: %s", relative)
		}
		content, err := os.ReadFile(absolute)
		if err != nil {
			return "", err
		}
		_, _ = io.WriteString(hash, relative+"\x00"+info.Mode().Perm().String()+"\x00")
		_, _ = hash.Write(content)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (r *runtime) prepareCachedGoBinaries(ctx context.Context, cacheRoot, goCache, goIdentity string) (string, error) {
	plan, err := r.computeGoBuildPlan(ctx, goIdentity, goCache)
	if err != nil {
		return "", err
	}
	targetBin := filepath.Join(r.runDir, "bin")
	if err := os.RemoveAll(targetBin); err != nil {
		return "", err
	}
	if err := os.MkdirAll(targetBin, 0o700); err != nil {
		return "", err
	}
	builds := append([]goBinaryBuild{plan.Kernel}, plan.Plugins...)
	for _, build := range builds {
		cached, err := r.ensureCachedGoBinary(ctx, cacheRoot, goCache, build)
		if err != nil {
			return "", err
		}
		logBuildCacheResult(build.ID, cached)
		if err := materializeCachedFile(filepath.Join(cached.Path, "bin", build.ID), filepath.Join(targetBin, build.ID)); err != nil {
			return "", fmt.Errorf("装配 Go 构建缓存 %s: %w", build.ID, err)
		}
	}
	return plan.Aggregate, nil
}

func (r *runtime) ensureCachedGoBinary(ctx context.Context, cacheRoot, goCache string, build goBinaryBuild) (cachedBuild, error) {
	return ensureCachedBuild(cacheRoot, build.Category, build.Digest, func(candidate string) error {
		binDir := filepath.Join(candidate, "bin")
		if err := os.MkdirAll(binDir, 0o700); err != nil {
			return err
		}
		versionSymbol := "main.pluginVersion"
		if build.ID == "backend-kernel" {
			versionSymbol = "main.version"
		}
		return r.command(ctx, map[string]string{"CGO_ENABLED": "1", "GOCACHE": goCache},
			"go", "build", "-trimpath", "-buildvcs=false",
			"-ldflags", "-s -w -buildid= -X "+versionSymbol+"="+build.Version,
			"-o", filepath.Join(binDir, build.ID), build.Package)
	}, func(candidate string) error {
		return requireCachedFiles(filepath.Join(candidate, "bin"), build.ID)
	})
}

// refreshDevelopmentBackendKernel keeps an immutable Seed plugin repository
// while pairing it with the current development orchestrator's Backend host.
// Production releases do this pairing in the signed release manifest instead.
func (r *runtime) refreshDevelopmentBackendKernel(ctx context.Context) (bool, error) {
	cacheRoot := filepath.Join(r.options.stateRoot, "build-cache")
	goCache := filepath.Join(r.options.stateRoot, "go-cache")
	if err := os.MkdirAll(goCache, 0o700); err != nil {
		return false, err
	}
	identity, err := developmentGoIdentity(ctx)
	if err != nil {
		return false, err
	}
	build, err := r.computeBackendKernelBuild(ctx, identity, goCache)
	if err != nil {
		return false, err
	}
	cached, err := r.ensureCachedGoBinary(ctx, cacheRoot, goCache, build)
	if err != nil {
		return false, err
	}
	logBuildCacheResult("Backend Kernel 宿主", cached)
	return replaceCachedFileIfChanged(
		filepath.Join(cached.Path, "bin", build.ID),
		filepath.Join(r.runDir, "dynamic", build.ID),
	)
}

// pairPreparedBackendKernel makes the runtime entry point an exact projection
// of the dependency-aware Backend Kernel build. The dynamic-go cache owns the
// Go ABI host and .so bundle; its convenience kernel copy must never override
// a newer kernel produced from the full Backend dependency closure.
func (r *runtime) pairPreparedBackendKernel() error {
	source := filepath.Join(r.runDir, "bin", "backend-kernel")
	target := filepath.Join(r.runDir, "dynamic", "backend-kernel")
	changed, err := replaceCachedFileIfChanged(source, target)
	if err != nil {
		return fmt.Errorf("配对 Backend Kernel 运行入口: %w", err)
	}
	equal, err := regularFilesEqual(source, target)
	if err != nil {
		return fmt.Errorf("复核 Backend Kernel 运行入口: %w", err)
	}
	if !equal {
		return errors.New("Backend Kernel 运行入口与依赖闭包构建不一致")
	}
	if changed {
		log.Printf("已用本次 Backend 依赖闭包构建刷新实际 Kernel 运行入口")
	}
	return nil
}

func replaceCachedFileIfChanged(source, target string) (bool, error) {
	equal, err := regularFilesEqual(source, target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if equal {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return false, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".host-candidate-")
	if err != nil {
		return false, err
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return false, err
	}
	if err := os.Remove(temporaryPath); err != nil {
		return false, err
	}
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := materializeCachedFile(source, temporaryPath); err != nil {
		return false, err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return false, err
	}
	return true, nil
}

func regularFilesEqual(left, right string) (bool, error) {
	leftRaw, err := os.ReadFile(left)
	if err != nil {
		return false, err
	}
	rightRaw, err := os.ReadFile(right)
	if err != nil {
		return false, err
	}
	return bytes.Equal(leftRaw, rightRaw), nil
}

func materializeCachedFile(source, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("缓存文件无效: %s", source)
	}
	if err := os.Link(source, target); err == nil {
		return nil
	}
	return copyBuildFile(source, target, info.Mode().Perm())
}
