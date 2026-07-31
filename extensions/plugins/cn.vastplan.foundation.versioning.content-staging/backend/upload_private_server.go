package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
	staging "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.versioning.content-staging/contentstaging"
)

const (
	privateUploadTLSCertEnv  = "VASTPLAN_CONTENT_UPLOAD_PRIVATE_TLS_CERT"
	privateUploadTLSKeyEnv   = "VASTPLAN_CONTENT_UPLOAD_PRIVATE_TLS_KEY"
	privateUploadClientCAEnv = "VASTPLAN_CONTENT_UPLOAD_PRIVATE_CLIENT_CA"
)

func startPrivateUploadTransport(configuration *staging.DataPlaneConfiguration, service *staging.Service, tickets *uploadTicketStore) (*uploadTransport, error) {
	if configuration == nil || configuration.Private == nil {
		return &uploadTransport{}, nil
	}
	private := configuration.Private
	certificate, err := tls.LoadX509KeyPair(os.Getenv(privateUploadTLSCertEnv), os.Getenv(privateUploadTLSKeyEnv))
	if err != nil {
		return nil, errors.New("加载 Content Upload Private TLS 身份失败")
	}
	caPEM, err := os.ReadFile(os.Getenv(privateUploadClientCAEnv))
	if err != nil {
		return nil, errors.New("读取 Content Upload Private Client CA 失败")
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("Content Upload Private Client CA 无有效证书")
	}
	listener, err := net.Listen("tcp", private.Listen)
	if err != nil {
		return nil, err
	}
	tlsListener := tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{certificate}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientCAs, MinVersion: tls.VersionTLS13})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) })
	mux.Handle("PUT /v1/uploads/{uploadId}", requirePrivateClient(newUploadHandler(service, tickets, nil), private.ClientIdentityPrefixes))
	transport := &uploadTransport{server: &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10}}
	transport.ready.Store(true)
	go func() {
		if serveErr := transport.server.Serve(tlsListener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			transport.ready.Store(false)
		}
	}()
	return transport, nil
}

func requirePrivateClient(next http.Handler, prefixes []string) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !verifiedSPIFFEClient(request.TLS, prefixes) {
			http.Error(response, "mTLS workload identity required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func verifiedSPIFFEClient(state *tls.ConnectionState, prefixes []string) bool {
	if state == nil || len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 {
		return false
	}
	for _, identity := range state.VerifiedChains[0][0].URIs {
		value := identity.String()
		if identity.Scheme != "spiffe" {
			continue
		}
		for _, prefix := range prefixes {
			if value != prefix && strings.HasPrefix(value, prefix) {
				return true
			}
		}
	}
	return false
}

func newPrivateTicketStore(configuration staging.DataPlaneConfiguration, service *staging.Service) *uploadTicketStore {
	if configuration.Private == nil {
		return nil
	}
	return newModeUploadTicketStore(configuration, configuration.Private.InstanceID, apiv1.ModePrivateDirect, service)
}

func shutdownUploadTransports(ctx context.Context, transports ...*uploadTransport) error {
	var result error
	for _, transport := range transports {
		result = errors.Join(result, transport.Shutdown(ctx))
	}
	return result
}
