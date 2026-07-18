// Package protocollimit 定义 Backend Kernel 各协议面的统一资源边界。
//
// Host、插件 SDK 与跨节点 addressing 必须共享同一组默认值，避免调用链某一段
// 接受了另一段无法承载的请求。调用方可以按部署容量覆盖，但零值始终收敛到安全默认。
package protocollimit

import "time"

const (
	DefaultMaxPayloadBytes     = 4 << 20 // 4 MiB；更大数据应改走对象存储或分片流。
	DefaultMaxStreamFrameBytes = 1 << 20 // 1 MiB；让 HTTP/2 背压能及时生效。
	DefaultMaxMetadataBytes    = 16 << 10
	DefaultMaxConcurrentCalls  = 256
	DefaultMaxPendingRequests  = 512
	DefaultMaxCallDepth        = 16
	DefaultDeadline            = 30 * time.Second
	DefaultDrainTimeout        = 30 * time.Second
	protobufEnvelopeAllowance  = 256 << 10
)

// Limits 是协议入口必须共同执行的资源契约。字段为零或负数时使用默认值，
// 不能用零值关闭保护；需要扩大容量时应显式配置并通过容量测试。
type Limits struct {
	MaxPayloadBytes     int
	MaxStreamFrameBytes int
	MaxMetadataBytes    uint32
	MaxConcurrentCalls  int
	MaxPendingRequests  int
	MaxCallDepth        int
	DefaultDeadline     time.Duration
	DrainTimeout        time.Duration
}

// Default 返回生产安全默认值，便于配置层展示最终生效值。
func Default() Limits {
	return Limits{
		MaxPayloadBytes:     DefaultMaxPayloadBytes,
		MaxStreamFrameBytes: DefaultMaxStreamFrameBytes,
		MaxMetadataBytes:    DefaultMaxMetadataBytes,
		MaxConcurrentCalls:  DefaultMaxConcurrentCalls,
		MaxPendingRequests:  DefaultMaxPendingRequests,
		MaxCallDepth:        DefaultMaxCallDepth,
		DefaultDeadline:     DefaultDeadline,
		DrainTimeout:        DefaultDrainTimeout,
	}
}

// Normalize 把未配置字段补齐。它返回副本，不改变调用方持有的配置。
func (l Limits) Normalize() Limits {
	d := Default()
	if l.MaxPayloadBytes <= 0 {
		l.MaxPayloadBytes = d.MaxPayloadBytes
	}
	if l.MaxStreamFrameBytes <= 0 {
		l.MaxStreamFrameBytes = d.MaxStreamFrameBytes
	}
	if l.MaxMetadataBytes == 0 {
		l.MaxMetadataBytes = d.MaxMetadataBytes
	}
	if l.MaxConcurrentCalls <= 0 {
		l.MaxConcurrentCalls = d.MaxConcurrentCalls
	}
	if l.MaxPendingRequests <= 0 {
		l.MaxPendingRequests = d.MaxPendingRequests
	}
	if l.MaxCallDepth <= 0 {
		l.MaxCallDepth = d.MaxCallDepth
	}
	if l.DefaultDeadline <= 0 {
		l.DefaultDeadline = d.DefaultDeadline
	}
	if l.DrainTimeout <= 0 {
		l.DrainTimeout = d.DrainTimeout
	}
	return l
}

// MaxMessageBytes 是 gRPC 信封上限。payload 之外预留固定 protobuf 信封空间，
// 贡献声明等非业务消息也因此受到同一个硬边界保护。
func (l Limits) MaxMessageBytes() int {
	l = l.Normalize()
	maxBody := l.MaxPayloadBytes
	if l.MaxStreamFrameBytes > maxBody {
		maxBody = l.MaxStreamFrameBytes
	}
	return maxBody + protobufEnvelopeAllowance
}

func (l Limits) PayloadAllowed(payload []byte) bool {
	return len(payload) <= l.Normalize().MaxPayloadBytes
}

// MetadataAllowed 约束序列化后的 CallContext 或传输 metadata 大小。
func (l Limits) MetadataAllowed(serializedBytes int) bool {
	return serializedBytes <= int(l.Normalize().MaxMetadataBytes)
}

func (l Limits) StreamFrameAllowed(payload []byte) bool {
	return len(payload) <= l.Normalize().MaxStreamFrameBytes
}
