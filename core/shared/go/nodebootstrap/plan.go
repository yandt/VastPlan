package nodebootstrap

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// SecretPayload is material read by the trusted bootstrap command. It must not
// be logged, persisted in a deployment document or returned by an API.
type SecretPayload struct {
	Destination string
	Mode        uint32
	Content     []byte
}

// RenderSystemdUnit renders the fixed Node Agent service. User-controlled
// values become systemd arguments, never shell fragments.
func RenderSystemdUnit(request Request) (string, error) {
	if err := request.Validate(); err != nil {
		return "", err
	}
	node := request.Node
	args := []string{
		InstallRoot + "/current", "reconcile",
		"-nats-url", node.NATSURL,
		"-nats-ca", node.NATSCA,
		"-nats-cert", node.NATSCert,
		"-nats-key", node.NATSKey,
		"-nats-seed", node.NATSSeed,
		"-transport-seed", node.TransportSeed,
		"-transport-trust", node.TransportTrust,
		"-repository-url", node.RepositoryURL,
		"-repository-trust", node.RepositoryTrust,
		"-runtime-root", StateRoot + "/runtime/plugins",
		"-actual-state", StateRoot + "/actual-state.json",
		"-node-id", node.ID,
		"-deployment", node.Deployment,
		"-tenant", node.Tenant,
		"-capacity-cpu-millis", strconv.FormatInt(node.CapacityCPU, 10),
		"-capacity-memory-bytes", strconv.FormatInt(node.CapacityMemory, 10),
		"-capacity-gpu", strconv.FormatInt(node.CapacityGPU, 10),
		"-third-party-plugin-policy", "require-isolation",
		"-plugin-placement-default", "process-only",
	}
	if node.RepositoryCA != "" {
		args = append(args, "-repository-ca", node.RepositoryCA)
	}
	if node.Labels != "" {
		args = append(args, "-labels", node.Labels)
	}
	for i := range args {
		args[i] = systemdArgument(args[i])
	}
	return `[Unit]
Description=VastPlan Backend Node Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=` + ServiceUser + `
Group=` + ServiceUser + `
EnvironmentFile=` + ArtifactTokenFile + `
ExecStart=` + strings.Join(args, " ") + `
Restart=always
RestartSec=5s
TimeoutStopSec=90s
KillSignal=SIGTERM
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
ReadWritePaths=` + StateRoot + `

[Install]
WantedBy=multi-user.target
`, nil
}

// RenderInstallScript builds a fixed, idempotent bootstrap program. The only
// accepted remote command is `sudo -n /bin/sh -s --`; all varying content is
// base64-decoded from stdin after Request validation.
func RenderInstallScript(request Request, secrets []SecretPayload) ([]byte, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	unit, err := RenderSystemdUnit(request)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]SecretFile, len(request.SecretFiles))
	for _, file := range request.SecretFiles {
		allowed[file.Destination] = file
	}
	if len(secrets) != len(allowed) {
		return nil, errors.New("秘密文件内容与引导规格不完整")
	}
	sort.Slice(secrets, func(i, j int) bool { return secrets[i].Destination < secrets[j].Destination })
	seen := map[string]struct{}{}
	totalSecretBytes := 0
	var script bytes.Buffer
	script.WriteString("#!/bin/sh\nset -eu\numask 077\n")
	script.WriteString("for command in curl sha256sum systemctl base64 getent groupadd useradd usermod install mktemp; do command -v \"$command\" >/dev/null 2>&1; done\n")
	script.WriteString("if ! getent group " + ServiceUser + " >/dev/null 2>&1; then groupadd --system " + ServiceUser + "; fi\n")
	script.WriteString("if ! getent passwd " + ServiceUser + " >/dev/null 2>&1; then useradd --system --gid " + ServiceUser + " --home " + StateRoot + " --shell /usr/sbin/nologin " + ServiceUser + "; else usermod -a -G " + ServiceUser + " " + ServiceUser + "; fi\n")
	script.WriteString("install -d -m 0755 " + InstallRoot + "/releases\n")
	script.WriteString("install -d -o " + ServiceUser + " -g " + ServiceUser + " -m 0700 " + StateRoot + " " + StateRoot + "/runtime\n")
	script.WriteString("install -d -m 0755 " + ConfigRoot + "\n")
	// The agent may read secret files through its group but cannot replace them.
	script.WriteString("install -d -o root -g " + ServiceUser + " -m 0750 " + SecretsRoot + "\n")
	for _, payload := range secrets {
		spec, ok := allowed[payload.Destination]
		if !ok || payload.Mode != spec.Mode || len(payload.Content) == 0 {
			return nil, fmt.Errorf("秘密文件内容与规格不匹配: %s", payload.Destination)
		}
		if _, duplicate := seen[payload.Destination]; duplicate {
			return nil, fmt.Errorf("秘密文件内容重复: %s", payload.Destination)
		}
		if payload.Destination == ArtifactTokenFile && !validArtifactTokenFile(payload.Content) {
			return nil, errors.New("制品令牌环境文件只能包含 VASTPLAN_ARTIFACT_READ_TOKEN")
		}
		totalSecretBytes += len(payload.Content)
		if totalSecretBytes > maxBootstrapSecretTotalBytes {
			return nil, fmt.Errorf("引导文件总大小不能超过 %d 字节", maxBootstrapSecretTotalBytes)
		}
		seen[payload.Destination] = struct{}{}
		writeBase64File(&script, payload.Content, payload.Destination, payload.Mode, "root:"+ServiceUser)
	}
	versionDir := InstallRoot + "/releases/" + request.Release.Version
	script.WriteString("install -d -m 0755 " + versionDir + "\n")
	script.WriteString("kernel_tmp=$(mktemp " + versionDir + "/.backend-kernel.XXXXXX)\n")
	script.WriteString("trap 'rm -f \"$kernel_tmp\"' EXIT HUP INT TERM\n")
	script.WriteString("kernel_url=$(printf '%s' '" + base64.StdEncoding.EncodeToString([]byte(request.Release.URL)) + "' | base64 -d)\n")
	script.WriteString("curl --fail --location --silent --show-error --proto '=https' --tlsv1.2 --output \"$kernel_tmp\" \"$kernel_url\"\n")
	script.WriteString("printf '%s  %s\\n' '" + request.Release.SHA256 + "' \"$kernel_tmp\" | sha256sum --check --status\n")
	script.WriteString("install -m 0755 \"$kernel_tmp\" " + versionDir + "/backend-kernel\n")
	script.WriteString("ln -sfn releases/" + request.Release.Version + "/backend-kernel " + InstallRoot + "/current.next\n")
	script.WriteString("mv -Tf " + InstallRoot + "/current.next " + InstallRoot + "/current\n")
	writeBase64File(&script, []byte(unit), SystemdUnitPath, 0o644, "root:root")
	script.WriteString("systemctl daemon-reload\n")
	script.WriteString("systemctl enable --now vastplan-node-agent.service\n")
	script.WriteString("systemctl is-active --quiet vastplan-node-agent.service\n")
	script.WriteString("trap - EXIT HUP INT TERM\nrm -f \"$kernel_tmp\"\n")
	return script.Bytes(), nil
}

func writeBase64File(script *bytes.Buffer, content []byte, destination string, mode uint32, owner string) {
	temporary := destination + ".next"
	script.WriteString("printf '%s' '")
	script.WriteString(base64.StdEncoding.EncodeToString(content))
	script.WriteString("' | base64 -d > ")
	script.WriteString(temporary)
	script.WriteByte('\n')
	script.WriteString(fmt.Sprintf("chmod %04o %s\n", mode, temporary))
	script.WriteString("chown " + owner + " " + temporary + "\n")
	script.WriteString("mv -f " + temporary + " " + destination + "\n")
}

func systemdArgument(value string) string {
	// systemd expands percent specifiers even inside quotes.
	value = strings.ReplaceAll(value, "%", "%%")
	return strconv.Quote(value)
}

func validArtifactTokenFile(content []byte) bool {
	const prefix = "VASTPLAN_ARTIFACT_READ_TOKEN="
	value := strings.TrimSuffix(string(content), "\n")
	if !strings.HasPrefix(value, prefix) || strings.Contains(value, "\n") {
		return false
	}
	token := strings.TrimPrefix(value, prefix)
	if len(token) < 16 || len(token) > 4096 {
		return false
	}
	for _, c := range token {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && !strings.ContainsRune("._~-", c) {
			return false
		}
	}
	return true
}
