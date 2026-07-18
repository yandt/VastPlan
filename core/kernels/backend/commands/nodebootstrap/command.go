// Package nodebootstrapcommand implements the production node-bootstrap
// subcommand. It is intentionally separate from Portal HTTP handling so the
// same audited operation can later be invoked by a typed deployment service.
package nodebootstrapcommand

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
)

const maxRequestBytes = 1 << 20

type remoteExecutor interface {
	Execute(context.Context, nodebootstrap.Target, []byte) error
}

// Run validates a declarative bootstrap request, loads owner-only local
// material and enrolls one Linux host through a strict known_hosts boundary.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("node-bootstrap", flag.ContinueOnError)
	flags.SetOutput(stderr)
	requestFile := flags.String("request", "", "0600 Linux 节点引导请求 JSON")
	identityFile := flags.String("identity", "", "0600 SSH 私钥文件")
	knownHostsFile := flags.String("known-hosts", "", "受控 known_hosts 文件（禁止 TOFU）")
	timeout := flags.Duration("timeout", 2*time.Minute, "完整 SSH 引导超时")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *requestFile == "" || *identityFile == "" || *knownHostsFile == "" {
		flags.Usage()
		return errors.New("必须提供 -request、-identity 和 -known-hosts")
	}
	if *timeout <= 0 || *timeout > 30*time.Minute {
		return errors.New("timeout 必须位于 0 到 30 分钟之间")
	}
	request, err := loadRequest(*requestFile)
	if err != nil {
		return err
	}
	executor := nodebootstrap.SSHExecutor{IdentityFile: *identityFile, KnownHostsFile: *knownHostsFile, Timeout: min(*timeout, 30*time.Second)}
	return execute(ctx, request, executor, *timeout, stdout)
}

func execute(ctx context.Context, request nodebootstrap.Request, executor remoteExecutor, timeout time.Duration, stdout io.Writer) error {
	if executor == nil {
		return errors.New("SSH executor 不能为空")
	}
	if err := request.Validate(); err != nil {
		return fmt.Errorf("引导请求无效: %w", err)
	}
	secrets, err := nodebootstrap.LoadSecretPayloads(request.SecretFiles)
	if err != nil {
		return err
	}
	defer func() {
		for i := range secrets {
			for j := range secrets[i].Content {
				secrets[i].Content[j] = 0
			}
		}
	}()
	script, err := nodebootstrap.RenderInstallScript(request, secrets)
	if err != nil {
		return err
	}
	defer func() {
		for i := range script {
			script[i] = 0
		}
	}()
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := executor.Execute(runCtx, request.Target, script); err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(map[string]any{
		"status": "systemd_active", "nodeId": request.Node.ID,
		"endpoint": request.Target.Endpoint(), "service": "vastplan-node-agent.service",
	})
}

func loadRequest(filename string) (nodebootstrap.Request, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return nodebootstrap.Request{}, fmt.Errorf("读取引导请求: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() <= 0 || info.Size() > maxRequestBytes {
		return nodebootstrap.Request{}, errors.New("引导请求必须是仅属主可访问且大小受限的普通文件")
	}
	handle, err := os.Open(filename)
	if err != nil {
		return nodebootstrap.Request{}, fmt.Errorf("读取引导请求: %w", err)
	}
	defer handle.Close()
	openedInfo, err := handle.Stat()
	if err != nil || !os.SameFile(info, openedInfo) || !openedInfo.Mode().IsRegular() {
		return nodebootstrap.Request{}, errors.New("引导请求在读取期间发生替换")
	}
	raw, err := io.ReadAll(io.LimitReader(handle, maxRequestBytes+1))
	if err != nil || len(raw) > maxRequestBytes {
		return nodebootstrap.Request{}, errors.New("读取引导请求失败或内容过大")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request nodebootstrap.Request
	if err := decoder.Decode(&request); err != nil {
		return nodebootstrap.Request{}, fmt.Errorf("解析引导请求: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nodebootstrap.Request{}, errors.New("引导请求只能包含一个 JSON 文档")
	}
	if err := request.Validate(); err != nil {
		return nodebootstrap.Request{}, fmt.Errorf("引导请求无效: %w", err)
	}
	return request, nil
}
