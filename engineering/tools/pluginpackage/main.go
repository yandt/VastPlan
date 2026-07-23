// pluginpackage assembles built Backend/Frontend entries into a signed plugin artifact.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
)

func main() {
	source := flag.String("source", "", "插件目录（须含 vastplan.plugin.json）")
	packageFile := flag.String("package", "", "复用已生成的不可变 .tar.gz；与 -source 互斥")
	backendBin := flag.String("backend-bin", "", "写入清单 entry.backend 的已构建可执行文件")
	backendModule := flag.String("backend-module", "", "写入 node-worker entry.backend 的自包含 ESM bundle")
	frontendBundle := flag.String("frontend-bundle", "", "写入清单 entry.frontend 的旧单文件 ESM bundle")
	frontendGraph := flag.String("frontend-graph", "", "写入签名清单的 browser Module Graph JSON")
	frontendServerGraph := flag.String("frontend-server-graph", "", "写入签名清单的 server Module Graph JSON")
	frontendGraphRoot := flag.String("frontend-graph-root", "", "Module Graph 节点路径相对的已构建目录根")
	dynamicGoBin := flag.String("dynamic-go-bin", "", "写入 execution.backend.dynamicGo.entry 的首方 Go .so")
	dynamicGoFingerprint := flag.String("dynamic-go-fingerprint", "", "写入签名清单并在 plugin.Open 前校验的 64 位构建指纹")
	licenseFile := flag.String("license-file", "LICENSE", "清单声明 license 时注入制品的许可证文本；默认仓库根 LICENSE")
	noticeFile := flag.String("notice-file", "NOTICE", "清单声明 noticeFile 时注入制品的归属告示；默认仓库根 NOTICE")
	sbomFile := flag.String("sbom", "", "可选：注入并绑定标准 CycloneDX JSON SBOM")
	out := flag.String("out", "", "可选：输出 .tar.gz 文件")
	repositoryRoot := flag.String("repository", "", "可选：直接发布到本地制品仓库")
	remoteRepository := flag.String("remote-repository", "", "可选：发布到 HTTPS 远端制品仓库")
	remoteToken := flag.String("remote-token", "", "远端仓库发布令牌；建议通过环境注入")
	remoteReadToken := flag.String("remote-read-token", "", "远端仓库读取令牌；stable 发布用于复验 testing 候选")
	remoteCA := flag.String("remote-ca", "", "远端仓库自定义 CA PEM")
	remoteTimeout := flag.Duration("remote-timeout", 5*time.Minute, "远端发布总超时")
	trustFile := flag.String("trust", "", "远端仓库发布者信任文档")
	signKey := flag.String("sign-key", "", "Ed25519 PKCS#8 PEM 发布私钥")
	keyID := flag.String("key-id", "", "发布密钥 ID")
	channel := flag.String("channel", "stable", "发布 channel")
	flag.Parse()
	if *remoteToken == "" {
		*remoteToken = os.Getenv("VASTPLAN_ARTIFACT_PUBLISH_TOKEN")
	}
	if *remoteReadToken == "" {
		*remoteReadToken = os.Getenv("VASTPLAN_ARTIFACT_READ_TOKEN")
	}
	if (*source == "") == (*packageFile == "") || (*out == "" && *repositoryRoot == "" && *remoteRepository == "") {
		fmt.Fprintln(os.Stderr, "用法: go run ./engineering/tools/pluginpackage (-source <插件目录> | -package <候选.tar.gz>) [-out <制品.tar.gz>] [-remote-repository <HTTPS 仓库>]")
		os.Exit(2)
	}

	var packageBytes []byte
	var manifestID, manifestVersion, manifestPublisher string
	if *packageFile != "" {
		if *backendBin != "" || *backendModule != "" || *frontendBundle != "" || *frontendGraph != "" || *frontendServerGraph != "" || *frontendGraphRoot != "" || *dynamicGoBin != "" || *dynamicGoFingerprint != "" || *sbomFile != "" {
			fatalf("-package 复用不可变候选时不能再注入 Backend、Frontend、dynamic-go 或 SBOM 内容")
		}
		var err error
		packageBytes, manifestID, manifestVersion, manifestPublisher, err = loadExistingPackage(*packageFile)
		if err != nil {
			fatalf("读取候选制品失败: %v", err)
		}
	} else {
		effectiveSBOM := *sbomFile
		if effectiveSBOM == "" {
			generated, cleanup, err := generateAutomaticSBOM(automaticSBOMInputs{Source: *source, BackendBin: *backendBin, NodeBackendModule: *backendModule, FrontendGraphRoot: *frontendGraphRoot, DynamicGoBin: *dynamicGoBin})
			if err != nil {
				fatalf("自动生成插件 SBOM 失败: %v", err)
			}
			defer cleanup()
			effectiveSBOM = generated
		}
		packageSource, cleanup := stagePackageWithSupplyChain(*source, *backendBin, *backendModule, *frontendBundle, *frontendGraph, *frontendServerGraph, *frontendGraphRoot, *dynamicGoBin, *dynamicGoFingerprint, *licenseFile, *noticeFile, effectiveSBOM)
		defer cleanup()
		var err error
		builtBytes, manifest, err := pluginservice.PackageDirectory(packageSource)
		if err != nil {
			fatalf("打包失败: %v", err)
		}
		packageBytes = builtBytes
		manifestID, manifestVersion, manifestPublisher = manifest.ID, manifest.Version, manifest.Publisher
	}
	if *out != "" {
		if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
			fatalf("创建输出目录失败: %v", err)
		}
		if err := os.WriteFile(*out, packageBytes, 0o644); err != nil {
			fatalf("写入制品失败: %v", err)
		}
	}
	digest := sha256.Sum256(packageBytes)
	fmt.Printf("已准备 %s@%s\nSHA-256: %s\n", manifestID, manifestVersion, hex.EncodeToString(digest[:]))
	if *out != "" {
		fmt.Printf("制品文件: %s\n", *out)
	}
	if *repositoryRoot != "" {
		repository, err := pluginservice.NewRepository(*repositoryRoot)
		if err != nil {
			fatalf("打开仓库失败: %v", err)
		}
		artifact, err := repository.Publish(*channel, packageBytes)
		if err != nil {
			fatalf("发布失败: %v", err)
		}
		fmt.Printf("已发布: %s@%s/%s (%s)\n", artifact.PluginID, artifact.Version, artifact.Channel, artifact.Object)
	}
	if *remoteRepository != "" {
		published, err := publishRemote(packageBytes, manifestPublisher, *channel, remotePublishOptions{RepositoryURL: *remoteRepository, PublishToken: *remoteToken, ReadToken: *remoteReadToken, TrustFile: *trustFile, SignKey: *signKey, KeyID: *keyID, CAFile: *remoteCA, Timeout: *remoteTimeout})
		if err != nil {
			fatalf("远端发布失败: %v", err)
		}
		fmt.Printf("已签名并发布到远端: %s@%s/%s publisher=%s keyId=%s sha256=%s\n", published.PluginID, published.Version, published.Channel, manifestPublisher, *keyID, published.SHA256)
	}
}

func fatalf(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
