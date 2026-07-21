//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type synchronizedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *synchronizedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(value)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

type portalKernelProcess struct {
	address string
	cancel  context.CancelFunc
	exited  <-chan struct{}
	result  *portalKernelProcessResult
	logs    *synchronizedBuffer
}

type portalKernelProcessResult struct {
	mu  sync.Mutex
	err error
}

func (r *portalKernelProcessResult) set(err error) {
	r.mu.Lock()
	r.err = err
	r.mu.Unlock()
}

func (r *portalKernelProcessResult) get() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

const portalBaseURLPlaceholder = "__VASTPLAN_PORTAL_BASE_URL__"

func startPortalKernelProcess(t *testing.T, root string, arguments ...string) *portalKernelProcess {
	t.Helper()
	address := freePortalKernelAddress(t)
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	baseURL := "https://localhost:" + port
	for index := range arguments {
		arguments[index] = strings.ReplaceAll(arguments[index], portalBaseURLPlaceholder, baseURL)
	}
	ctx, cancel := context.WithCancel(context.Background())
	logs := &synchronizedBuffer{}
	command := exec.CommandContext(ctx, "node", append([]string{filepath.Join(root, "core", "kernels", "frontend-host", "dist", "portal-host.cjs"), "--listen", address}, arguments...)...)
	command.Dir = root
	command.Stdout, command.Stderr = logs, logs
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	exited := make(chan struct{})
	result := &portalKernelProcessResult{}
	go func() {
		result.set(command.Wait())
		close(exited)
	}()
	process := &portalKernelProcess{address: address, cancel: cancel, exited: exited, result: result, logs: logs}
	t.Cleanup(func() {
		cancel()
		select {
		case <-exited:
			err := result.get()
			if err != nil && ctx.Err() == nil {
				t.Errorf("Node Portal Kernel shutdown: %v\n%s", err, logs.String())
			}
		case <-time.After(15 * time.Second):
			t.Errorf("Node Portal Kernel did not stop\n%s", logs.String())
		}
	})
	return process
}

func startOIDCPortalKernel(t *testing.T, root string, addressing *portalAddressingFixture, oidc *portalOIDCProvider, deliveryOrigin string) *portalKernelProcess {
	t.Helper()
	temporary := t.TempDir()
	portalAssets := writePortalKernelAssets(t, temporary)
	portalCert, portalKey := writePortalKernelTLSCertificate(t, temporary)
	sessionKey := make([]byte, 32)
	if _, err := rand.Read(sessionKey); err != nil {
		t.Fatal(err)
	}
	sessionKeyFile := filepath.Join(temporary, "portal-session.key")
	if err := os.WriteFile(sessionKeyFile, sessionKey, 0o600); err != nil {
		t.Fatal(err)
	}
	arguments := []string{
		"--portal-assets", portalAssets, "--tls-cert", portalCert, "--tls-key", portalKey,
		"--identity-provider", "oidc", "--oidc-issuer", oidc.issuer, "--oidc-client-id", oidc.clientID,
		"--oidc-client-auth-method", "none", "--oidc-redirect-uri", portalBaseURLPlaceholder + "/auth/callback",
		"--oidc-session-key-file", sessionKeyFile, "--oidc-allow-insecure",
	}
	if deliveryOrigin != "" {
		arguments = append(arguments, "--frontend-delivery-cache", filepath.Join(temporary, "frontend-cache"), "--frontend-delivery-origin", deliveryOrigin)
	}
	arguments = append(arguments, addressing.portalArguments(root)...)
	return startPortalKernelProcess(t, root, arguments...)
}

func buildPortalKernel(t *testing.T, root string) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js 未安装，跳过 Node Portal Kernel E2E")
	}
	if _, err := exec.LookPath("pnpm"); err != nil {
		t.Skip("pnpm 未安装，跳过 Node Portal Kernel E2E")
	}
	command := exec.Command("pnpm", "--filter", "@vastplan/portal-host", "build")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("构建 Node Portal Kernel: %v\n%s", err, output)
	}
}

func freePortalKernelAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func writePortalKernelAssets(t *testing.T, parent string) string {
	t.Helper()
	root := filepath.Join(parent, "portal-assets")
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	index := `<div id="vastplan-portal" aria-live="polite"></div><script type="importmap" nonce="__VASTPLAN_CSP_NONCE__">{"imports":{}}</script><script type="module" nonce="__VASTPLAN_CSP_NONCE__" src="/assets/portal.js"></script>`
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "portal.js"), []byte("export {};"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func writePortalKernelTLSCertificate(t *testing.T, directory string) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "node-portal-kernel-e2e"}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certFile := filepath.Join(directory, "portal-cert.pem")
	keyFile := filepath.Join(directory, "portal-key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

func (p *portalKernelProcess) baseURL() string {
	_, port, _ := net.SplitHostPort(p.address)
	return fmt.Sprintf("https://localhost:%s", port)
}
