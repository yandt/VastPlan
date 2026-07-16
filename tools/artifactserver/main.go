// artifactserver 提供只接受可信签名制品的 HTTPS 仓库服务。
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"cdsoft.com.cn/VastPlan/kernels/backend/pluginservice"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8443", "HTTPS 监听地址")
	repositoryRoot := flag.String("repository", ".vastplan/artifact-server", "不可变制品存储目录")
	trustFile := flag.String("trust", "", "发布者信任文档")
	certFile := flag.String("tls-cert", "", "TLS 证书 PEM")
	keyFile := flag.String("tls-key", "", "TLS 私钥 PEM")
	flag.Parse()
	if *trustFile == "" || *certFile == "" || *keyFile == "" {
		flag.Usage()
		os.Exit(2)
	}
	readToken := os.Getenv("VASTPLAN_ARTIFACT_READ_TOKEN")
	publishToken := os.Getenv("VASTPLAN_ARTIFACT_PUBLISH_TOKEN")
	if readToken == "" || publishToken == "" {
		log.Fatal("必须通过 VASTPLAN_ARTIFACT_READ_TOKEN 和 VASTPLAN_ARTIFACT_PUBLISH_TOKEN 配置分离令牌")
	}
	if readToken == publishToken {
		log.Fatal("制品读令牌与发布令牌必须不同")
	}
	trust, err := pluginservice.LoadTrustStore(*trustFile)
	if err != nil {
		log.Fatal(err)
	}
	local, err := pluginservice.NewRepository(*repositoryRoot)
	if err != nil {
		log.Fatal(err)
	}
	handler := &pluginservice.ArtifactHTTPServer{
		Repository: &pluginservice.SignedRepository{Local: local, Trust: trust},
		ReadToken:  readToken, PublishToken: publishToken, RequireTLS: true,
		Logf: func(format string, values ...any) { log.Printf("[artifact-audit] "+format, values...) },
	}
	server := &http.Server{
		Addr: *addr, Handler: handler,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 5 * time.Minute,
		WriteTimeout: 5 * time.Minute, IdleTimeout: 90 * time.Second,
	}
	log.Printf("远端制品仓库监听 https://%s", *addr)
	if err := server.ListenAndServeTLS(*certFile, *keyFile); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "制品服务退出: %v\n", err)
		os.Exit(1)
	}
}
