package plugindev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type Candidate struct {
	PluginID     string
	Version      string
	SourceDigest string
	PackageFile  string
	Generation   uint64
}

type Builder interface {
	Build(context.Context, Spec, string, uint64) (Candidate, error)
}

type CommandBuilder struct {
	RepositoryRoot string
	StateRoot      string
	GoCache        string
}

func (b CommandBuilder) Build(ctx context.Context, spec Spec, digest string, generation uint64) (Candidate, error) {
	version, err := WorkspaceVersion(spec.Version, digest[:16])
	if err != nil {
		return Candidate{}, err
	}
	root, err := filepath.Abs(b.RepositoryRoot)
	if err != nil {
		return Candidate{}, err
	}
	buildRoot := filepath.Join(b.StateRoot, "plugin-dev", "builds", safeComponent(spec.ID), fmt.Sprintf("generation-%06d", generation))
	if err := os.RemoveAll(buildRoot); err != nil {
		return Candidate{}, err
	}
	if err := os.MkdirAll(buildRoot, 0o700); err != nil {
		return Candidate{}, err
	}
	stagingRoot := filepath.Join(root, ".vastplan", "plugin-dev-staging")
	if err := os.MkdirAll(stagingRoot, 0o700); err != nil {
		return Candidate{}, err
	}
	stagedSource, err := os.MkdirTemp(stagingRoot, safeComponent(spec.ID)+"-")
	if err != nil {
		return Candidate{}, err
	}
	defer os.RemoveAll(stagedSource)
	if err := copyDevelopmentTree(spec.SourceRoot, stagedSource); err != nil {
		return Candidate{}, fmt.Errorf("暂存插件源码: %w", err)
	}
	manifest := spec.Manifest
	manifest.Version = version
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Candidate{}, err
	}
	if _, err := pluginv1.ParseManifest(manifestRaw); err != nil {
		return Candidate{}, err
	}
	if err := os.WriteFile(filepath.Join(stagedSource, "vastplan.plugin.json"), append(manifestRaw, '\n'), 0o600); err != nil {
		return Candidate{}, err
	}

	packageArgs := []string{"run", "./engineering/tools/pluginpackage", "-source", stagedSource, "-sbom-dependency-source", spec.SourceRoot, "-license-file", filepath.Join(root, "LICENSE"), "-notice-file", filepath.Join(root, "NOTICE")}
	switch spec.Driver {
	case DriverNone:
		if spec.Entry != "" {
			return Candidate{}, errors.New("Backend 入口缺少构建驱动")
		}
	case DriverNativeGo:
		binary := filepath.Join(buildRoot, "backend", spec.ID)
		if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
			return Candidate{}, err
		}
		if err := b.run(ctx, root, "go", "build", "-trimpath", "-buildvcs=false", "-o", binary, "./"+spec.Relative+"/backend"); err != nil {
			return Candidate{}, fmt.Errorf("构建 native-go 候选: %w", err)
		}
		packageArgs = append(packageArgs, "-backend-bin", binary)
	case DriverNodeWorker:
		modules := filepath.Join(buildRoot, "node")
		if err := b.run(ctx, root, "pnpm", "install", "--offline", "--frozen-lockfile"); err != nil {
			return Candidate{}, fmt.Errorf("复核 Node 依赖锁: %w", err)
		}
		if err := b.run(ctx, root, "node", "engineering/tools/build-node-backend-plugins.mjs", "--out-dir", modules, "--plugin", spec.ID); err != nil {
			return Candidate{}, fmt.Errorf("构建 node-worker 候选: %w", err)
		}
		packageArgs = append(packageArgs, "-backend-module", filepath.Join(modules, spec.ID, filepath.FromSlash(spec.Entry)))
	case DriverPython, DriverPythonInterpreter:
		// Python source and its signed pylock/wheels are packaged directly. The
		// Node Agent remains the only installer of the verified environment.
	case DriverDynamicGo:
		fingerprintRaw, err := b.output(ctx, root, "go", "run", "./engineering/tools/dynamicgofingerprint", "-root", ".")
		if err != nil {
			return Candidate{}, fmt.Errorf("计算 dynamic-go 指纹: %w", err)
		}
		fingerprint := strings.TrimSpace(string(fingerprintRaw))
		binary := filepath.Join(buildRoot, "backend", spec.ID)
		module := filepath.Join(buildRoot, "dynamic", spec.ID+".so")
		for _, directory := range []string{filepath.Dir(binary), filepath.Dir(module)} {
			if err := os.MkdirAll(directory, 0o700); err != nil {
				return Candidate{}, err
			}
		}
		if err := b.run(ctx, root, "go", "build", "-trimpath", "-buildvcs=false", "-o", binary, "./"+spec.Relative+"/backend"); err != nil {
			return Candidate{}, fmt.Errorf("构建 dynamic-go 进程入口: %w", err)
		}
		dynamicPackage := "./" + spec.Relative + "/dynamic"
		if err := b.runWithEnv(ctx, root, map[string]string{"CGO_ENABLED": "1"}, "go", "build", "-trimpath", "-buildvcs=false", "-buildmode=plugin", "-ldflags", "-X main.dynamicGoBuildFingerprint="+fingerprint, "-o", module, dynamicPackage); err != nil {
			return Candidate{}, fmt.Errorf("构建 dynamic-go 模块: %w", err)
		}
		packageArgs = append(packageArgs, "-backend-bin", binary, "-dynamic-go-bin", module, "-dynamic-go-fingerprint", fingerprint)
	default:
		return Candidate{}, fmt.Errorf("不支持的开发构建驱动 %q", spec.Driver)
	}
	if spec.FrontendEntry != "" {
		frontendRoot := filepath.Join(buildRoot, "frontend")
		frontendManifest := filepath.Join(buildRoot, "frontend-modules.json")
		if err := b.run(ctx, root, "pnpm", "install", "--offline", "--frozen-lockfile"); err != nil {
			return Candidate{}, fmt.Errorf("复核 Frontend 依赖锁: %w", err)
		}
		if err := b.run(ctx, root, "node", "engineering/tools/build-frontend-plugins.mjs", "--out-dir", frontendRoot, "--manifest", frontendManifest, "--plugin", spec.ID); err != nil {
			return Candidate{}, fmt.Errorf("构建 Frontend 候选: %w", err)
		}
		graphRoot := filepath.Join(frontendRoot, spec.ID)
		graphFile := filepath.Join(graphRoot, "frontend", "dist", "vastplan.browser-graph.json")
		packageArgs = append(packageArgs, "-frontend-graph", graphFile, "-frontend-graph-root", graphRoot)
		if strings.TrimSpace(spec.Manifest.Entry["frontendServer"]) != "" {
			packageArgs = append(packageArgs, "-frontend-server-graph", filepath.Join(graphRoot, "frontend", "dist", "vastplan.server-graph.json"))
		}
	}
	packageFile := filepath.Join(buildRoot, spec.ID+"-"+version+".tar.gz")
	packageArgs = append(packageArgs, "-out", packageFile)
	if err := b.run(ctx, root, "go", packageArgs...); err != nil {
		return Candidate{}, fmt.Errorf("打包 workspace 候选: %w", err)
	}
	return Candidate{PluginID: spec.ID, Version: version, SourceDigest: digest, PackageFile: packageFile, Generation: generation}, nil
}

func (b CommandBuilder) run(ctx context.Context, directory, name string, arguments ...string) error {
	return b.runWithEnv(ctx, directory, nil, name, arguments...)
}

func (b CommandBuilder) runWithEnv(ctx context.Context, directory string, extra map[string]string, name string, arguments ...string) error {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = directory
	command.Env = append([]string(nil), os.Environ()...)
	if b.GoCache != "" {
		command.Env = append(command.Env, "GOCACHE="+b.GoCache)
	}
	for key, value := range extra {
		command.Env = append(command.Env, key+"="+value)
	}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s: %w\n%s", name, err, strings.TrimSpace(output.String()))
	}
	return nil
}

func (b CommandBuilder) output(ctx context.Context, directory, name string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = directory
	command.Env = append([]string(nil), os.Environ()...)
	if b.GoCache != "" {
		command.Env = append(command.Env, "GOCACHE="+b.GoCache)
	}
	return command.CombinedOutput()
}

func copyDevelopmentTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return os.MkdirAll(target, 0o700)
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".vastplan", "node_modules", "__pycache__", "graphify-out":
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(target, relative), 0o700)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("workspace 包不允许符号链接: %s", relative)
		}
		if !entry.Type().IsRegular() || ignoredDevelopmentFile(relative) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyDevelopmentFile(path, filepath.Join(target, relative), info.Mode().Perm())
	})
}

func copyDevelopmentFile(source, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	return errors.Join(copyErr, output.Close())
}

func safeComponent(value string) string {
	return strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(value)
}
