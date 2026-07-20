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

// Config is the fully resolved, non-interactive artifact server launch
// configuration. Secret values are injected by a trusted launcher and never
// serialized into a Platform Profile or desired state document.
type Config struct {
	Addr         string
	Repository   string
	TrustFile    string
	TLSCertFile  string
	TLSKeyFile   string
	ReadToken    string
	PublishToken string
}

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
	return RunConfig(ctx, Config{
		Addr: *addr, Repository: *repositoryRoot, TrustFile: *trustFile, TLSCertFile: *certFile, TLSKeyFile: *keyFile,
		ReadToken: os.Getenv("VASTPLAN_ARTIFACT_READ_TOKEN"), PublishToken: os.Getenv("VASTPLAN_ARTIFACT_PUBLISH_TOKEN"),
	}, stderr)
}

// RunConfig starts the HTTPS repository from an already-resolved launch
// profile. It is used by both the ordinary production command and the seed
// bootstrap command, so their transport/trust behavior cannot drift.
func RunConfig(ctx context.Context, config Config, stderr io.Writer) error {
	if ctx == nil || stderr == nil {
		return errors.New("制品服务启动参数无效")
	}
	if config.Addr == "" {
		config.Addr = "127.0.0.1:8443"
	}
	if config.Repository == "" || config.TrustFile == "" || config.TLSCertFile == "" || config.TLSKeyFile == "" {
		return errors.New("制品仓库必须配置存储、信任文档和 TLS 身份")
	}
	if config.ReadToken == "" || config.PublishToken == "" {
		return errors.New("制品仓库必须配置分离的读写令牌")
	}
	if config.ReadToken == config.PublishToken {
		return errors.New("制品读令牌与发布令牌必须不同")
	}
	trust, err := pluginservice.LoadTrustStore(config.TrustFile)
	if err != nil {
		return err
	}
	local, err := pluginservice.NewRepository(config.Repository)
	if err != nil {
		return err
	}
	logger := log.New(stderr, "", log.LstdFlags)
	handler := &artifactapi.Server{
		Repository: pluginservice.HTTPRepositoryAdapter{Repository: &pluginservice.SignedRepository{Local: local, Trust: trust}},
		ReadToken:  config.ReadToken, PublishToken: config.PublishToken, RequireTLS: true,
		Logf: func(format string, values ...any) { logger.Printf("[artifact-audit] "+format, values...) },
	}
	server := &http.Server{
		Addr: config.Addr, Handler: handler, ErrorLog: logger,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 5 * time.Minute,
		WriteTimeout: 5 * time.Minute, IdleTimeout: 90 * time.Second,
	}
	logger.Printf("远端制品仓库监听 https://%s", config.Addr)
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServeTLS(config.TLSCertFile, config.TLSKeyFile) }()
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
