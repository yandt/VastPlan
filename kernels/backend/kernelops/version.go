// Package kernelops 提供随 Backend 正式二进制交付的离线运维入口。
//
// 这些入口只读取本地配置和状态，不启动插件、不连接控制面，也不把生产排障依赖
// 源码仓库或 Go 工具链。
package kernelops

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
)

// VersionInfo 是可供发布校验和支持包复用的稳定版本视图。
type VersionInfo struct {
	Kernel    string `json:"kernel"`
	Version   string `json:"version"`
	GoVersion string `json:"go_version"`
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
}

func versionInfo(version string) VersionInfo {
	goVersion := runtime.Version()
	if info, ok := debug.ReadBuildInfo(); ok && info.GoVersion != "" {
		goVersion = info.GoVersion
	}
	return VersionInfo{
		Kernel: "backend", Version: version, GoVersion: goVersion,
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
	}
}

// PrintVersion 输出人可读版本；--json 用于发布流水线和支持工具的机器校验。
func PrintVersion(output io.Writer, version string, args []string) error {
	if len(args) == 0 {
		_, err := fmt.Fprintf(output, "backend@%s %s/%s %s\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
		return err
	}
	if len(args) != 1 || args[0] != "--json" {
		return errors.New("用法: version [--json]")
	}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(versionInfo(version))
}
