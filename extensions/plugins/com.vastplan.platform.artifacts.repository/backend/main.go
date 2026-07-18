// Command artifactrepository 启动 HTTPS 制品仓库基础插件进程。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactapi"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const pluginID, pluginVersion = "com.vastplan.platform.artifacts.repository", "0.2.0"

type serverConfig struct {
	addr, repository, trust, cert, key, readToken, publishToken string
}

func loadConfig() (serverConfig, error) {
	config := serverConfig{
		addr:         os.Getenv("VASTPLAN_ARTIFACT_LISTEN_ADDR"),
		repository:   os.Getenv("VASTPLAN_ARTIFACT_REPOSITORY"),
		trust:        os.Getenv("VASTPLAN_ARTIFACT_TRUST"),
		cert:         os.Getenv("VASTPLAN_ARTIFACT_TLS_CERT"),
		key:          os.Getenv("VASTPLAN_ARTIFACT_TLS_KEY"),
		readToken:    os.Getenv("VASTPLAN_ARTIFACT_READ_TOKEN"),
		publishToken: os.Getenv("VASTPLAN_ARTIFACT_PUBLISH_TOKEN"),
	}
	if config.addr == "" {
		config.addr = "127.0.0.1:8443"
	}
	if config.repository == "" || config.trust == "" || config.cert == "" || config.key == "" || config.readToken == "" || config.publishToken == "" || config.readToken == config.publishToken {
		return config, errors.New("制品仓库必须配置存储、信任文档、TLS 证书和不同的读写令牌")
	}
	return config, nil
}

func main() {
	config, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	trust, err := pluginservice.LoadTrustStore(config.trust)
	if err != nil {
		log.Fatal(err)
	}
	local, err := pluginservice.NewRepository(config.repository)
	if err != nil {
		log.Fatal(err)
	}
	handler := &artifactapi.Server{
		Repository:   pluginservice.HTTPRepositoryAdapter{Repository: &pluginservice.SignedRepository{Local: local, Trust: trust}},
		ReadToken:    config.readToken,
		PublishToken: config.publishToken,
		RequireTLS:   true,
		Logf: func(format string, args ...any) {
			log.Printf("[artifact-audit] "+format, args...)
		},
	}
	server := &http.Server{
		Addr: config.addr, Handler: handler,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 5 * time.Minute,
		WriteTimeout: 5 * time.Minute, IdleTimeout: 90 * time.Second,
	}
	go func() {
		if serveErr := server.ListenAndServeTLS(config.cert, config.key); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Printf("制品仓库服务退出: %v", serveErr)
		}
	}()

	p := sdk.New(pluginID, pluginVersion, map[string]string{"backend": "^1.0"})
	p.Contribute(sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage, ID: "platform.artifacts.repository",
		Descriptor: []byte(`{"title":"制品仓库状态","subcommands":[{"name":"status","description":"读取仓库运行状态"}]}`),
		Handlers: map[string]sdk.Handler{"status": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
			status, marshalErr := json.Marshal(map[string]any{"listen": config.addr, "ready": true})
			if marshalErr != nil {
				return nil, nil, marshalErr
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, status, nil
		}},
	})
	if err := p.Serve(); err != nil {
		log.Printf("制品仓库插件退出: %v", err)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}
