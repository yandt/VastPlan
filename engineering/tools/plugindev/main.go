// Command plugindev watches one manifest-discovered Backend plugin, builds
// immutable workspace candidates, and submits them through the existing local
// Test Release path. It is development-only and never starts automatically.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"cdsoft.com.cn/VastPlan/engineering/internal/plugindev"
)

func main() {
	var root, stateRoot, statusURL, selector, target, binding string
	var poll, debounce time.Duration
	flag.StringVar(&root, "root", ".", "VastPlan 仓库根目录")
	flag.StringVar(&stateRoot, "state-root", ".vastplan/dev-platform", "开发状态根目录")
	flag.StringVar(&statusURL, "status-url", "http://127.0.0.1:18080/__vastplan_dev/status", "本地平台状态端点")
	flag.StringVar(&selector, "plugin", "", "插件 ID，或 extensions/plugins、examples/plugins 下的插件目录")
	flag.StringVar(&target, "backend-target", "", "已发布应用中的测试槽位：deployment/unit")
	flag.StringVar(&binding, "backend-binding", "", "可选的既有 TestTargetBinding ID")
	flag.DurationVar(&poll, "poll", 300*time.Millisecond, "源码扫描间隔")
	flag.DurationVar(&debounce, "debounce", 600*time.Millisecond, "连续保存防抖窗口")
	flag.Parse()
	if selector == "" || target == "" {
		log.Fatal("必须提供 -plugin 与 -backend-target")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		log.Fatal(err)
	}
	if !filepath.IsAbs(stateRoot) {
		stateRoot = filepath.Join(root, stateRoot)
	}
	if poll < 100*time.Millisecond || debounce < 100*time.Millisecond || poll > 10*time.Second || debounce > 30*time.Second {
		log.Fatal("-poll/-debounce 超出安全范围")
	}
	spec, err := plugindev.Discover(root, selector)
	if err != nil {
		log.Fatal(err)
	}
	statusPath := filepath.Join(stateRoot, "plugin-dev", "status", safeName(spec.ID)+".json")
	logger := func(format string, values ...any) { log.Printf(format, values...) }
	controller := &plugindev.Controller{
		RepositoryRoot: root, Spec: spec, Target: target, PollInterval: poll, Debounce: debounce,
		Builder: plugindev.CommandBuilder{RepositoryRoot: root, StateRoot: stateRoot, GoCache: filepath.Join(stateRoot, "go-cache")},
		Publisher: plugindev.CommandPublisher{
			RepositoryRoot: root, StateRoot: stateRoot, StatusURL: statusURL, BackendTarget: target,
			BackendBinding: binding, GoCache: filepath.Join(stateRoot, "go-cache"), Logf: logger,
		},
		Status: plugindev.FileStatusWriter{Path: statusPath}, Logf: logger,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("Backend Plugin Dev Controller 已启动 plugin=%s driver=%s target=%s status=%s", spec.ID, spec.Driver, target, statusPath)
	if err := controller.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "Plugin Dev Controller 退出: %v\n", err)
		os.Exit(1)
	}
}

func safeName(value string) string {
	result := make([]rune, 0, len(value))
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			result = append(result, r)
		} else {
			result = append(result, '_')
		}
	}
	return string(result)
}
