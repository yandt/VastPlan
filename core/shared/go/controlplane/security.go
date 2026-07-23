package controlplane

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

// ConnectionConfig 是生产 NATS 身份的完整输入。TLS 保护传输与服务端身份，客户端证书
// 提供设备/工作负载身份，NKey 再映射到账号内的最小 Subject 权限。
type ConnectionConfig struct {
	URL        string
	ClientName string
	CAFile     string
	CertFile   string
	KeyFile    string
	SeedFile   string
	Insecure   bool // 只允许本地开发和测试显式开启。
	Logf       func(string, ...any)
}

// ConnectWithConfig 建立生产安全连接；除非 Insecure 显式开启，否则缺少 mTLS 或 NKey
// 任一要素都会 fail-closed，避免“配置了一半安全参数”却静默退回明文匿名连接。
func ConnectWithConfig(config ConnectionConfig) (*nats.Conn, error) {
	if strings.TrimSpace(config.URL) == "" {
		return nil, errors.New("NATS URL 不能为空")
	}
	options := baseNATSOptions(config.ClientName, config.Logf)
	if config.Insecure {
		if config.CAFile != "" || config.CertFile != "" || config.KeyFile != "" || config.SeedFile != "" {
			return nil, errors.New("-nats-allow-insecure 不能与 TLS/NKey 安全参数混用")
		}
	} else {
		if config.CAFile == "" || config.CertFile == "" || config.KeyFile == "" || config.SeedFile == "" {
			return nil, errors.New("生产 NATS 连接必须同时配置 CA、客户端证书、客户端私钥和 NKey seed")
		}
		if !strings.HasPrefix(config.URL, "tls://") {
			return nil, errors.New("生产 NATS URL 必须使用 tls://")
		}
		tlsConfig, err := loadNATSTLSConfig(config.CAFile, config.CertFile, config.KeyFile)
		if err != nil {
			return nil, err
		}
		seedInfo, err := os.Stat(config.SeedFile)
		if err != nil {
			return nil, fmt.Errorf("读取 NATS NKey seed 属性: %w", err)
		}
		if seedInfo.Mode().Perm()&0o077 != 0 {
			return nil, fmt.Errorf("NATS NKey seed 权限过宽 %o，要求 0600 或更严格", seedInfo.Mode().Perm())
		}
		nkeyOption, err := nats.NkeyOptionFromSeed(config.SeedFile)
		if err != nil {
			return nil, fmt.Errorf("加载 NATS NKey seed: %w", err)
		}
		options = append(options, nats.Secure(tlsConfig), nkeyOption)
	}
	nc, err := nats.Connect(config.URL, options...)
	if err != nil {
		return nil, fmt.Errorf("连接 NATS %s: %w", config.URL, err)
	}
	return nc, nil
}

func baseNATSOptions(clientName string, logf func(string, ...any)) []nats.Option {
	if clientName == "" {
		clientName = "vastplan"
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return []nats.Option{
		nats.Name(clientName), nats.Timeout(5 * time.Second), nats.MaxReconnects(-1),
		nats.ReconnectWait(500 * time.Millisecond),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) { logf("NATS 已断开: %v", err) }),
		nats.ReconnectHandler(func(conn *nats.Conn) { logf("NATS 已重连 %s", conn.ConnectedUrl()) }),
		nats.ClosedHandler(func(conn *nats.Conn) { logf("NATS 连接已关闭: %v", conn.LastError()) }),
	}
}

func loadNATSTLSConfig(caFile, certFile, keyFile string) (*tls.Config, error) {
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("读取 NATS CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("NATS CA PEM 不包含有效证书")
	}
	keyInfo, err := os.Stat(keyFile)
	if err != nil {
		return nil, fmt.Errorf("读取 NATS 客户端私钥属性: %w", err)
	}
	if keyInfo.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("NATS 客户端私钥权限过宽 %o，要求 0600 或更严格", keyInfo.Mode().Perm())
	}
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("加载 NATS 客户端证书: %w", err)
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS13, RootCAs: roots,
		Certificates: []tls.Certificate{certificate},
	}, nil
}

// SecurityRole 对应一个 NKey 用户的最小职责，不等同于业务租户。
type SecurityRole string

const (
	RoleBootstrap        SecurityRole = "bootstrap"
	RoleCatalogPublisher SecurityRole = "catalog-publisher"
	RoleController       SecurityRole = "controller"
	RoleNode             SecurityRole = "node"
	RoleManager          SecurityRole = "manager-node"
	RoleRuntime          SecurityRole = "runtime"
)

type SubjectACL struct {
	PublishAllow   []string
	PublishDeny    []string
	SubscribeAllow []string
	SubscribeDeny  []string
}

// RoleACL 是代码中的权限单一真相源，配置生成和测试都引用同一份策略。
func RoleACL(role SecurityRole) (SubjectACL, error) {
	return roleACL(role, "", "", "", "")
}

// RoleACLForIdentity 在生成服务端配置时把节点身份绑定到自己的状态 key。
// Node 角色若没有 NodeID 不允许生成生产 ACL；无节点范围的 RoleACL 仅供策略检查，
// 不得直接用于 NATS 用户配置。
func RoleACLForIdentity(identity NKeyIdentity) (SubjectACL, error) {
	return roleACL(identity.Role, identity.TenantID, identity.Deployment, identity.NodeID, identity.CatalogID)
}

func roleACL(role SecurityRole, tenant, deployment, nodeID, catalogID string) (SubjectACL, error) {
	switch role {
	case RoleBootstrap:
		return SubjectACL{PublishAllow: []string{">"}, SubscribeAllow: []string{">"}}, nil
	case RoleCatalogPublisher:
		if strings.TrimSpace(catalogID) == "" {
			return SubjectACL{}, errors.New("catalog-publisher NATS ACL 必须绑定 catalog id")
		}
		key := BackendPlatformCatalogKey(catalogID)
		stream := "KV_" + BackendPlatformCatalogsBucket
		return SubjectACL{
			PublishAllow: []string{
				"$JS.API.STREAM.INFO." + stream,
				"$JS.API.DIRECT.GET." + stream + "." + key,
				"$KV." + BackendPlatformCatalogsBucket + "." + key,
			},
			SubscribeAllow: []string{"_INBOX.>"},
		}, nil
	case RoleController:
		return SubjectACL{
			PublishAllow: append(openAllAPI(), append(
				kvAPIForRead(DeploymentsBucket, NodesBucket, ActualBucket, AssignmentsBucket, CompositionsBucket, ControllersBucket, AutoscalingBucket, ConfigurationAuthoritiesBucket, BackendPlatformCatalogsBucket),
				"$KV."+AssignmentsBucket+".>", "$KV."+CompositionsBucket+".>", "$KV."+ControllersBucket+".>",
			)...),
			SubscribeAllow: []string{
				"_INBOX.>", "$KV." + DeploymentsBucket + ".>", "$KV." + NodesBucket + ".>", "$KV." + ActualBucket + ".>",
				"$KV." + AssignmentsBucket + ".>", "$KV." + CompositionsBucket + ".>", "$KV." + ControllersBucket + ".>", "$KV." + AutoscalingBucket + ".>",
			},
		}, nil
	case RoleNode, RoleManager:
		if nodeID == "" || tenant == "" || deployment == "" {
			return SubjectACL{}, errors.New("node NATS ACL 必须绑定 tenant、deployment 与 node id")
		}
		acl := SubjectACL{
			PublishAllow: append(openAllAPI(), append(
				kvAPIForRead(DesiredBucket, DeploymentsBucket, AssignmentsBucket, CapabilitiesBucket, ConfigurationAuthoritiesBucket, SharedStateBucket),
				"$KV."+ActualBucket+"."+ActualKey(tenant, deployment, nodeID), "$KV."+NodesBucket+"."+NodeKey(tenant, deployment, nodeID),
				"$KV."+CapabilitiesBucket+".>", "vp.rpc.v1.>", "vp.rpc.cancel.v1", "vp.event.v1.>",
				"$KV."+ConfigurationAuthoritiesBucket+"."+ConfigurationAuthorityPrefix(tenant)+">",
				"$KV."+SharedStateBucket+".>",
				"vp.event.persist.v1.>",
				"$JS.API.CONSUMER.CREATE."+EventsStream, "$JS.API.CONSUMER.CREATE."+EventsStream+".>",
				"$JS.API.CONSUMER.DURABLE.CREATE."+EventsStream+".>", "$JS.API.CONSUMER.INFO."+EventsStream+".>",
				"$JS.API.CONSUMER.MSG.NEXT."+EventsStream+".>",
			)...),
			PublishDeny: []string{
				"$KV." + DesiredBucket + ".>", "$KV." + DeploymentsBucket + ".>", "$KV." + AssignmentsBucket + ".>",
			},
			SubscribeAllow: []string{
				"_INBOX.>", "$KV." + DesiredBucket + ".>", "$KV." + DeploymentsBucket + ".>", "$KV." + AssignmentsBucket + ".>",
				"$KV." + CapabilitiesBucket + ".>", "$KV." + ConfigurationAuthoritiesBucket + ".>", "$KV." + SharedStateBucket + ".>", "vp.rpc.v1.>", "vp.rpc.cancel.v1", "vp.event.v1.>",
			},
		}
		if role == RoleManager {
			acl.PublishAllow = append(acl.PublishAllow, kvAPIForRead(NodesBucket, BackendPlatformCatalogsBucket)...)
			acl.SubscribeAllow = append(acl.SubscribeAllow, "$KV."+BackendPlatformCatalogsBucket+".>")
		}
		return acl, nil
	case RoleRuntime:
		return SubjectACL{
			PublishAllow: append(append(kvAPIForRead(CapabilitiesBucket), "$JS.API.STREAM.INFO."+EventsStream, "$JS.API.STREAM.INFO.KV_"+AutoscalingBucket),
				"$KV."+CapabilitiesBucket+".>", "vp.rpc.v1.>", "vp.rpc.cancel.v1", "vp.event.v1.>",
				"$KV."+AutoscalingBucket+".>",
				"vp.event.persist.v1.>",
				"$JS.API.CONSUMER.CREATE."+EventsStream, "$JS.API.CONSUMER.CREATE."+EventsStream+".>",
				"$JS.API.CONSUMER.DURABLE.CREATE."+EventsStream+".>", "$JS.API.CONSUMER.INFO."+EventsStream+".>",
				"$JS.API.CONSUMER.MSG.NEXT."+EventsStream+".>"),
			SubscribeAllow: []string{
				"_INBOX.>", "$KV." + CapabilitiesBucket + ".>", "vp.rpc.v1.>", "vp.rpc.cancel.v1", "vp.event.v1.>",
			},
		}, nil
	default:
		return SubjectACL{}, fmt.Errorf("未知 NATS 安全角色 %q", role)
	}
}

func openAllAPI() []string {
	return append(kvAPIForInfo(DesiredBucket, ActualBucket, NodesBucket, CapabilitiesBucket, DeploymentsBucket, AssignmentsBucket, CompositionsBucket, ControllersBucket, AutoscalingBucket, ConfigurationAuthoritiesBucket, BackendPlatformCatalogsBucket, SharedStateBucket), "$JS.API.STREAM.INFO."+EventsStream)
}

func kvAPIForInfo(buckets ...string) []string {
	patterns := make([]string, 0, len(buckets))
	for _, bucket := range buckets {
		patterns = append(patterns, "$JS.API.STREAM.INFO.KV_"+bucket)
	}
	return patterns
}

func kvAPIForRead(buckets ...string) []string {
	patterns := make([]string, 0, len(buckets)*8)
	for _, bucket := range buckets {
		stream := "KV_" + bucket
		patterns = append(patterns,
			"$JS.API.STREAM.INFO."+stream,
			"$JS.API.DIRECT.GET."+stream+".>",
			"$JS.API.STREAM.MSG.GET."+stream,
			"$JS.API.CONSUMER.CREATE."+stream,
			"$JS.API.CONSUMER.CREATE."+stream+".>",
			"$JS.API.CONSUMER.DURABLE.CREATE."+stream+".>",
			"$JS.API.CONSUMER.INFO."+stream+".>",
			"$JS.API.CONSUMER.MSG.NEXT."+stream+".>",
		)
	}
	return patterns
}

type NKeyIdentity struct {
	Name       string
	Role       SecurityRole
	PublicKey  string
	TenantID   string
	Deployment string
	NodeID     string
	CatalogID  string
}

type ServerSecurityConfig struct {
	ServerName      string
	Listen          string
	StoreDir        string
	TLSCertFile     string
	TLSKeyFile      string
	TLSCAFile       string
	SystemPublicKey string
	Identities      []NKeyIdentity
}

// RenderNATSServerConfig 生成静态账号配置。输出只含公钥，不包含任何 seed。
func RenderNATSServerConfig(config ServerSecurityConfig) (string, error) {
	if config.ServerName == "" {
		config.ServerName = "vastplan-controlplane"
	}
	if config.Listen == "" {
		config.Listen = "0.0.0.0:4222"
	}
	for name, value := range map[string]string{
		"store_dir": config.StoreDir, "tls.cert_file": config.TLSCertFile,
		"tls.key_file": config.TLSKeyFile, "tls.ca_file": config.TLSCAFile,
	} {
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("NATS 安全配置缺少 %s", name)
		}
	}
	if !nkeys.IsValidPublicUserKey(config.SystemPublicKey) {
		return "", errors.New("NATS system user 公钥无效")
	}
	if len(config.Identities) == 0 {
		return "", errors.New("NATS 安全配置至少需要一个 VastPlan 身份")
	}
	sort.Slice(config.Identities, func(i, j int) bool {
		if config.Identities[i].Role != config.Identities[j].Role {
			return config.Identities[i].Role < config.Identities[j].Role
		}
		if config.Identities[i].Name != config.Identities[j].Name {
			return config.Identities[i].Name < config.Identities[j].Name
		}
		return config.Identities[i].PublicKey < config.Identities[j].PublicKey
	})
	seenPublicKeys := map[string]struct{}{}
	seenNodeIDs := map[string]struct{}{}
	var users strings.Builder
	for index, identity := range config.Identities {
		if _, exists := seenPublicKeys[identity.PublicKey]; exists {
			return "", fmt.Errorf("NATS 用户公钥重复: %s", identity.PublicKey)
		}
		seenPublicKeys[identity.PublicKey] = struct{}{}
		if !nkeys.IsValidPublicUserKey(identity.PublicKey) {
			return "", fmt.Errorf("NATS %s 用户公钥无效", identity.Role)
		}
		if identity.Role == RoleNode || identity.Role == RoleManager {
			if _, exists := seenNodeIDs[identity.NodeID]; exists {
				return "", fmt.Errorf("node id 必须在控制面全局唯一: %s", identity.NodeID)
			}
			seenNodeIDs[identity.NodeID] = struct{}{}
		}
		acl, err := RoleACLForIdentity(identity)
		if err != nil {
			return "", err
		}
		if index > 0 {
			users.WriteString(",\n")
		}
		fmt.Fprintf(&users, "      { nkey: %s, permissions: %s }", quote(identity.PublicKey), renderACL(acl))
	}
	return fmt.Sprintf(`server_name: %s
listen: %s
jetstream { store_dir: %s }
tls {
  cert_file: %s
  key_file: %s
  ca_file: %s
  verify: true
  timeout: 5
}
accounts {
  SYS { users: [ { nkey: %s } ] }
  VASTPLAN {
    jetstream: enabled
    users: [
%s
    ]
  }
}
system_account: SYS
`, quote(config.ServerName), quote(config.Listen), quote(filepath.Clean(config.StoreDir)),
		quote(filepath.Clean(config.TLSCertFile)), quote(filepath.Clean(config.TLSKeyFile)), quote(filepath.Clean(config.TLSCAFile)),
		quote(config.SystemPublicKey), users.String()), nil
}

func renderACL(acl SubjectACL) string {
	return fmt.Sprintf("{ publish: { allow: %s, deny: %s }, subscribe: { allow: %s, deny: %s } }",
		renderStrings(acl.PublishAllow), renderStrings(acl.PublishDeny),
		renderStrings(acl.SubscribeAllow), renderStrings(acl.SubscribeDeny))
}

func renderStrings(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = quote(value)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func quote(value string) string { return strconv.Quote(value) }
