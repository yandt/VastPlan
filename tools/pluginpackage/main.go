// pluginpackage 把一个插件目录和已构建 backend 二进制打成可发布制品。
//
//	用法：go run ./tools/pluginpackage -source plugins/com.example.demo \
//	  -backend-bin dist/demo -license-file LICENSE -notice-file NOTICE \
//	  -out /tmp/demo.tar.gz -repository .vastplan/repository
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/kernels/backend/pluginservice"
	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
)

func main() {
	source := flag.String("source", "", "插件目录（须含 vastplan.plugin.json）")
	backendBin := flag.String("backend-bin", "", "写入清单 entry.backend 的已构建可执行文件")
	dynamicGoBin := flag.String("dynamic-go-bin", "", "写入 execution.backend.dynamicGo.entry 的首方 Go .so")
	dynamicGoFingerprint := flag.String("dynamic-go-fingerprint", "", "写入签名清单并在 plugin.Open 前校验的 64 位构建指纹")
	licenseFile := flag.String("license-file", "LICENSE", "清单声明 license 时注入制品的许可证文本；默认仓库根 LICENSE")
	noticeFile := flag.String("notice-file", "NOTICE", "清单声明 noticeFile 时注入制品的归属告示；默认仓库根 NOTICE")
	out := flag.String("out", "", "可选：输出 .tar.gz 文件")
	repositoryRoot := flag.String("repository", "", "可选：直接发布到本地制品仓库")
	remoteRepository := flag.String("remote-repository", "", "可选：发布到 HTTPS 远端制品仓库")
	remoteToken := flag.String("remote-token", "", "远端仓库发布令牌；建议通过环境注入")
	trustFile := flag.String("trust", "", "远端仓库发布者信任文档")
	signKey := flag.String("sign-key", "", "Ed25519 PKCS#8 PEM 发布私钥")
	keyID := flag.String("key-id", "", "发布密钥 ID")
	channel := flag.String("channel", "stable", "发布 channel")
	flag.Parse()
	if *remoteToken == "" {
		*remoteToken = os.Getenv("VASTPLAN_ARTIFACT_PUBLISH_TOKEN")
	}
	if *source == "" || (*out == "" && *repositoryRoot == "" && *remoteRepository == "") {
		fmt.Fprintln(os.Stderr, "用法: go run ./tools/pluginpackage -source <插件目录> [-backend-bin <二进制>] [-out <制品.tar.gz>] [-repository <仓库>] [-remote-repository <HTTPS URL> -trust <trust.json> -sign-key <key.pem> -key-id <id>]")
		os.Exit(2)
	}

	packageSource, cleanup := stagePackage(*source, *backendBin, *dynamicGoBin, *dynamicGoFingerprint,
		*licenseFile, *noticeFile)
	defer cleanup()
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
	if *remoteRepository != "" {
		if *trustFile == "" || *signKey == "" || *keyID == "" || *remoteToken == "" {
			fatalf("远端发布必须配置 -trust、-sign-key、-key-id 和发布令牌")
		}
		artifact, err := pluginservice.Describe(*channel, packageBytes)
		if err != nil {
			fatalf("生成制品元数据失败: %v", err)
		}
		privateKey, err := pluginservice.LoadEd25519PrivateKeyPEM(*signKey)
		if err != nil {
			fatalf("加载发布私钥失败: %v", err)
		}
		trust, err := pluginservice.LoadTrustStore(*trustFile)
		if err != nil {
			fatalf("加载信任文档失败: %v", err)
		}
		attestation, err := pluginservice.SignArtifact(artifact, manifest.Publisher, *keyID, privateKey, time.Now().UTC())
		if err != nil {
			fatalf("签署制品失败: %v", err)
		}
		remote := &pluginservice.RemoteRepository{
			BaseURL: *remoteRepository, Token: *remoteToken, Trust: trust,
		}
		published, err := remote.PublishRemote(context.Background(), attestation, packageBytes)
		if err != nil {
			fatalf("远端发布失败: %v", err)
		}
		fmt.Printf("已签名并发布到远端: %s@%s/%s publisher=%s keyId=%s sha256=%s\n",
			published.PluginID, published.Version, published.Channel, manifest.Publisher, *keyID, published.SHA256)
	}
}

// stagePackage 只在需要注入已构建二进制或许可证文本时创建临时目录。
// 许可证目的路径来自已校验清单，不能由命令行改写（ADR-0046）。
func stagePackage(source, backendBin, dynamicGoBin, dynamicGoFingerprint, licenseSource, noticeSource string) (string, func()) {
	manifestRaw, err := os.ReadFile(filepath.Join(source, "vastplan.plugin.json"))
	if err != nil {
		fatalf("读取插件清单失败: %v", err)
	}
	manifest, err := pluginv1.ParseManifest(manifestRaw)
	if err != nil {
		fatalf("插件清单无效: %v", err)
	}
	licensePresent := true
	if manifest.License != "" {
		licensePresent, err = regularNonempty(filepath.Join(source, filepath.FromSlash(manifest.LicenseFile)))
		if err != nil {
			fatalf("读取插件许可证文件失败: %v", err)
		}
	}
	noticePresent := true
	if manifest.NoticeFile != "" {
		noticePresent, err = regularNonempty(filepath.Join(source, filepath.FromSlash(manifest.NoticeFile)))
		if err != nil {
			fatalf("读取插件归属告示失败: %v", err)
		}
	}
	if backendBin == "" && dynamicGoBin == "" && licensePresent && noticePresent {
		return source, func() {}
	}
	var dynamicGoEntry string
	if dynamicGoBin != "" {
		if manifest.Execution == nil || manifest.Execution.Backend == nil || manifest.Execution.Backend.DynamicGo == nil {
			fatalf("清单未声明 execution.backend.dynamicGo")
		}
		dynamicGoEntry = manifest.Execution.Backend.DynamicGo.Entry
		dynamicGoFingerprint = strings.TrimSpace(dynamicGoFingerprint)
		decoded, decodeErr := hex.DecodeString(dynamicGoFingerprint)
		if decodeErr != nil || len(decoded) != sha256.Size || dynamicGoFingerprint != strings.ToLower(dynamicGoFingerprint) {
			fatalf("-dynamic-go-fingerprint 必须是 64 位小写 SHA-256 十六进制值")
		}
		manifest.Execution.Backend.DynamicGo.Fingerprint = dynamicGoFingerprint
		info, statErr := os.Stat(dynamicGoBin)
		if statErr != nil {
			fatalf("读取 dynamic-go 模块失败: %v", statErr)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			fatalf("dynamic-go 模块不是非空普通文件: %s", dynamicGoBin)
		}
	}

	var entry string
	if backendBin != "" {
		var ok bool
		entry, ok = manifest.Entry["backend"]
		if !ok {
			fatalf("清单未声明 entry.backend")
		}
		info, statErr := os.Stat(backendBin)
		if statErr != nil {
			fatalf("读取 backend 二进制失败: %v", statErr)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			fatalf("backend 二进制不是可执行普通文件: %s", backendBin)
		}
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
	if backendBin != "" {
		target := filepath.Join(staging, filepath.FromSlash(entry))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			cleanup()
			fatalf("创建 backend 入口目录失败: %v", err)
		}
		if err := copyFile(backendBin, target, 0o755); err != nil {
			cleanup()
			fatalf("写入 backend 入口失败: %v", err)
		}
	}
	if dynamicGoBin != "" {
		target := filepath.Join(staging, filepath.FromSlash(dynamicGoEntry))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			cleanup()
			fatalf("创建 dynamic-go 入口目录失败: %v", err)
		}
		if err := copyFile(dynamicGoBin, target, 0o644); err != nil {
			cleanup()
			fatalf("写入 dynamic-go 模块失败: %v", err)
		}
		manifestJSON, marshalErr := json.MarshalIndent(manifest, "", "  ")
		if marshalErr != nil {
			cleanup()
			fatalf("编码 dynamic-go 签名清单失败: %v", marshalErr)
		}
		manifestJSON = append(manifestJSON, '\n')
		if err := os.WriteFile(filepath.Join(staging, "vastplan.plugin.json"), manifestJSON, 0o644); err != nil {
			cleanup()
			fatalf("写入 dynamic-go 签名清单失败: %v", err)
		}
	}
	if !licensePresent {
		if licenseSource == "" {
			cleanup()
			fatalf("清单声明了 %s，但未提供 -license-file", manifest.License)
		}
		target := filepath.Join(staging, filepath.FromSlash(manifest.LicenseFile))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			cleanup()
			fatalf("创建许可证文件目录失败: %v", err)
		}
		if err := copyFile(licenseSource, target, 0o644); err != nil {
			cleanup()
			fatalf("注入许可证文件失败: %v", err)
		}
	}
	if !noticePresent {
		if noticeSource == "" {
			cleanup()
			fatalf("清单声明了 noticeFile，但未提供 -notice-file")
		}
		target := filepath.Join(staging, filepath.FromSlash(manifest.NoticeFile))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			cleanup()
			fatalf("创建归属告示目录失败: %v", err)
		}
		if err := copyFile(noticeSource, target, 0o644); err != nil {
			cleanup()
			fatalf("注入归属告示失败: %v", err)
		}
	}
	return staging, cleanup
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
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if entry.IsDir() && entry.Name() == "__pycache__" {
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
