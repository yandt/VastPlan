package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	stdruntime "runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

const developmentBuildCacheSchema = 1

type buildCacheMarker struct {
	Schema    int       `json:"schema"`
	Category  string    `json:"category"`
	Digest    string    `json:"digest"`
	CreatedAt time.Time `json:"createdAt"`
}

type cachedBuild struct {
	Path string
	Hit  bool
}

func ensureCachedBuild(cacheRoot, category, digest string, build func(string) error, validate func(string) error) (cachedBuild, error) {
	if category == "" || len(digest) != sha256.Size*2 {
		return cachedBuild{}, errors.New("构建缓存 category 或 digest 无效")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return cachedBuild{}, errors.New("构建缓存 digest 不是 SHA-256")
	}
	categoryRoot := filepath.Join(cacheRoot, category)
	target := filepath.Join(categoryRoot, digest)
	if validCachedBuild(target, category, digest, validate) {
		return cachedBuild{Path: target, Hit: true}, nil
	}
	if err := os.RemoveAll(target); err != nil {
		return cachedBuild{}, fmt.Errorf("清理无效构建缓存 %s: %w", category, err)
	}
	if err := os.MkdirAll(categoryRoot, 0o700); err != nil {
		return cachedBuild{}, err
	}
	temporary, err := os.MkdirTemp(categoryRoot, ".candidate-")
	if err != nil {
		return cachedBuild{}, err
	}
	defer func() { _ = os.RemoveAll(temporary) }()
	if err := build(temporary); err != nil {
		return cachedBuild{}, err
	}
	if err := validate(temporary); err != nil {
		return cachedBuild{}, fmt.Errorf("验证 %s 构建缓存候选: %w", category, err)
	}
	marker := buildCacheMarker{Schema: developmentBuildCacheSchema, Category: category, Digest: digest, CreatedAt: time.Now().UTC()}
	raw, err := json.Marshal(marker)
	if err != nil {
		return cachedBuild{}, err
	}
	if err := os.WriteFile(filepath.Join(temporary, ".complete.json"), append(raw, '\n'), 0o600); err != nil {
		return cachedBuild{}, err
	}
	if err := os.Rename(temporary, target); err != nil {
		// Another platformdev process may have completed the exact same digest
		// while this candidate was building. The immutable marker and validator
		// decide whether that race is a safe cache hit.
		if validCachedBuild(target, category, digest, validate) {
			return cachedBuild{Path: target, Hit: true}, nil
		}
		return cachedBuild{}, fmt.Errorf("提交 %s 构建缓存: %w", category, err)
	}
	return cachedBuild{Path: target}, nil
}

func validCachedBuild(path, category, digest string, validate func(string) error) bool {
	raw, err := os.ReadFile(filepath.Join(path, ".complete.json"))
	if err != nil {
		return false
	}
	var marker buildCacheMarker
	if json.Unmarshal(raw, &marker) != nil || marker.Schema != developmentBuildCacheSchema || marker.Category != category || marker.Digest != digest {
		return false
	}
	return validate(path) == nil
}

func materializeCachedDirectory(source, target string) error {
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." || relative == ".complete.json" {
			if relative == "." {
				return os.MkdirAll(target, 0o700)
			}
			return nil
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("构建缓存不允许符号链接: %s", relative)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("构建缓存只允许普通文件: %s", relative)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		if err := os.Link(path, destination); err == nil {
			return nil
		} else if !errors.Is(err, syscall.EXDEV) && !errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.ENOTSUP) {
			return err
		}
		return copyBuildFile(path, destination, info.Mode().Perm())
	})
}

func copyBuildFile(source, target string, mode fs.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	return errors.Join(copyErr, out.Close())
}

func digestBuildInputs(root string, relativePaths []string, salts []string, include func(string) bool) (string, error) {
	hash := sha256.New()
	for _, salt := range salts {
		_, _ = io.WriteString(hash, "salt\x00"+salt+"\x00")
	}
	paths := append([]string(nil), relativePaths...)
	sort.Strings(paths)
	for _, relativeRoot := range paths {
		absoluteRoot := filepath.Join(root, filepath.FromSlash(relativeRoot))
		if err := filepath.WalkDir(absoluteRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if entry.IsDir() {
				switch entry.Name() {
				case ".git", ".vastplan", "node_modules", "dist", "__pycache__", "graphify-out":
					if path != absoluteRoot {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				target, err := os.Readlink(path)
				if err != nil {
					return err
				}
				_, _ = io.WriteString(hash, relative+"\x00symlink\x00"+target+"\x00")
				return nil
			}
			if !entry.Type().IsRegular() || (include != nil && !include(relative)) {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			_, _ = io.WriteString(hash, relative+"\x00"+info.Mode().Perm().String()+"\x00")
			_, _ = hash.Write(content)
			_, _ = hash.Write([]byte{0})
			return nil
		}); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func commandIdentity(ctx context.Context, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("读取工具链身份 %s: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func developmentGoIdentity(ctx context.Context) (string, error) {
	version, err := commandIdentity(ctx, "go", "version")
	if err != nil {
		return "", err
	}
	return strings.Join([]string{version, stdruntime.GOOS, stdruntime.GOARCH, "CGO_ENABLED=1"}, "|"), nil
}

func developmentFrontendIdentity(ctx context.Context) (string, error) {
	node, err := commandIdentity(ctx, "node", "--version")
	if err != nil {
		return "", err
	}
	pnpm, err := commandIdentity(ctx, "pnpm", "--version")
	if err != nil {
		return "", err
	}
	return "node=" + node + "|pnpm=" + pnpm, nil
}

func requireCachedFiles(root string, files ...string) error {
	for _, relative := range files {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("缓存文件无效: %s", relative)
		}
	}
	return nil
}

func (r *runtime) prepareCachedBuilds(ctx context.Context) error {
	cacheRoot := filepath.Join(r.options.stateRoot, "build-cache")
	goCache := filepath.Join(r.options.stateRoot, "go-cache")
	if err := os.MkdirAll(goCache, 0o700); err != nil {
		return err
	}
	goIdentity, err := developmentGoIdentity(ctx)
	if err != nil {
		return err
	}
	frontendIdentity, err := developmentFrontendIdentity(ctx)
	if err != nil {
		return err
	}

	backendDigest, err := digestBuildInputs(r.options.root, []string{
		"go.mod", "go.sum", "contracts", "core", "extensions/plugins", "extensions/sdk/go", "engineering/tools/build.sh",
	}, []string{goIdentity, "backend-build-v1"}, backendBuildInput)
	if err != nil {
		return fmt.Errorf("计算 Backend 构建摘要: %w", err)
	}
	log.Printf("[2/6] 准备 Backend 内核与插件 digest=%s", backendDigest[:12])
	backend, err := ensureCachedBuild(cacheRoot, "backend", backendDigest, func(candidate string) error {
		return r.command(ctx, map[string]string{
			"CGO_ENABLED": "1", "OUT_DIR": filepath.Join(candidate, "bin"), "GOCACHE": goCache,
		}, "./engineering/tools/build.sh")
	}, func(candidate string) error {
		return r.validateBackendBuild(filepath.Join(candidate, "bin"))
	})
	if err != nil {
		return err
	}
	logBuildCacheResult("Backend 内核与插件", backend)
	if err := materializeCachedDirectory(filepath.Join(backend.Path, "bin"), filepath.Join(r.runDir, "bin")); err != nil {
		return fmt.Errorf("装配 Backend 构建缓存: %w", err)
	}

	hmr := frontendHMR{root: r.options.root}
	frontendSources, err := hmr.sourceSignatures()
	if err != nil {
		return err
	}
	frontendDigest := digestStrings(frontendIdentity, frontendSources.host, frontendSources.plugins, fmt.Sprintf("hot=%t", r.options.hot), "frontend-build-v2")
	log.Printf("[3/6] 准备按需加载的 Portal 与前端插件 digest=%s", frontendDigest[:12])
	frontend, err := ensureCachedBuild(cacheRoot, "frontend", frontendDigest, func(candidate string) error {
		portalBuildEnv := map[string]string{"PORTAL_OUT_DIR": filepath.Join(candidate, "portal-assets")}
		if r.options.hot {
			portalBuildEnv["PORTAL_DEV_HMR"] = "1"
		}
		if err := r.command(ctx, portalBuildEnv, "./engineering/tools/build-frontend.sh"); err != nil {
			return fmt.Errorf("构建 Portal 失败（若依赖尚未安装，请先运行 pnpm install）: %w", err)
		}
		return r.captureFrontendModules(filepath.Join(candidate, "frontend-modules"))
	}, func(candidate string) error {
		return r.validateFrontendBuild(candidate)
	})
	if err != nil {
		return err
	}
	logBuildCacheResult("Portal 与前端插件", frontend)
	if err := materializeCachedDirectory(filepath.Join(frontend.Path, "portal-assets"), filepath.Join(r.runDir, "portal-assets")); err != nil {
		return fmt.Errorf("装配 Portal 构建缓存: %w", err)
	}
	if err := materializeCachedDirectory(filepath.Join(frontend.Path, "frontend-modules"), filepath.Join(r.runDir, "frontend-modules")); err != nil {
		return fmt.Errorf("装配前端插件构建缓存: %w", err)
	}

	dynamicFingerprint, err := r.dynamicGoFingerprint(ctx, goCache)
	if err != nil {
		return err
	}
	dynamicDigest := digestStrings(goIdentity, dynamicFingerprint, "dynamic-go-build-v1")
	log.Printf("[4/6] 准备 bootstrap-policy dynamic-go 制品 digest=%s", dynamicDigest[:12])
	dynamic, err := ensureCachedBuild(cacheRoot, "dynamic-go", dynamicDigest, func(candidate string) error {
		return r.command(ctx, map[string]string{
			"OUT_DIR": filepath.Join(candidate, "dynamic"), "GOCACHE": goCache,
		}, "./engineering/tools/build-dynamic-go.sh")
	}, func(candidate string) error {
		return requireCachedFiles(filepath.Join(candidate, "dynamic"),
			"backend-kernel", "vastplan-go-dynamic-host",
			"cn.vastplan.foundation.security.bootstrap-policy.so",
			"cn.vastplan.foundation.security.bootstrap-policy.tar.gz")
	})
	if err != nil {
		return err
	}
	logBuildCacheResult("dynamic-go", dynamic)
	if err := materializeCachedDirectory(filepath.Join(dynamic.Path, "dynamic"), filepath.Join(r.runDir, "dynamic")); err != nil {
		return fmt.Errorf("装配 dynamic-go 构建缓存: %w", err)
	}

	packageSourceDigest, err := digestBuildInputs(r.options.root, []string{
		"LICENSE", "NOTICE", "extensions/plugins", "engineering/tools/pluginpackage",
	}, []string{backendDigest, frontendDigest, dynamicDigest, "package-build-v2"}, packageBuildInput)
	if err != nil {
		return fmt.Errorf("计算插件制品摘要: %w", err)
	}
	log.Printf("[5/6] 准备本地不可变插件仓库 digest=%s", packageSourceDigest[:12])
	packages, err := ensureCachedBuild(cacheRoot, "packages", packageSourceDigest, func(candidate string) error {
		return r.packageArtifacts(ctx, filepath.Join(candidate, "repository"),
			filepath.Join(r.runDir, "bin"), filepath.Join(r.runDir, "frontend-modules"), filepath.Join(r.runDir, "dynamic"))
	}, func(candidate string) error {
		return r.validatePackageRepository(filepath.Join(candidate, "repository"))
	})
	if err != nil {
		return err
	}
	logBuildCacheResult("插件制品仓库", packages)
	if err := materializeCachedDirectory(filepath.Join(packages.Path, "repository"), filepath.Join(r.runDir, "repository")); err != nil {
		return fmt.Errorf("装配插件仓库缓存: %w", err)
	}
	return nil
}

func logBuildCacheResult(name string, build cachedBuild) {
	if build.Hit {
		log.Printf("复用未变化的%s", name)
		return
	}
	log.Printf("%s构建完成并写入共享缓存", name)
}

func backendBuildInput(relative string) bool {
	if strings.Contains("/"+relative+"/", "/frontend/") {
		return false
	}
	switch filepath.Ext(relative) {
	case ".go", ".mod", ".sum", ".json", ".proto", ".sh":
		return true
	default:
		return filepath.Base(relative) == "VERSION"
	}
}

func packageBuildInput(relative string) bool {
	if strings.HasSuffix(relative, ".pyc") || strings.HasSuffix(relative, ".pyo") {
		return false
	}
	return true
}

func digestStrings(values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = io.WriteString(hash, value)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (r *runtime) dynamicGoFingerprint(ctx context.Context, goCache string) (string, error) {
	command := exec.CommandContext(ctx, "go", "run", "./engineering/tools/dynamicgofingerprint", "-root", ".")
	command.Dir = r.options.root
	command.Env = mergedEnv(map[string]string{"CGO_ENABLED": "1", "GOCACHE": goCache})
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("计算 dynamic-go 构建指纹: %w: %s", err, strings.TrimSpace(string(output)))
	}
	fingerprint := strings.TrimSpace(string(output))
	decoded, err := hex.DecodeString(fingerprint)
	if err != nil || len(decoded) != sha256.Size {
		return "", errors.New("dynamic-go 构建指纹无效")
	}
	return fingerprint, nil
}

func (r *runtime) validateBackendBuild(binDir string) error {
	files := []string{"backend-kernel"}
	specs, err := discoverPackageSpecs(r.options.root)
	if err != nil {
		return err
	}
	for _, spec := range specs {
		if spec.backend {
			files = append(files, spec.id)
		}
	}
	return requireCachedFiles(binDir, files...)
}

func (r *runtime) captureFrontendModules(target string) error {
	specs, err := discoverPackageSpecs(r.options.root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return err
	}
	for _, spec := range specs {
		if !spec.frontend {
			continue
		}
		source := filepath.Join(r.options.root, "extensions", "plugins", spec.id, "frontend", "dist")
		destination := filepath.Join(target, spec.id, "frontend", "dist")
		if err := materializeCachedDirectory(source, destination); err != nil {
			return fmt.Errorf("缓存前端插件 Module Graph %s: %w", spec.id, err)
		}
	}
	return nil
}

func (r *runtime) validateFrontendBuild(candidate string) error {
	if err := requireCachedFiles(filepath.Join(candidate, "portal-assets"), "index.html", "assets/portal-kernel.js", "assets/portal.css"); err != nil {
		return err
	}
	specs, err := discoverPackageSpecs(r.options.root)
	if err != nil {
		return err
	}
	var files []string
	for _, spec := range specs {
		if spec.frontend {
			files = append(files, filepath.Join(spec.id, "frontend", "dist", "vastplan.browser-graph.json"), filepath.Join(spec.id, filepath.FromSlash(spec.frontendEntry)))
			if spec.frontendServerEntry != "" {
				files = append(files, filepath.Join(spec.id, "frontend", "dist", "vastplan.server-graph.json"), filepath.Join(spec.id, filepath.FromSlash(spec.frontendServerEntry)))
			}
		}
	}
	return requireCachedFiles(filepath.Join(candidate, "frontend-modules"), files...)
}

func (r *runtime) validatePackageRepository(repository string) error {
	specs, err := discoverPackageSpecs(r.options.root)
	if err != nil {
		return err
	}
	pluginIDs := make([]string, 0, len(specs)+1)
	for _, spec := range specs {
		pluginIDs = append(pluginIDs, spec.id)
	}
	pluginIDs = append(pluginIDs, "cn.vastplan.foundation.security.bootstrap-policy")
	for _, pluginID := range pluginIDs {
		root := filepath.Join(repository, "artifacts", pluginID)
		found := false
		err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() && entry.Name() == "artifact.json" {
				found = true
			}
			return nil
		})
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("插件仓库缺少 %s", pluginID)
		}
	}
	return nil
}
