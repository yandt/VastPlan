package main

import (
	"flag"

	"github.com/nats-io/nats.go"

	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

type natsFlags struct {
	url, ca, cert, key, seed string
	insecure                 bool
}

func addNATSFlags(flags *flag.FlagSet) *natsFlags {
	value := &natsFlags{}
	flags.StringVar(&value.url, "nats-url", "", "NATS URL；生产必须使用 tls://")
	flags.StringVar(&value.ca, "nats-ca", "", "NATS CA PEM")
	flags.StringVar(&value.cert, "nats-cert", "", "NATS mTLS 客户端证书 PEM")
	flags.StringVar(&value.key, "nats-key", "", "NATS mTLS 客户端私钥 PEM")
	flags.StringVar(&value.seed, "nats-seed", "", "Shared State backup 或 restore 角色 NKey seed")
	flags.BoolVar(&value.insecure, "nats-allow-insecure", false, "仅本地测试允许明文匿名 NATS")
	return value
}

func (value *natsFlags) connect(clientName string) (*nats.Conn, error) {
	return controlplane.ConnectWithConfig(controlplane.ConnectionConfig{
		URL: value.url, ClientName: clientName, CAFile: value.ca, CertFile: value.cert,
		KeyFile: value.key, SeedFile: value.seed, Insecure: value.insecure,
	})
}
