// Package nodebootstrap defines the production boundary for enrolling a Linux
// host into a VastPlan cluster. SSH is used only for the initial hand-off;
// after systemd starts the Node Agent, normal Deployment v2 reconciliation
// happens over the authenticated control plane.
package nodebootstrap

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	InstallRoot       = "/opt/vastplan/backend"
	StateRoot         = "/var/lib/vastplan"
	ConfigRoot        = "/etc/vastplan"
	SecretsRoot       = ConfigRoot + "/secrets"
	SystemdUnitPath   = "/etc/systemd/system/vastplan-node-agent.service"
	ArtifactTokenFile = SecretsRoot + "/artifact.env"
	ServiceUser       = "vastplan"

	maxBootstrapSecretFiles      = 32
	maxBootstrapSecretTotalBytes = 16 << 20
)

var (
	identifierPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
	versionPattern    = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][a-zA-Z0-9._-]+)?$`)
	digestPattern     = regexp.MustCompile(`^[a-f0-9]{64}$`)
	labelPattern      = regexp.MustCompile(`^[a-zA-Z0-9._/-]+=[a-zA-Z0-9._/-]+(?:,[a-zA-Z0-9._/-]+=[a-zA-Z0-9._/-]+)*$`)
	secretNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
)

// Request contains only non-secret deployment intent. SecretFile.Source paths
// are read locally by the trusted bootstrap command and their contents travel
// through the encrypted SSH stream; they are never embedded in Deployment v2.
type Request struct {
	Target      Target       `json:"target"`
	Release     Release      `json:"release"`
	Node        NodeAgent    `json:"node"`
	SecretFiles []SecretFile `json:"secretFiles"`
}

type Target struct {
	Address string `json:"address"`
	Port    int    `json:"port,omitempty"`
	User    string `json:"user"`
}

type Release struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
}

type NodeAgent struct {
	ID              string `json:"id"`
	Tenant          string `json:"tenant"`
	Deployment      string `json:"deployment"`
	Labels          string `json:"labels,omitempty"`
	NATSURL         string `json:"natsUrl"`
	NATSCA          string `json:"natsCa"`
	NATSCert        string `json:"natsCert"`
	NATSKey         string `json:"natsKey"`
	NATSSeed        string `json:"natsSeed"`
	TransportSeed   string `json:"transportSeed"`
	TransportTrust  string `json:"transportTrust"`
	RepositoryURL   string `json:"repositoryUrl"`
	RepositoryCA    string `json:"repositoryCa,omitempty"`
	RepositoryTrust string `json:"repositoryTrust"`
	CapacityCPU     int64  `json:"capacityCpuMillis,omitempty"`
	CapacityMemory  int64  `json:"capacityMemoryBytes,omitempty"`
	CapacityGPU     int64  `json:"capacityGpu,omitempty"`
}

// SecretFile describes a local source and a fixed remote destination under
// /etc/vastplan/secrets. Mode is fixed to 0440: root owns the files and the
// dedicated VastPlan service group can read but cannot rewrite them.
type SecretFile struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        uint32 `json:"mode,omitempty"`
}

func (r Request) Validate() error {
	if err := r.Target.Validate(); err != nil {
		return err
	}
	if err := r.Release.Validate(); err != nil {
		return err
	}
	if err := r.Node.Validate(); err != nil {
		return err
	}
	if len(r.SecretFiles) > maxBootstrapSecretFiles {
		return fmt.Errorf("秘密文件不能超过 %d 个", maxBootstrapSecretFiles)
	}
	destinations := map[string]struct{}{}
	for i := range r.SecretFiles {
		if err := r.SecretFiles[i].Validate(); err != nil {
			return fmt.Errorf("secretFiles[%d]: %w", i, err)
		}
		if _, duplicate := destinations[r.SecretFiles[i].Destination]; duplicate {
			return fmt.Errorf("远端秘密文件目标重复: %s", r.SecretFiles[i].Destination)
		}
		destinations[r.SecretFiles[i].Destination] = struct{}{}
	}
	for _, required := range []string{r.Node.NATSCA, r.Node.NATSCert, r.Node.NATSKey, r.Node.NATSSeed, r.Node.TransportSeed, r.Node.TransportTrust, r.Node.RepositoryTrust, ArtifactTokenFile} {
		if _, ok := destinations[required]; !ok {
			return fmt.Errorf("缺少必须下发的秘密/信任文件: %s", required)
		}
	}
	if r.Node.RepositoryCA != "" {
		if _, ok := destinations[r.Node.RepositoryCA]; !ok {
			return fmt.Errorf("缺少制品仓库 CA 文件: %s", r.Node.RepositoryCA)
		}
	}
	return nil
}

func (t Target) Validate() error {
	if strings.TrimSpace(t.Address) == "" || strings.ContainsAny(t.Address, "\x00\r\n") {
		return errors.New("SSH 目标地址无效")
	}
	if ip := net.ParseIP(t.Address); ip == nil && !validDNSName(t.Address) {
		return errors.New("SSH 目标必须是 IP 或规范 DNS 名")
	}
	if t.Port < 0 || t.Port > 65535 {
		return errors.New("SSH 端口无效")
	}
	if !identifierPattern.MatchString(t.User) {
		return errors.New("SSH 用户名无效")
	}
	return nil
}

func (t Target) Endpoint() string {
	port := t.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(t.Address, fmt.Sprint(port))
}

func (r Release) Validate() error {
	if !versionPattern.MatchString(r.Version) {
		return errors.New("内核版本必须是不可变语义版本")
	}
	if err := secureURL(r.URL); err != nil {
		return fmt.Errorf("内核下载地址: %w", err)
	}
	if !digestPattern.MatchString(r.SHA256) {
		return errors.New("内核 SHA-256 必须是 64 位小写十六进制")
	}
	return nil
}

func (n NodeAgent) Validate() error {
	for name, value := range map[string]string{"node id": n.ID, "tenant": n.Tenant, "deployment": n.Deployment} {
		if !identifierPattern.MatchString(value) {
			return fmt.Errorf("%s 无效", name)
		}
	}
	if n.Labels != "" && !labelPattern.MatchString(n.Labels) {
		return errors.New("节点标签必须是逗号分隔的 key=value")
	}
	natsURL, err := url.Parse(n.NATSURL)
	if err != nil || natsURL.Scheme != "tls" || natsURL.Host == "" || natsURL.User != nil || natsURL.RawQuery != "" || natsURL.Fragment != "" {
		return errors.New("生产 Node Agent 必须使用不含凭证的 tls:// NATS URL")
	}
	if err := secureURL(n.RepositoryURL); err != nil {
		return fmt.Errorf("制品仓库地址: %w", err)
	}
	for name, value := range map[string]string{
		"natsCa": n.NATSCA, "natsCert": n.NATSCert, "natsKey": n.NATSKey, "natsSeed": n.NATSSeed,
		"transportSeed": n.TransportSeed, "transportTrust": n.TransportTrust, "repositoryTrust": n.RepositoryTrust,
	} {
		if err := secureRemotePath(value); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if n.RepositoryCA != "" {
		if err := secureRemotePath(n.RepositoryCA); err != nil {
			return fmt.Errorf("repositoryCa: %w", err)
		}
	}
	if n.CapacityCPU < 0 || n.CapacityMemory < 0 || n.CapacityGPU < 0 {
		return errors.New("节点容量不能为负数")
	}
	return nil
}

func (s *SecretFile) Validate() error {
	if !filepath.IsAbs(s.Source) || filepath.Clean(s.Source) != s.Source || strings.ContainsAny(s.Source, "\x00\r\n") {
		return errors.New("本地秘密文件 source 必须是规范绝对路径")
	}
	if err := secureRemotePath(s.Destination); err != nil {
		return err
	}
	if s.Mode == 0 {
		s.Mode = 0o440
	}
	if s.Mode != 0o440 {
		return errors.New("远端秘密文件 mode 必须是 0440")
	}
	return nil
}

func secureURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return errors.New("必须是无内嵌凭证的 https:// URL")
	}
	return nil
}

func secureRemotePath(value string) error {
	if !filepath.IsAbs(value) || filepath.Clean(value) != value || !strings.HasPrefix(value, SecretsRoot+"/") || strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("远端文件必须位于 %s", SecretsRoot)
	}
	name := strings.TrimPrefix(value, SecretsRoot+"/")
	if !secretNamePattern.MatchString(name) {
		return errors.New("远端秘密文件必须使用安全的单层文件名")
	}
	return nil
}

func validDNSName(value string) bool {
	if len(value) > 253 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}
