// autoscalingmetric 把监控系统采集值写入 VastPlan 的短租约自动伸缩指标入口。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"cdsoft.com.cn/VastPlan/shared/go/controlplane"
)

func main() {
	natsURL := flag.String("nats-url", "tls://127.0.0.1:4222", "NATS URL")
	natsCA := flag.String("nats-ca", "", "NATS CA PEM")
	natsCert := flag.String("nats-cert", "", "NATS mTLS 客户端证书 PEM")
	natsKey := flag.String("nats-key", "", "NATS mTLS 客户端私钥 PEM")
	natsSeed := flag.String("nats-seed", "", "runtime 角色 NKey seed（0600）")
	natsAllowInsecure := flag.Bool("nats-allow-insecure", false, "仅本地开发：允许明文匿名 NATS")
	tenant := flag.String("tenant", "", "部署租户")
	deployment := flag.String("deployment", "", "Deployment 名称")
	unit := flag.String("unit", "", "service unit id")
	metric := flag.String("metric", "", "指标名，须与 autoscaling.metric 一致")
	value := flag.Float64("value", -1, "当前非负指标值")
	flag.Parse()

	nc, err := controlplane.ConnectWithConfig(controlplane.ConnectionConfig{
		URL: *natsURL, ClientName: "vastplan-autoscaling-metric",
		CAFile: *natsCA, CertFile: *natsCert, KeyFile: *natsKey, SeedFile: *natsSeed,
		Insecure: *natsAllowInsecure,
	})
	if err != nil {
		fatalf("连接 NATS: %v", err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		fatalf("创建 JetStream 客户端: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	kv, err := js.KeyValue(ctx, controlplane.AutoscalingBucket)
	if err != nil {
		fatalf("打开自动伸缩指标 bucket: %v", err)
	}
	if err := controlplane.PublishAutoscalingMetric(ctx, kv, controlplane.AutoscalingMetric{
		Tenant: *tenant, Deployment: *deployment, Unit: *unit, Metric: *metric, Value: *value,
	}); err != nil {
		fatalf("%v", err)
	}
	fmt.Printf("已发布自动伸缩指标 deployment=%s unit=%s metric=%s value=%g\n", *deployment, *unit, *metric, *value)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
