// artifactkey 生成制品发布用 Ed25519 私钥和可分发的信任文档。
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
)

func main() {
	publisher := flag.String("publisher", "", "插件清单中的 publisher")
	keyID := flag.String("key-id", "", "发布密钥稳定 ID，例如 release-2026-01")
	privateOut := flag.String("private-out", "", "PKCS#8 PEM 私钥输出路径")
	trustOut := flag.String("trust-out", "", "公开信任文档输出路径")
	flag.Parse()
	if *publisher == "" || *keyID == "" || *privateOut == "" || *trustOut == "" {
		flag.Usage()
		os.Exit(2)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		fatalf("生成 Ed25519 密钥失败: %v", err)
	}
	privatePEM, err := pluginservice.MarshalEd25519PrivateKeyPEM(privateKey)
	if err != nil {
		fatalf("编码私钥失败: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(*privateOut), 0o700); err != nil {
		fatalf("创建私钥目录失败: %v", err)
	}
	privateFile, err := os.OpenFile(*privateOut, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		fatalf("创建私钥文件失败（不会覆盖已有密钥）: %v", err)
	}
	if _, err := privateFile.Write(privatePEM); err != nil {
		_ = privateFile.Close()
		fatalf("写入私钥失败: %v", err)
	}
	if err := privateFile.Close(); err != nil {
		fatalf("关闭私钥文件失败: %v", err)
	}
	document := pluginservice.TrustDocumentForPublicKeys(pluginservice.TrustKey{
		Publisher: *publisher, KeyID: *keyID,
		PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	})
	trustJSON, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		fatalf("编码信任文档失败: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(*trustOut), 0o755); err != nil {
		fatalf("创建信任文档目录失败: %v", err)
	}
	if err := os.WriteFile(*trustOut, append(trustJSON, '\n'), 0o644); err != nil {
		fatalf("写入信任文档失败: %v", err)
	}
	fmt.Printf("已生成发布密钥 publisher=%s keyId=%s\n私钥: %s\n信任文档: %s\n", *publisher, *keyID, *privateOut, *trustOut)
}

func fatalf(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
