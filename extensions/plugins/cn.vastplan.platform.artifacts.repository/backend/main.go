// Command artifactrepository 启动 HTTPS 制品仓库基础插件进程。
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactstorage"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/repositoryruntime"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const pluginID, pluginVersion = "cn.vastplan.platform.artifacts.repository", "0.9.0"

var runtimeRepositoryDescriptor = []byte(`{"title":"制品仓库","subcommands":[{"name":"status","description":"读取仓库运行状态"},{"name":"listCatalog","description":"分页查询已验证制品目录"},{"name":"listPublishJournal","description":"按 revision 查询发布流水账"},{"name":"resolve","description":"生成精确依赖锁"},{"name":"setLifecycle","description":"以 CAS 更新制品生命周期"},{"name":"putReferences","description":"发布完整制品引用快照"},{"name":"listReferences","description":"读取制品引用保护状态"},{"name":"migrationStatus","description":"读取迁移状态"},{"name":"prepareMigration","description":"准备候选 volume"},{"name":"syncMigration","description":"追平候选 volume"},{"name":"cutoverMigration","description":"原子切换候选 volume"},{"name":"rollbackMigration","description":"回滚到源 volume"},{"name":"finalizeMigration","description":"结束观察双写"},{"name":"releaseMigration","description":"隔离旧 volume"}]}`)

type serverConfig struct {
	addr, repository, storageProvider, volumeID, migrationState, trust, cert, key, readToken, publishToken, bundleToken string
}

func loadConfig() (serverConfig, error) {
	var startup struct {
		Listen          string `json:"listen"`
		StorageProvider string `json:"storageProvider"`
		VolumeID        string `json:"volumeId"`
	}
	if err := sdk.DecodeStartupConfiguration(&startup); err != nil {
		return serverConfig{}, err
	}
	config := serverConfig{
		addr:            startup.Listen,
		repository:      os.Getenv("VASTPLAN_ARTIFACT_REPOSITORY"),
		storageProvider: startup.StorageProvider,
		volumeID:        startup.VolumeID,
		migrationState:  os.Getenv("VASTPLAN_ARTIFACT_MIGRATION_STATE"),
		trust:           os.Getenv("VASTPLAN_ARTIFACT_TRUST"),
		cert:            os.Getenv("VASTPLAN_ARTIFACT_TLS_CERT"),
		key:             os.Getenv("VASTPLAN_ARTIFACT_TLS_KEY"),
		readToken:       os.Getenv("VASTPLAN_ARTIFACT_READ_TOKEN"),
		publishToken:    os.Getenv("VASTPLAN_ARTIFACT_PUBLISH_TOKEN"),
		bundleToken:     os.Getenv("VASTPLAN_ARTIFACT_BUNDLE_TOKEN"),
	}
	if config.addr == "" {
		config.addr = "127.0.0.1:8443"
	}
	if config.storageProvider == "" {
		config.storageProvider = "platform.artifacts.storage.file"
	}
	if config.volumeID == "" {
		config.volumeID = "repository.primary"
	}
	if err := artifactstorage.ValidateProviderID(config.storageProvider); err != nil {
		return config, err
	}
	if err := artifactstorage.ValidateVolumeID(config.volumeID); err != nil {
		return config, err
	}
	if config.repository == "" || config.migrationState == "" || config.trust == "" || config.cert == "" || config.key == "" || config.readToken == "" || config.publishToken == "" || config.bundleToken == "" || config.readToken == config.publishToken || config.readToken == config.bundleToken || config.publishToken == config.bundleToken {
		return config, errors.New("制品仓库必须配置存储、信任文档、TLS 证书和互不相同的读取/发布/Bundle 令牌")
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
	trustRaw, err := os.ReadFile(config.trust)
	if err != nil {
		log.Fatalf("读取制品信任快照失败: %v", err)
	}
	manager, err := repositoryruntime.Open(artifactstorage.Volume{
		Handle: "artifact-storage://configured", ProviderID: config.storageProvider, VolumeID: config.volumeID,
		AccessMode: "filesystem", MountPath: config.repository, Generation: 1, Ready: true,
	}, trust, config.migrationState)
	if err != nil {
		log.Fatalf("打开可迁移制品仓库失败: %v", err)
	}
	handler := &artifactapi.Server{
		Repository:   manager,
		ReadToken:    config.readToken,
		PublishToken: config.publishToken,
		RequireTLS:   true,
		Logf: func(format string, args ...any) {
			log.Printf("[artifact-audit] "+format, args...)
		},
	}
	catalogHandler := &catalog.HTTPHandler{
		Store: manager, ReadToken: config.readToken, BundleToken: config.bundleToken, ImportToken: config.publishToken,
		BundleSource: manager, BundleDestination: manager, TrustSnapshot: trustRaw, BundleDirectory: filepath.Join(filepath.Dir(config.migrationState), "bundles"), RequireTLS: true,
		Logf: func(format string, args ...any) { log.Printf("[artifact-audit] "+format, args...) },
	}
	mux := http.NewServeMux()
	mux.Handle("/v1/catalog/", catalogHandler)
	mux.Handle("/", handler)
	server := &http.Server{
		Addr: config.addr, Handler: mux,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 5 * time.Minute,
		WriteTimeout: 5 * time.Minute, IdleTimeout: 90 * time.Second,
	}
	certificate, err := tls.LoadX509KeyPair(config.cert, config.key)
	if err != nil {
		log.Fatalf("加载制品仓库 TLS 身份失败: %v", err)
	}
	listener, err := net.Listen("tcp", config.addr)
	if err != nil {
		log.Fatalf("监听制品仓库地址失败: %v", err)
	}
	tlsListener := tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	var ready atomic.Bool
	ready.Store(true)
	go func() {
		if serveErr := server.Serve(tlsListener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Printf("制品仓库服务退出: %v", serveErr)
		}
		ready.Store(false)
	}()

	p := sdk.New(pluginID, pluginVersion, map[string]string{"backend": "^0.1"})
	p.Contribute(sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage, ID: "platform.artifacts.repository",
		Descriptor: runtimeRepositoryDescriptor,
		Handlers: map[string]sdk.Handler{
			"status": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
				status, marshalErr := json.Marshal(map[string]any{"listen": config.addr, "ready": ready.Load(), "storageProvider": config.storageProvider, "storageVolumeId": manager.ActiveVolume().VolumeID, "catalog": manager.Stats(), "migration": manager.Migration()})
				if marshalErr != nil {
					return nil, nil, marshalErr
				}
				return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, status, nil
			},
			"listCatalog": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
				var request struct {
					PluginID     string `json:"pluginId"`
					PluginPrefix string `json:"pluginPrefix"`
					Namespace    string `json:"namespace"`
					Publisher    string `json:"publisher"`
					Version      string `json:"version"`
					Channel      string `json:"channel"`
					Target       string `json:"target"`
					Page         int    `json:"page"`
					PageSize     int    `json:"pageSize"`
				}
				if err := decodeParams(raw, &request); err != nil {
					return nil, nil, err
				}
				response := manager.Query(catalog.Query{
					PluginID: request.PluginID, PluginPrefix: request.PluginPrefix, Namespace: request.Namespace,
					Publisher: request.Publisher, Version: request.Version, Channel: request.Channel,
					Target: request.Target, Page: request.Page, PageSize: request.PageSize,
				})
				payload, err := json.Marshal(response)
				return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
			},
			"listPublishJournal": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
				var request struct {
					AfterRevision uint64 `json:"afterRevision"`
					Limit         int    `json:"limit"`
				}
				if err := decodeParams(raw, &request); err != nil {
					return nil, nil, err
				}
				payload, err := json.Marshal(manager.Journal(request.AfterRevision, request.Limit))
				return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
			},
			"resolve": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
				var request pluginv1.ArtifactResolveRequest
				if err := decodeParams(raw, &request); err != nil {
					return nil, nil, err
				}
				lock, err := manager.Resolve(request)
				if err != nil {
					return nil, nil, err
				}
				payload, err := json.Marshal(lock)
				return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
			},
			"setLifecycle": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
				var request catalog.LifecycleRequest
				if err := decodeParams(raw, &request); err != nil {
					return nil, nil, err
				}
				entry, revision, err := manager.SetLifecycle(request, time.Now().UTC())
				if err != nil {
					return nil, nil, err
				}
				payload, err := json.Marshal(map[string]any{"revision": revision, "entry": entry})
				return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
			},
			"putReferences": func(_ context.Context, _ sdk.Host, call *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
				if call == nil || call.GetTenantId() == "" || call.GetCaller().GetId() == "" || (call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN && call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_SYSTEM) {
					return nil, nil, errors.New("引用快照必须由可信插件或内核服务身份发布")
				}
				var request pluginv1.ArtifactReferenceSnapshot
				if err := decodeParams(raw, &request); err != nil {
					return nil, nil, err
				}
				if !referenceOwnerAllowed(call.GetCaller().GetId(), request.OwnerKind) || !referenceOwnerIDAllowed(call.GetCaller().GetId(), request.OwnerKind, request.OwnerID) {
					return nil, nil, errors.New("调用者无权声明该引用 owner kind")
				}
				snapshot, revision, err := manager.PutReferences(call.GetTenantId(), call.GetCaller().GetId(), request, time.Now().UTC())
				if err != nil {
					return nil, nil, err
				}
				payload, err := json.Marshal(map[string]any{"revision": revision, "snapshot": snapshot})
				return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
			},
			"listReferences": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
				revision, snapshots := manager.References()
				payload, err := json.Marshal(map[string]any{"revision": revision, "items": snapshots})
				return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, payload, err
			},
			"migrationStatus":   migrationHandler(manager, "migrationStatus"),
			"prepareMigration":  migrationHandler(manager, "prepareMigration"),
			"syncMigration":     migrationHandler(manager, "syncMigration"),
			"cutoverMigration":  migrationHandler(manager, "cutoverMigration"),
			"rollbackMigration": migrationHandler(manager, "rollbackMigration"),
			"finalizeMigration": migrationHandler(manager, "finalizeMigration"),
			"releaseMigration":  migrationHandler(manager, "releaseMigration"),
		},
	})
	if err := p.Serve(); err != nil {
		log.Printf("制品仓库插件退出: %v", err)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

func referenceOwnerAllowed(callerID, ownerKind string) bool {
	switch callerID {
	case "cn.vastplan.platform.infrastructure.deployment-manager":
		return ownerKind == "deployment-active" || ownerKind == "artifact-lock" || ownerKind == "rollback-history"
	case "cn.vastplan.platform.configuration.portal-composer":
		return ownerKind == "portal-activation" || ownerKind == "artifact-lock" || ownerKind == "rollback-history"
	default:
		return strings.HasPrefix(callerID, "node-agent/") && ownerKind == "assignment-active"
	}
}

func referenceOwnerIDAllowed(callerID, ownerKind, ownerID string) bool {
	switch callerID {
	case "cn.vastplan.platform.infrastructure.deployment-manager":
		return strings.HasPrefix(ownerID, "deployment/")
	case "cn.vastplan.platform.configuration.portal-composer":
		return strings.HasPrefix(ownerID, "portal/")
	default:
		nodeID := strings.TrimPrefix(callerID, "node-agent/")
		return ownerKind == "assignment-active" && nodeID != "" && strings.HasPrefix(ownerID, "assignment/") && strings.HasSuffix(ownerID, "/"+nodeID)
	}
}

func decodeParams(raw []byte, target any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("解析仓库查询参数: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("仓库查询参数只能包含一个 JSON 对象")
		}
		return err
	}
	return nil
}
