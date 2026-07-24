package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type frozenNodeInstaller func(context.Context, string) ([]byte, error)

// ensureFrozenNodeDependencies repairs the workspace links from the committed
// lock before any frontend build or SBOM capture. The build must describe the
// dependencies it actually consumed, so silently accepting a stale
// node_modules tree would make a stable plugin ref non-reproducible.
func ensureFrozenNodeDependencies(ctx context.Context, root string, install frozenNodeInstaller) error {
	for _, name := range []string{"package.json", "pnpm-lock.yaml"} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("Node 工作区缺少 %s", name)
		}
	}
	if install == nil {
		install = runFrozenNodeInstall
	}
	output, err := install(ctx, root)
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	detail := strings.TrimSpace(string(output))
	if detail != "" {
		detail = ": " + detail
	}
	return fmt.Errorf("离线对齐 Node 依赖失败%s: %w；请先在项目根目录运行 pnpm install --frozen-lockfile，再重试", detail, err)
}

func runFrozenNodeInstall(ctx context.Context, root string) ([]byte, error) {
	command := exec.CommandContext(ctx, "pnpm", "install", "--offline", "--frozen-lockfile")
	command.Dir = root
	output, err := command.CombinedOutput()
	if errors.Is(err, exec.ErrNotFound) {
		return output, errors.New("找不到 pnpm")
	}
	return output, err
}
