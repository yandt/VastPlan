package portaledgecommand

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type platformRouterOptions struct {
	URL, CA, Cert, Key, NKeySeed  string
	TransportSeed, TransportTrust string
	NodeID                        string
	AllowInsecure                 bool
}

func (o platformRouterOptions) validate() error {
	if o.URL == "" {
		return nil
	}
	if o.NodeID == "" {
		return errors.New("启用平台管理远端调用时 node-id 不能为空")
	}
	if (o.TransportSeed == "") != (o.TransportTrust == "") {
		return errors.New("平台管理 addressing 传输身份必须同时配置 seed 和 trust")
	}
	if !o.AllowInsecure && o.TransportSeed == "" {
		return errors.New("生产平台管理调用必须配置 addressing 传输身份")
	}
	return nil
}

type platformRouter struct {
	router     *addressing.Router
	connection *nats.Conn
	transport  *addressing.TransportSecurity
}

func newPlatformRouter(ctx context.Context, options platformRouterOptions, logf func(string, ...any)) (*platformRouter, error) {
	if err := options.validate(); err != nil {
		return nil, err
	}
	if options.URL == "" {
		return nil, nil
	}
	result := &platformRouter{}
	var err error
	if options.TransportSeed != "" {
		result.transport, err = addressing.LoadTransportSecurity(options.TransportSeed, options.TransportTrust)
		if err != nil {
			return nil, err
		}
	}
	result.connection, err = controlplane.ConnectWithConfig(controlplane.ConnectionConfig{
		URL: options.URL, ClientName: "vastplan-portal-edge-" + options.NodeID,
		CAFile: options.CA, CertFile: options.Cert, KeyFile: options.Key, SeedFile: options.NKeySeed,
		Insecure: options.AllowInsecure, Logf: logf,
	})
	if err != nil {
		result.Close()
		return nil, err
	}
	js, err := jetstream.New(result.connection)
	if err != nil {
		result.Close()
		return nil, fmt.Errorf("创建 Portal Edge JetStream 客户端: %w", err)
	}
	openCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	buckets, err := controlplane.OpenBuckets(openCtx, js)
	if err != nil {
		result.Close()
		return nil, err
	}
	if result.transport != nil {
		result.router, err = addressing.NewSecureRouter(result.connection, buckets.Capabilities, options.NodeID, logf, result.transport)
	} else {
		result.router, err = addressing.NewRouter(result.connection, buckets.Capabilities, options.NodeID, logf)
	}
	if err != nil {
		result.Close()
		return nil, fmt.Errorf("创建 Portal Edge capability router: %w", err)
	}
	return result, nil
}

func (r *platformRouter) Close() {
	if r == nil {
		return
	}
	if r.router != nil {
		_ = r.router.Close()
	}
	if r.connection != nil {
		r.connection.Close()
	}
	if r.transport != nil {
		r.transport.Close()
	}
}
