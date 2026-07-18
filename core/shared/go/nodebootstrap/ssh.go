package nodebootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const maxRemoteDiagnosticBytes = 32 << 10

// SSHExecutor performs exactly one fixed bootstrap operation. It does not
// expose an arbitrary command API to Portal or deployment plugins.
type SSHExecutor struct {
	IdentityFile   string
	KnownHostsFile string
	Timeout        time.Duration
}

func (e SSHExecutor) Execute(ctx context.Context, target Target, script []byte) error {
	if err := target.Validate(); err != nil {
		return err
	}
	if len(script) == 0 {
		return errors.New("SSH 引导脚本不能为空")
	}
	if e.IdentityFile == "" || e.KnownHostsFile == "" {
		return errors.New("SSH 引导必须配置 identity 和 known_hosts")
	}
	identity, err := readOwnerFile(e.IdentityFile, true)
	if err != nil {
		return fmt.Errorf("读取 SSH identity: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(identity)
	for i := range identity {
		identity[i] = 0
	}
	if err != nil {
		return fmt.Errorf("解析 SSH identity: %w", err)
	}
	if _, err := readOwnerFile(e.KnownHostsFile, false); err != nil {
		return fmt.Errorf("校验 known_hosts: %w", err)
	}
	hostKeyCallback, err := knownhosts.New(e.KnownHostsFile)
	if err != nil {
		return fmt.Errorf("加载 known_hosts: %w", err)
	}
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	dialer := net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", target.Endpoint())
	if err != nil {
		return fmt.Errorf("连接 SSH 目标: %w", err)
	}
	defer connection.Close()
	clientConfig := &ssh.ClientConfig{
		User: target.User, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCallback, Timeout: timeout,
		ClientVersion: "SSH-2.0-VastPlan-NodeBootstrap",
	}
	clientConnection, channels, requests, err := ssh.NewClientConn(connection, target.Endpoint(), clientConfig)
	if err != nil {
		return fmt.Errorf("建立 SSH 安全会话: %w", err)
	}
	client := ssh.NewClient(clientConnection, channels, requests)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("创建 SSH session: %w", err)
	}
	defer session.Close()
	session.Stdin = bytes.NewReader(script)
	diagnostic := &limitedBuffer{limit: maxRemoteDiagnosticBytes}
	session.Stdout = diagnostic
	session.Stderr = diagnostic
	done := make(chan error, 1)
	go func() { done <- session.Run("sudo -n /bin/sh -s --") }()
	select {
	case <-ctx.Done():
		_ = client.Close()
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("远端引导失败: %w", err)
		}
		return nil
	}
}

func readOwnerFile(filename string, private bool) ([]byte, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxBootstrapSecretBytes {
		return nil, errors.New("必须是大小受限的普通文件")
	}
	if private && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("私钥文件不能授予 group/other 权限")
	}
	if !private && info.Mode().Perm()&0o022 != 0 {
		return nil, errors.New("known_hosts 不能被 group/other 写入")
	}
	handle, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	openedInfo, err := handle.Stat()
	if err != nil || !os.SameFile(info, openedInfo) || !openedInfo.Mode().IsRegular() {
		return nil, errors.New("文件在读取期间发生替换")
	}
	return io.ReadAll(io.LimitReader(handle, maxBootstrapSecretBytes+1))
}

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.buffer.Write(p)
	}
	return original, nil
}

func (b *limitedBuffer) String() string { return b.buffer.String() }
