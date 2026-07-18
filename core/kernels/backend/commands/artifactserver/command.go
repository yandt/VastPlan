// Package artifactservercommand 实现 Backend 内核的 artifact-server 生产子命令。
package artifactservercommand

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactapi"
)

// Run 启动只接受可信签名制品的 HTTPS 仓库服务，并在 context 取消时优雅关闭。
func Run(ctx context.Context, args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("artifact-server", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addr := flags.String("addr", "127.0.0.1:8443", "HTTPS 监听地址")
	repositoryRoot := flags.String("repository", ".vastplan/artifact-server", "不可变制品存储目录")
	trustFile := flags.String("trust", "", "发布者信任文档")
	certFile := flags.String("tls-cert", "", "TLS 证书 PEM")
	keyFile := flags.String("tls-key", "", "TLS 私钥 PEM")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *trustFile == "" || *certFile == "" || *keyFile == "" {
		flags.Usage()
		return errors.New("必须提供 -trust、-tls-cert 和 -tls-key")
	}
	readToken := os.Getenv("VASTPLAN_ARTIFACT_READ_TOKEN")
	publishToken := os.Getenv("VASTPLAN_ARTIFACT_PUBLISH_TOKEN")
	if readToken == "" || publishToken == "" {
		return errors.New("必须通过 VASTPLAN_ARTIFACT_READ_TOKEN 和 VASTPLAN_ARTIFACT_PUBLISH_TOKEN 配置分离令牌")
	}
	if readToken == publishToken {
		return errors.New("制品读令牌与发布令牌必须不同")
	}
	trust, err := pluginservice.LoadTrustStore(*trustFile)
	if err != nil {
		return err
	}
	local, err := pluginservice.NewRepository(*repositoryRoot)
	if err != nil {
		return err
	}
	logger := log.New(stderr, "", log.LstdFlags)
	handler := &artifactapi.Server{
		Repository: pluginservice.HTTPRepositoryAdapter{Repository: &pluginservice.SignedRepository{Local: local, Trust: trust}},
		ReadToken:  readToken, PublishToken: publishToken, RequireTLS: true,
		Logf: func(format string, values ...any) { logger.Printf("[artifact-audit] "+format, values...) },
	}
	server := &http.Server{
		Addr: *addr, Handler: handler, ErrorLog: logger,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 5 * time.Minute,
		WriteTimeout: 5 * time.Minute, IdleTimeout: 90 * time.Second,
	}
	logger.Printf("远端制品仓库监听 https://%s", *addr)
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServeTLS(*certFile, *keyFile) }()
	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("制品服务退出: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("关闭制品服务: %w", err)
		}
		return nil
	}
}
