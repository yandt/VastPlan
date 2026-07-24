package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"time"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactrepository/localtest"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/repositoryruntime"
)

type runningRepositoryTransport struct {
	server           *http.Server
	ready            atomic.Bool
	tickets          *dataPlaneTicketStore
	assessmentLeases *assessmentLeaseIssuer
	localAdapter     *repositoryruntime.LocalTestAdapter
}

func startRepositoryTransport(config serverConfig, manager *repositoryruntime.Manager, trustRaw []byte) (*runningRepositoryTransport, error) {
	running := &runningRepositoryTransport{}
	var listener net.Listener
	var err error
	switch config.profile.Protocol {
	case artifactrepositoryv1.ProtocolLocalTest:
		adapter, adapterErr := repositoryruntime.NewLocalTestAdapter(config.profile, manager)
		if adapterErr != nil {
			return nil, adapterErr
		}
		running.localAdapter = adapter
		handler, handlerErr := localtest.NewServer(config.profile, adapter, config.localToken)
		if handlerErr != nil {
			return nil, handlerErr
		}
		listener, err = localtest.Listen(config.profile)
		if err != nil {
			return nil, err
		}
		running.server = &http.Server{
			Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 5 * time.Minute,
			WriteTimeout: 5 * time.Minute, IdleTimeout: 30 * time.Second,
		}
	case artifactrepositoryv1.ProtocolRemote:
		running.tickets, running.assessmentLeases, err = remoteDataPlane(config, manager)
		if err != nil {
			return nil, err
		}
		handler := &artifactapi.Server{
			Repository: manager, ReadToken: config.readToken, PublishToken: config.publishToken, RequireTLS: true,
			Logf: func(format string, args ...any) { log.Printf("[artifact-audit] "+format, args...) },
		}
		catalogHandler := &catalog.HTTPHandler{
			Store: manager, ReadToken: config.readToken, BundleToken: config.bundleToken, ImportToken: config.publishToken, AssessmentToken: config.assessmentToken,
			BundleSource: manager, BundleDestination: manager, TrustSnapshot: trustRaw,
			BundleDirectory: filepath.Join(filepath.Dir(config.migrationState), "bundles"), RequireTLS: true,
			Logf: func(format string, args ...any) { log.Printf("[artifact-audit] "+format, args...) },
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", repositoryHealth)
		mux.Handle("/v1/catalog/", catalogHandler)
		mux.Handle("/", handler)
		running.server = &http.Server{
			Addr: config.addr, Handler: dataPlaneTicketMiddleware(mux, running.tickets, config.readToken),
			ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 5 * time.Minute,
			WriteTimeout: 5 * time.Minute, IdleTimeout: 90 * time.Second,
		}
		certificate, certificateErr := tls.LoadX509KeyPair(config.cert, config.key)
		if certificateErr != nil {
			return nil, certificateErr
		}
		tcpListener, listenErr := net.Listen("tcp", config.addr)
		if listenErr != nil {
			return nil, listenErr
		}
		listener = tls.NewListener(tcpListener, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	default:
		return nil, errors.New("未注册的制品仓库协议")
	}

	running.ready.Store(true)
	go func() {
		if serveErr := running.server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Printf("制品仓库 %s 传输退出: %v", config.profile.Protocol, serveErr)
		}
		running.ready.Store(false)
	}()
	return running, nil
}

func (r *runningRepositoryTransport) validateReceipt(profile artifactrepositoryv1.Profile, manager *repositoryruntime.Manager, receipt artifactrepositoryv1.Receipt, target string) (catalog.Entry, error) {
	if err := artifactrepositoryv1.ValidateReceipt(profile, receipt); err != nil {
		return catalog.Entry{}, err
	}
	if r != nil && r.localAdapter != nil {
		if err := r.localAdapter.ValidateReceipt(receipt, time.Now().UTC()); err != nil {
			return catalog.Entry{}, err
		}
	}
	page := manager.Query(catalog.Query{PluginID: receipt.Ref.PluginID, Version: receipt.Ref.Version, Channel: receipt.Ref.Channel, Target: target, Page: 1, PageSize: 2})
	if page.Total != 1 || len(page.Items) != 1 {
		return catalog.Entry{}, errors.New("仓库回执没有命中唯一 Catalog 条目")
	}
	entry := page.Items[0]
	if entry.Ref != receipt.Ref || entry.SHA256 != receipt.SHA256 || entry.RepositoryRevision != receipt.Revision {
		return catalog.Entry{}, errors.New("仓库回执与不可变 Catalog 发生漂移")
	}
	return entry, nil
}

func remoteDataPlane(config serverConfig, manager *repositoryruntime.Manager) (*dataPlaneTicketStore, *assessmentLeaseIssuer, error) {
	var tickets *dataPlaneTicketStore
	if config.apiExposure != nil {
		tickets = newDataPlaneTicketStore(config.apiExposure.InstanceID)
	}
	leases, err := newAssessmentLeaseIssuer(manager, tickets, config.apiExposure)
	return tickets, leases, err
}

func repositoryHealth(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = response.Write([]byte("ok\n"))
}

func (r *runningRepositoryTransport) Shutdown(ctx context.Context) error {
	if r == nil || r.server == nil {
		return nil
	}
	return r.server.Shutdown(ctx)
}
