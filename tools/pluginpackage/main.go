// pluginpackage 把一个插件目录和已构建 backend 二进制打成可发布制品。
//
//	用法：go run ./tools/pluginpackage -source plugins/com.example.demo \
//	  -backend-bin dist/demo -out /tmp/demo.tar.gz -repository .vastplan/repository
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"cdsoft.com.cn/VastPlan/kernels/backend/pluginservice"
	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
)

func main() {
	source := flag.String("source", "", "插件目录（须含 vastplan.plugin.json）")
	backendBin := flag.String("backend-bin", "", "写入清单 entry.backend 的已构建可执行文件")
	out := flag.String("out", "", "可选：输出 .tar.gz 文件")
	repositoryRoot := flag.String("repository", "", "可选：直接发布到本地制品仓库")
	channel := flag.String("channel", "stable", "发布 channel")
	flag.Parse()
	if *source == "" || (*out == "" && *repositoryRoot == "") {
		fmt.Fprintln(os.Stderr, "用法: go run ./tools/pluginpackage -source <插件目录> [-backend-bin <二进制>] [-out <制品.tar.gz>] [-repository <仓库>]")
		os.Exit(2)
	}

	packageSource := *source
	if *backendBin != "" {
		stagedSource, cleanup := stageBackendBinary(*source, *backendBin)
		packageSource = stagedSource
		defer cleanup()
	}
	packageBytes, manifest, err := pluginservice.PackageDirectory(packageSource)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打包失败: %v\n", err)
		os.Exit(1)
	}
	if *out != "" {
		if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "创建输出目录失败: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*out, packageBytes, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "写入制品失败: %v\n", err)
			os.Exit(1)
		}
	}
	digest := sha256.Sum256(packageBytes)
	fmt.Printf("已打包 %s@%s\nSHA-256: %s\n", manifest.ID, manifest.Version, hex.EncodeToString(digest[:]))
	if *out != "" {
		fmt.Printf("制品文件: %s\n", *out)
	}
	if *repositoryRoot != "" {
		repository, err := pluginservice.NewRepository(*repositoryRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "打开仓库失败: %v\n", err)
			os.Exit(1)
		}
		artifact, err := repository.Publish(*channel, packageBytes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "发布失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("已发布: %s@%s/%s (%s)\n", artifact.PluginID, artifact.Version, artifact.Channel, artifact.Object)
	}
}

func stageBackendBinary(source, backendBin string) (string, func()) {
	manifestRaw, err := os.ReadFile(filepath.Join(source, "vastplan.plugin.json"))
	if err != nil {
		fatalf("读取插件清单失败: %v", err)
	}
	manifest, err := pluginv1.ParseManifest(manifestRaw)
	if err != nil {
		fatalf("插件清单无效: %v", err)
	}
	entry, ok := manifest.Entry["backend"]
	if !ok {
		fatalf("清单未声明 entry.backend")
	}
	info, err := os.Stat(backendBin)
	if err != nil {
		fatalf("读取 backend 二进制失败: %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		fatalf("backend 二进制不是可执行普通文件: %s", backendBin)
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
	target := filepath.Join(staging, filepath.FromSlash(entry))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		cleanup()
		fatalf("创建 backend 入口目录失败: %v", err)
	}
	if err := copyFile(backendBin, target, 0o755); err != nil {
		cleanup()
		fatalf("写入 backend 入口失败: %v", err)
	}
	return staging, cleanup
}

func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, filename)
		if err != nil {
			return err
		}
		if rel == "." {
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
	defer func() {
		_ = in.Close() // 输入文件只读，输出写入/关闭错误决定复制是否成功。
	}()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	return errors.Join(copyErr, closeErr)
}

func fatalf(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
