// Package controlplane 定义 VastPlan NATS 控制面的稳定 bucket、key 与连接约定。
//
// 本包只管理控制面基础设施，不承载业务数据；DesiredState 自身的 revision/digest
// 仍是配置冲突真源，不能把 JetStream KV 当关系型事务数据库使用。
package controlplane

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	DesiredBucket      = "VASTPLAN_DESIRED_V1"
	ActualBucket       = "VASTPLAN_ACTUAL_V1"
	NodesBucket        = "VASTPLAN_NODES_V1"
	CapabilitiesBucket = "VASTPLAN_CAPABILITIES_V1"
	DeploymentsBucket  = "VASTPLAN_DEPLOYMENTS_V2"
	AssignmentsBucket  = "VASTPLAN_ASSIGNMENTS_V1"
	ControllersBucket  = "VASTPLAN_CONTROLLERS_V1"
	AutoscalingBucket  = "VASTPLAN_AUTOSCALING_V1"
	EventsStream       = "VASTPLAN_EVENTS_V1"

	MaxDesiredStateBytes = 1 << 20
	ActualStateHistory   = 16
)

// Buckets 集中返回控制面的版本化 KV 句柄，避免组件各自拼 bucket 名。
type Buckets struct {
	Desired      jetstream.KeyValue
	Actual       jetstream.KeyValue
	Nodes        jetstream.KeyValue
	Capabilities jetstream.KeyValue
	Deployments  jetstream.KeyValue
	Assignments  jetstream.KeyValue
	Controllers  jetstream.KeyValue
	Autoscaling  jetstream.KeyValue
	Events       jetstream.Stream
}

// Connect 建立可无限重连的 NATS 连接。首次连接仍 fail-fast，让启动配置错误明确暴露；
// 已运行后的短暂断线由客户端恢复订阅和 KV watcher。
func Connect(url, clientName string, logf func(string, ...any)) (*nats.Conn, error) {
	return ConnectWithConfig(ConnectionConfig{
		URL: url, ClientName: clientName, Insecure: true, Logf: logf,
	})
}

// EnsureBuckets 创建或校准控制面 bucket。生产集群 replicas 应至少为 3；本地和 E2E
// 可显式传 1。节点租约使用短 TTL；实际态保留最近 16 个生命周期检查点，期望态
// 保留 64 份历史。两者均有硬上限，不能把 KV 误用成无限增长的审计日志。
func EnsureBuckets(ctx context.Context, js jetstream.JetStream, replicas int, storage jetstream.StorageType) (Buckets, error) {
	if replicas <= 0 {
		replicas = 1
	}
	desired, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: DesiredBucket, Description: "VastPlan DesiredState v1",
		History: 64, MaxValueSize: MaxDesiredStateBytes, Replicas: replicas, Storage: storage,
	})
	if err != nil {
		return Buckets{}, fmt.Errorf("创建期望态 bucket: %w", err)
	}
	actual, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: ActualBucket, Description: "VastPlan node actual state v2",
		History: ActualStateHistory, MaxValueSize: MaxDesiredStateBytes, Replicas: replicas, Storage: storage,
	})
	if err != nil {
		return Buckets{}, fmt.Errorf("创建实际态 bucket: %w", err)
	}
	nodes, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: NodesBucket, Description: "VastPlan node leases v1",
		History: 1, TTL: 30 * time.Second, LimitMarkerTTL: time.Minute,
		MaxValueSize: 64 << 10, Replicas: replicas, Storage: storage,
	})
	if err != nil {
		return Buckets{}, fmt.Errorf("创建节点 bucket: %w", err)
	}
	capabilities, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: CapabilitiesBucket, Description: "VastPlan capability leases v1",
		History: 1, TTL: 30 * time.Second, LimitMarkerTTL: time.Minute,
		MaxValueSize: 64 << 10, Replicas: replicas, Storage: storage,
	})
	if err != nil {
		return Buckets{}, fmt.Errorf("创建能力目录 bucket: %w", err)
	}
	deployments, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: DeploymentsBucket, Description: "VastPlan cluster deployment v2",
		History: 64, MaxValueSize: MaxDesiredStateBytes, Replicas: replicas, Storage: storage,
	})
	if err != nil {
		return Buckets{}, fmt.Errorf("创建集群部署 bucket: %w", err)
	}
	assignments, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: AssignmentsBucket, Description: "VastPlan per-node desired assignments v1",
		History: 8, MaxValueSize: MaxDesiredStateBytes, Replicas: replicas, Storage: storage,
	})
	if err != nil {
		return Buckets{}, fmt.Errorf("创建节点分配 bucket: %w", err)
	}
	controllers, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: ControllersBucket, Description: "VastPlan controller leader leases v1",
		History: 1, TTL: 15 * time.Second, LimitMarkerTTL: time.Minute,
		MaxValueSize: 16 << 10, Replicas: replicas, Storage: storage,
	})
	if err != nil {
		return Buckets{}, fmt.Errorf("创建控制器选主 bucket: %w", err)
	}
	autoscaling, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: AutoscalingBucket, Description: "VastPlan autoscaling metrics v1",
		History: 1, TTL: AutoscalingMetricMaxAge, LimitMarkerTTL: time.Minute,
		MaxValueSize: 16 << 10, Replicas: replicas, Storage: storage,
	})
	if err != nil {
		return Buckets{}, fmt.Errorf("创建自动伸缩指标 bucket: %w", err)
	}
	events, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: EventsStream, Description: "VastPlan durable domain events v1",
		Subjects: []string{"vp.event.persist.v1.>"}, Retention: jetstream.LimitsPolicy,
		MaxAge: 7 * 24 * time.Hour, MaxBytes: 10 << 30, Discard: jetstream.DiscardOld,
		Duplicates: 10 * time.Minute, Replicas: replicas, Storage: storage,
	})
	if err != nil {
		return Buckets{}, fmt.Errorf("创建持久事件 stream: %w", err)
	}
	return Buckets{
		Desired: desired, Actual: actual, Nodes: nodes, Capabilities: capabilities,
		Deployments: deployments, Assignments: assignments, Controllers: controllers, Autoscaling: autoscaling, Events: events,
	}, nil
}

// OpenBuckets 打开已经由控制面管理员创建的 bucket。Node Agent 正常运行路径只需
// 读写 key，不应默认拥有修改 stream 配置的权限。
func OpenBuckets(ctx context.Context, js jetstream.JetStream) (Buckets, error) {
	desired, err := js.KeyValue(ctx, DesiredBucket)
	if err != nil {
		return Buckets{}, fmt.Errorf("打开期望态 bucket: %w", err)
	}
	actual, err := js.KeyValue(ctx, ActualBucket)
	if err != nil {
		return Buckets{}, fmt.Errorf("打开实际态 bucket: %w", err)
	}
	nodes, err := js.KeyValue(ctx, NodesBucket)
	if err != nil {
		return Buckets{}, fmt.Errorf("打开节点 bucket: %w", err)
	}
	capabilities, err := js.KeyValue(ctx, CapabilitiesBucket)
	if err != nil {
		return Buckets{}, fmt.Errorf("打开能力目录 bucket: %w", err)
	}
	deployments, err := js.KeyValue(ctx, DeploymentsBucket)
	if err != nil {
		return Buckets{}, fmt.Errorf("打开集群部署 bucket: %w", err)
	}
	assignments, err := js.KeyValue(ctx, AssignmentsBucket)
	if err != nil {
		return Buckets{}, fmt.Errorf("打开节点分配 bucket: %w", err)
	}
	controllers, err := js.KeyValue(ctx, ControllersBucket)
	if err != nil {
		return Buckets{}, fmt.Errorf("打开控制器选主 bucket: %w", err)
	}
	autoscaling, err := js.KeyValue(ctx, AutoscalingBucket)
	if err != nil {
		return Buckets{}, fmt.Errorf("打开自动伸缩指标 bucket: %w", err)
	}
	events, err := js.Stream(ctx, EventsStream)
	if err != nil {
		return Buckets{}, fmt.Errorf("打开持久事件 stream: %w", err)
	}
	return Buckets{
		Desired: desired, Actual: actual, Nodes: nodes, Capabilities: capabilities,
		Deployments: deployments, Assignments: assignments, Controllers: controllers, Autoscaling: autoscaling, Events: events,
	}, nil
}

// DesiredKey 按 tenant/name 生成稳定层级 key；编码避免租户名中的点或空格改变层级。
func DesiredKey(tenant, name string) string {
	if tenant == "" {
		tenant = "_global"
	}
	return "tenants." + keyToken(tenant) + ".states." + keyToken(name)
}

func DeploymentKey(tenant, name string) string { return DesiredKey(tenant, name) }

func AssignmentPrefix(tenant, name string) string {
	return DeploymentKey(tenant, name) + ".nodes."
}

func AssignmentKey(tenant, name, nodeID string) string {
	return AssignmentPrefix(tenant, name) + keyToken(nodeID)
}

func ScheduleKey(tenant, name string) string {
	return DeploymentKey(tenant, name) + ".schedule"
}

func AutoscalingMetricKey(tenant, deployment, unit, metric string) string {
	return "tenants." + keyToken(tenant) + ".deployments." + keyToken(deployment) + ".units." + keyToken(unit) + ".metrics." + keyToken(metric)
}

func AssignmentNodeID(tenant, name, key string) (string, error) {
	prefix := AssignmentPrefix(tenant, name)
	if !strings.HasPrefix(key, prefix) {
		return "", fmt.Errorf("assignment key %q 不属于 %s/%s", key, tenant, name)
	}
	return AssignmentKeyNodeID(key)
}

func AssignmentKeyNodeID(key string) (string, error) {
	token := key[strings.LastIndex(key, ".")+1:]
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) == 0 {
		return "", fmt.Errorf("assignment key %q 的 node id 非法", key)
	}
	return string(raw), nil
}

func ActualKey(nodeID string) string { return "nodes." + keyToken(nodeID) }
func NodeKey(nodeID string) string   { return "nodes." + keyToken(nodeID) }

func CapabilityKey(capability, instanceID string) string {
	return "capabilities." + keyToken(capability) + "." + keyToken(instanceID)
}

func RPCSubject(capability string) string { return "vp.rpc.v1." + keyToken(capability) }
func RPCQueue(capability string) string   { return "vp.rpc.v1." + keyToken(capability) }
func RPCSubjectFor(capability, logicalService, routingDomain string) string {
	if logicalService == "" && routingDomain == "" {
		return RPCSubject(capability)
	}
	return "vp.rpc.v1." + keyToken(logicalService) + "." + keyToken(capability) + "." + keyToken(routingDomain)
}
func RPCQueueFor(capability, logicalService, routingDomain string) string {
	return RPCSubjectFor(capability, logicalService, routingDomain)
}
func RPCSubjectForPartition(capability, logicalService, routingDomain, partitionKey string) string {
	if partitionKey == "" {
		return RPCSubjectFor(capability, logicalService, routingDomain)
	}
	return RPCSubjectFor(capability, logicalService, routingDomain+"/partition/"+partitionKey)
}
func RPCQueueForPartition(capability, logicalService, routingDomain, partitionKey string) string {
	return RPCSubjectForPartition(capability, logicalService, routingDomain, partitionKey)
}
func EventSubject(eventType string) string { return "vp.event.v1." + keyToken(eventType) }
func PersistentEventSubject(eventType string) string {
	return "vp.event.persist.v1." + keyToken(eventType)
}

func keyToken(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}
