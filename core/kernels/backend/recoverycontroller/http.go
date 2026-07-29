package recoverycontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	recoveryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/recovery/v1"
)

type Server struct {
	controller *Controller
	server     *http.Server
	listener   net.Listener
}

func StartServer(listen string, controller *Controller) (*Server, error) {
	if controller == nil {
		return nil, errors.New("Recovery HTTP Controller 未配置")
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return nil, errors.New("Recovery HTTP 监听地址无效")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("Recovery HTTP 只允许监听 loopback 地址")
	}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	item := &Server{controller: controller, listener: listener}
	mux.HandleFunc("/healthz", item.health)
	mux.HandleFunc("/readyz", item.ready)
	mux.HandleFunc("/v1/recovery/status", item.status)
	item.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	go func() { _ = item.server.Serve(listener) }()
	return item, nil
}

func (s *Server) Addr() string { return s.listener.Addr().String() }

func (s *Server) Close(ctx context.Context) error {
	if s == nil || s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

func (s *Server) health(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusOK)
}

func (s *Server) ready(response http.ResponseWriter, request *http.Request) {
	status, ok := s.readStatus(response, request)
	if !ok {
		return
	}
	if status.Overall == recoveryv1.OverallStarting || status.Overall == recoveryv1.UnitFailed {
		response.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	response.WriteHeader(http.StatusOK)
}

func (s *Server) status(response http.ResponseWriter, request *http.Request) {
	status, ok := s.readStatus(response, request)
	if !ok {
		return
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusOK)
	if request.Method != http.MethodHead {
		_ = json.NewEncoder(response).Encode(status)
	}
}

func (s *Server) readStatus(response http.ResponseWriter, request *http.Request) (recoveryv1.Status, bool) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		response.WriteHeader(http.StatusMethodNotAllowed)
		return recoveryv1.Status{}, false
	}
	response.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()
	status, err := s.controller.Status(ctx)
	if err != nil {
		http.Error(response, fmt.Sprintf("recovery status unavailable: %v", err), http.StatusServiceUnavailable)
		return recoveryv1.Status{}, false
	}
	return status, true
}
