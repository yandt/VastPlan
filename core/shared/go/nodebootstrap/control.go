package nodebootstrap

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	KernelService             = "kernel.node.bootstrap"
	DeploymentManagerPluginID = "com.vastplan.platform.infrastructure.deployment-manager"
)

var credentialNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,159}$`)

// CredentialSecretFile maps a credential reference to one fixed remote file.
// The referenced material is resolved only inside a trusted Broker callback.
type CredentialSecretFile struct {
	Credential  string `json:"credential"`
	Destination string `json:"destination"`
	Mode        uint32 `json:"mode,omitempty"`
}

// Plan is the non-secret control-plane contract persisted by deployment-manager.
type Plan struct {
	Target                  Target                 `json:"target"`
	Release                 Release                `json:"release"`
	Node                    NodeAgent              `json:"node"`
	SSHIdentityCredential   string                 `json:"sshIdentityCredential"`
	SSHKnownHostsCredential string                 `json:"sshKnownHostsCredential"`
	SecretFiles             []CredentialSecretFile `json:"secretFiles"`
}

func (p Plan) Validate() error {
	if err := p.Target.Validate(); err != nil {
		return err
	}
	if err := p.Release.Validate(); err != nil {
		return err
	}
	if err := p.Node.Validate(); err != nil {
		return err
	}
	canonical := map[string]string{
		"natsCa": p.Node.NATSCA, "natsCert": p.Node.NATSCert, "natsKey": p.Node.NATSKey, "natsSeed": p.Node.NATSSeed,
		"transportSeed": p.Node.TransportSeed, "transportTrust": p.Node.TransportTrust, "repositoryTrust": p.Node.RepositoryTrust,
	}
	expected := map[string]string{
		"natsCa": SecretsRoot + "/nats-ca.pem", "natsCert": SecretsRoot + "/node.crt", "natsKey": SecretsRoot + "/node.key", "natsSeed": SecretsRoot + "/node.seed",
		"transportSeed": SecretsRoot + "/transport.seed", "transportTrust": SecretsRoot + "/transport-trust.json", "repositoryTrust": SecretsRoot + "/artifact-trust.json",
	}
	for name, value := range canonical {
		if value != expected[name] {
			return fmt.Errorf("%s 必须使用内核固定远端路径 %s", name, expected[name])
		}
	}
	if p.Node.RepositoryCA != "" && p.Node.RepositoryCA != SecretsRoot+"/artifact-ca.pem" {
		return fmt.Errorf("repositoryCa 必须使用内核固定远端路径 %s", SecretsRoot+"/artifact-ca.pem")
	}
	if err := validCredentialName(p.SSHIdentityCredential); err != nil {
		return fmt.Errorf("SSH identity credential: %w", err)
	}
	if err := validCredentialName(p.SSHKnownHostsCredential); err != nil {
		return fmt.Errorf("SSH known_hosts credential: %w", err)
	}
	if len(p.SecretFiles) > maxBootstrapSecretFiles {
		return fmt.Errorf("秘密文件不能超过 %d 个", maxBootstrapSecretFiles)
	}
	destinations := make(map[string]struct{}, len(p.SecretFiles))
	for i := range p.SecretFiles {
		file := &p.SecretFiles[i]
		if err := validCredentialName(file.Credential); err != nil {
			return fmt.Errorf("secretFiles[%d].credential: %w", i, err)
		}
		if file.Mode == 0 {
			file.Mode = 0o440
		}
		if file.Mode != 0o440 {
			return fmt.Errorf("secretFiles[%d].mode 必须是 0440", i)
		}
		if err := secureRemotePath(file.Destination); err != nil {
			return fmt.Errorf("secretFiles[%d].destination: %w", i, err)
		}
		if _, exists := destinations[file.Destination]; exists {
			return fmt.Errorf("远端秘密文件目标重复: %s", file.Destination)
		}
		destinations[file.Destination] = struct{}{}
	}
	for _, required := range []string{p.Node.NATSCA, p.Node.NATSCert, p.Node.NATSKey, p.Node.NATSSeed, p.Node.TransportSeed, p.Node.TransportTrust, p.Node.RepositoryTrust, ArtifactTokenFile} {
		if _, ok := destinations[required]; !ok {
			return fmt.Errorf("缺少必须下发的秘密/信任文件: %s", required)
		}
	}
	if p.Node.RepositoryCA != "" {
		if _, ok := destinations[p.Node.RepositoryCA]; !ok {
			return fmt.Errorf("缺少制品仓库 CA 文件: %s", p.Node.RepositoryCA)
		}
	}
	expectedFiles := 8
	if p.Node.RepositoryCA != "" {
		expectedFiles++
	}
	if len(destinations) != expectedFiles {
		return errors.New("在线引导计划不能下发额外秘密文件")
	}
	return nil
}

func validCredentialName(value string) error {
	if !credentialNamePattern.MatchString(value) {
		return errors.New("凭证名称必须是 1-160 位安全名称")
	}
	return nil
}

type Scope struct {
	TenantID  string
	ProjectID string
	PluginID  string
}

func (s Scope) Validate() error {
	if strings.TrimSpace(s.TenantID) == "" || strings.TrimSpace(s.PluginID) == "" {
		return errors.New("节点引导 scope 必须包含 tenant 和 plugin")
	}
	return nil
}

type Result struct {
	SystemdActive bool   `json:"systemdActive"`
	NodeID        string `json:"nodeId"`
	Endpoint      string `json:"endpoint"`
	Service       string `json:"service"`
}

// Broker is implemented by the trusted kernel deployment adapter. Plugins can
// submit a Plan but can neither resolve credentials nor choose a shell command.
type Broker interface {
	Bootstrap(context.Context, Scope, Plan) (Result, error)
}
