package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func (r *runtime) startPortalKernel(ctx context.Context, env map[string]string, natsURL string) error {
	base, err := r.portalBaseArguments(natsURL, r.options.portalListen)
	if err != nil {
		return err
	}
	portalArgs := append(append([]string{}, base...), r.identity.portalArguments(r)...)
	if _, err := r.startChild("portal-kernel", env, "node", portalArgs...); err != nil {
		return err
	}
	if err := waitHTTP(ctx, "http://"+r.options.portalListen+"/readyz", 45*time.Second, false); err != nil {
		return fmt.Errorf("Node Portal Kernel 未就绪: %w", err)
	}
	return nil
}

func (r *runtime) publishInitialPortal(ctx context.Context, env map[string]string, natsURL string) error {
	if !r.identity.needsBootstrapPublisher() {
		return publishPortal("http://"+r.options.portalListen,
			filepath.Join(r.options.root, "engineering", "deploy", "portal-application-composition.json"),
			filepath.Join(r.runDir, "portal-platform-catalog.json"))
	}
	listen, err := availableLoopbackAddress()
	if err != nil {
		return err
	}
	base, err := r.portalBaseArguments(natsURL, listen)
	if err != nil {
		return err
	}
	bootstrapArgs := append(append([]string{}, base...), autoLoginIdentityProtocol{}.portalArguments(r)...)
	return r.withBootstrapPublisherPortal(ctx, env, listen, bootstrapArgs, func() error {
		return publishPortal("http://"+listen,
			filepath.Join(r.options.root, "engineering", "deploy", "portal-application-composition.json"),
			filepath.Join(r.runDir, "portal-platform-catalog.json"))
	})
}

func availableLoopbackAddress() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("分配临时 Portal 发布端口: %w", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", err
	}
	return address, nil
}

func (r *runtime) portalBaseArguments(natsURL, listen string) ([]string, error) {
	args := []string{
		filepath.Join(r.options.root, "core", "kernels", "frontend-host", "dist", "portal-host.cjs"),
		"--listen", listen,
		"--allow-insecure-http",
		"--portal-assets", filepath.Join(r.runDir, "portal-assets"),
		"--access-profile-catalog", filepath.Join(r.runDir, "access-profile-catalog.json"),
		"--api-contract-catalog", filepath.Join(r.persistentStateRoot(), "api-contract-catalog.json"),
		"--frontend-delivery-origin", filepath.Join(r.persistentStateRoot(), "frontend-delivery-origin"),
		"--frontend-delivery-cache", filepath.Join(r.runDir, "frontend-delivery-cache"),
		"--nats-servers", natsURL, "--allow-insecure-nats",
		"--addressing-contracts", filepath.Join(r.options.root, "contracts", "proto"),
		"--transport-seed", filepath.Join(r.runDir, "secrets", portalHostTransportSeed),
		"--transport-trust", filepath.Join(r.runDir, "secrets", transportTrustDocument),
		"--composer-logical-service", "platform.portal-composer",
		"--interaction-logical-service", "platform.interaction-broker",
		"--platform-control-bootstrap-logical-service", "platform.database",
		"--kernel-recovery-url", "http://" + r.options.recoveryListen,
	}
	return appendPublishedAPIExposureCatalog(args, filepath.Join(r.persistentStateRoot(), "api-exposure-gateway.json"))
}

func (r *runtime) withBootstrapPublisherPortal(ctx context.Context, env map[string]string, listen string, args []string, action func() error) error {
	cmd := exec.Command("node", args...)
	configureManagedChild(cmd)
	cmd.Dir = r.options.root
	cmd.Env = mergedEnv(env)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动临时 Portal 发布端: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stop := func() error {
		if cmd.Process != nil {
			_ = cmd.Process.Signal(os.Interrupt)
		}
		select {
		case err := <-done:
			return err
		case <-time.After(8 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return <-done
		}
	}
	stopped := false
	defer func() {
		if !stopped {
			_ = stop()
		}
	}()
	if err := waitHTTPOrExit(ctx, "http://"+listen+"/readyz", 45*time.Second, done); err != nil {
		return fmt.Errorf("临时 Portal 发布端未就绪: %w", err)
	}
	if err := action(); err != nil {
		return err
	}
	if err := stop(); err != nil && !isExpectedProcessStop(err) {
		return fmt.Errorf("关闭临时 Portal 发布端: %w", err)
	}
	stopped = true
	return nil
}

func waitHTTPOrExit(ctx context.Context, endpoint string, timeout time.Duration, done <-chan error) error {
	probe := make(chan error, 1)
	go func() { probe <- waitHTTP(ctx, endpoint, timeout, true) }()
	select {
	case err := <-probe:
		return err
	case err := <-done:
		if err == nil {
			return errors.New("进程在就绪前退出")
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func isExpectedProcessStop(err error) bool {
	var exitError *exec.ExitError
	return errors.As(err, &exitError)
}
