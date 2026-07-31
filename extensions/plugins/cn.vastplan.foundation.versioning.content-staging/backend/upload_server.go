package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
	staging "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.versioning.content-staging/contentstaging"
)

const (
	uploadTLSCertEnv = "VASTPLAN_CONTENT_UPLOAD_TLS_CERT"
	uploadTLSKeyEnv  = "VASTPLAN_CONTENT_UPLOAD_TLS_KEY"
)

type uploadTransport struct {
	server *http.Server
	ready  atomic.Bool
}

func startUploadTransport(configuration *staging.DataPlaneConfiguration, service *staging.Service, tickets *uploadTicketStore) (*uploadTransport, error) {
	if configuration == nil {
		return &uploadTransport{}, nil
	}
	certificateFile, keyFile := os.Getenv(uploadTLSCertEnv), os.Getenv(uploadTLSKeyEnv)
	if certificateFile == "" || keyFile == "" {
		return nil, errors.New("Content Upload HTTPS 数据面缺少 TLS 证书或私钥")
	}
	certificate, err := tls.LoadX509KeyPair(certificateFile, keyFile)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", configuration.Listen)
	if err != nil {
		return nil, err
	}
	tlsListener := tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13})
	transport := &uploadTransport{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusNoContent)
	})
	uploadHandler := newUploadHandler(service, tickets, newUploadCORSPolicy(configuration))
	mux.Handle("PUT /v1/uploads/{uploadId}", uploadHandler)
	mux.Handle("OPTIONS /v1/uploads/{uploadId}", uploadHandler)
	transport.server = &http.Server{
		Handler: mux, ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10,
	}
	transport.ready.Store(true)
	go func() {
		if err := transport.server.Serve(tlsListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Content Upload HTTPS 数据面退出: %v", err)
		}
		transport.ready.Store(false)
	}()
	return transport, nil
}

func (t *uploadTransport) Shutdown(ctx context.Context) error {
	if t == nil || t.server == nil {
		return nil
	}
	t.ready.Store(false)
	return t.server.Shutdown(ctx)
}

type uploadCORSPolicy map[string]struct{}

func newUploadCORSPolicy(configuration *staging.DataPlaneConfiguration) uploadCORSPolicy {
	policy := uploadCORSPolicy{}
	if configuration == nil {
		return policy
	}
	policy[configuration.Endpoint] = struct{}{}
	for _, origin := range configuration.AllowedBrowserOrigins {
		policy[origin] = struct{}{}
	}
	return policy
}

func (p uploadCORSPolicy) authorize(response http.ResponseWriter, request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return request.Method != http.MethodOptions
	}
	if _, allowed := p[origin]; !allowed {
		return false
	}
	response.Header().Set("Access-Control-Allow-Origin", origin)
	response.Header().Set("Vary", "Origin")
	if request.Method != http.MethodOptions {
		return true
	}
	if request.Header.Get("Access-Control-Request-Method") != http.MethodPut || !allowedCORSHeaders(request.Header.Get("Access-Control-Request-Headers")) {
		return false
	}
	response.Header().Set("Access-Control-Allow-Methods", http.MethodPut)
	response.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	response.Header().Set("Access-Control-Max-Age", "30")
	return true
}

func allowedCORSHeaders(value string) bool {
	for _, header := range strings.Split(value, ",") {
		header = strings.TrimSpace(strings.ToLower(header))
		if header != "" && header != "content-type" {
			return false
		}
	}
	return true
}

func newUploadHandler(service *staging.Service, tickets *uploadTicketStore, cors uploadCORSPolicy) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "private, no-store")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		if !cors.authorize(response, request) {
			http.Error(response, "origin forbidden", http.StatusForbidden)
			return
		}
		if request.Method == http.MethodOptions {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		if request.TLS == nil {
			http.Error(response, "https required", http.StatusUpgradeRequired)
			return
		}
		ticket, ok := tickets.consume(request)
		if !ok || ticket.uploadID != request.PathValue("uploadId") {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		scope := staging.Scope{TenantID: ticket.claims.TenantID, ActorID: "user:" + ticket.claims.PrincipalID}
		status, err := service.UploadStatus(request.Context(), scope, ticket.uploadID)
		if err != nil {
			writeUploadError(response, err)
			return
		}
		if request.ContentLength >= 0 && request.ContentLength != status.Upload.ExpectedSize {
			http.Error(response, "content length mismatch", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithDeadline(request.Context(), status.Upload.LeaseExpiresAt)
		defer cancel()
		result, err := service.StreamUpload(ctx, scope, ticket.uploadID, request.Body)
		if err != nil {
			writeUploadError(response, err)
			return
		}
		if result.Upload.State == stagingv1.StateRejected {
			writeUploadResult(response, http.StatusUnprocessableEntity, result)
			return
		}
		writeUploadResult(response, http.StatusOK, result)
	})
}

func writeUploadResult(response http.ResponseWriter, status int, result stagingv1.UploadStatusResult) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(result)
}

func writeUploadError(response http.ResponseWriter, err error) {
	code, _ := staging.ErrorDetails(err)
	status := http.StatusServiceUnavailable
	switch code {
	case stagingv1.ErrorInvalidRequest:
		status = http.StatusBadRequest
	case stagingv1.ErrorLeaseNotFound:
		status = http.StatusNotFound
	case stagingv1.ErrorLeaseExpired:
		status = http.StatusGone
	case stagingv1.ErrorLeaseConflict:
		status = http.StatusConflict
	case stagingv1.ErrorLimitExceeded:
		status = http.StatusRequestEntityTooLarge
	case stagingv1.ErrorDataIncomplete:
		status = http.StatusUnprocessableEntity
	}
	http.Error(response, http.StatusText(status), status)
}
