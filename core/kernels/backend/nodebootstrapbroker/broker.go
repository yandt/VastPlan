// Package nodebootstrapbroker adapts the generic kernel CredentialBroker to
// the fixed Linux/SSH/systemd node bootstrap executor.
package nodebootstrapbroker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
)

type MaterialExecutor interface {
	Execute(context.Context, nodebootstrap.Target, []byte, []byte, []byte) error
}

type Broker struct {
	credentials kernelspi.CredentialBroker
	executor    MaterialExecutor
}

func New(credentials kernelspi.CredentialBroker, executor MaterialExecutor) (*Broker, error) {
	if credentials == nil || executor == nil {
		return nil, errors.New("节点引导必须配置 CredentialBroker 和 material executor")
	}
	return &Broker{credentials: credentials, executor: executor}, nil
}

func NewSSH(credentials kernelspi.CredentialBroker, timeout time.Duration) (*Broker, error) {
	return New(credentials, nodebootstrap.MaterialSSHExecutor{Timeout: timeout})
}

func (b *Broker) Bootstrap(ctx context.Context, scope nodebootstrap.Scope, plan nodebootstrap.Plan) (nodebootstrap.Result, error) {
	if err := scope.Validate(); err != nil {
		return nodebootstrap.Result{}, err
	}
	if err := plan.Validate(); err != nil {
		return nodebootstrap.Result{}, err
	}
	type reference struct {
		name        string
		destination string
		mode        uint32
	}
	refs := []reference{{name: plan.SSHIdentityCredential}, {name: plan.SSHKnownHostsCredential}}
	for _, file := range plan.SecretFiles {
		refs = append(refs, reference{name: file.Credential, destination: file.Destination, mode: file.Mode})
	}
	values := make([][]byte, len(refs))
	kernelScope := kernelspi.Scope{TenantID: scope.TenantID, ProjectID: scope.ProjectID, PluginID: scope.PluginID, Namespace: "node-bootstrap"}
	if err := kernelScope.Validate(); err != nil {
		return nodebootstrap.Result{}, err
	}
	var execute func(int) error
	execute = func(index int) error {
		if index < len(refs) {
			ref := kernelspi.CredentialRef{Name: refs[index].name, Scope: "tenant"}
			return b.credentials.WithCredential(ctx, kernelScope, ref, func(material kernelspi.CredentialMaterial) error {
				if material == nil {
					return fmt.Errorf("凭证 %s material 为空", refs[index].name)
				}
				raw := material.Bytes()
				if len(raw) == 0 || len(raw) > 4<<20 {
					return fmt.Errorf("凭证 %s material 大小无效", refs[index].name)
				}
				values[index] = raw
				defer func() { values[index] = nil }()
				return execute(index + 1)
			})
		}
		request := nodebootstrap.Request{Target: plan.Target, Release: plan.Release, Node: plan.Node}
		payloads := make([]nodebootstrap.SecretPayload, 0, len(plan.SecretFiles))
		for i, file := range plan.SecretFiles {
			request.SecretFiles = append(request.SecretFiles, nodebootstrap.SecretFile{Source: fmt.Sprintf("/credential/material-%02d", i), Destination: file.Destination, Mode: file.Mode})
			payloads = append(payloads, nodebootstrap.SecretPayload{Destination: file.Destination, Mode: file.Mode, Content: values[i+2]})
		}
		script, err := nodebootstrap.RenderInstallScript(request, payloads)
		if err != nil {
			return err
		}
		defer func() {
			for i := range script {
				script[i] = 0
			}
		}()
		return b.executor.Execute(ctx, plan.Target, script, values[0], values[1])
	}
	if err := execute(0); err != nil {
		return nodebootstrap.Result{}, fmt.Errorf("节点首次引导失败: %w", err)
	}
	return nodebootstrap.Result{SystemdActive: true, NodeID: plan.Node.ID, Endpoint: plan.Target.Endpoint(), Service: "vastplan-node-agent.service"}, nil
}
