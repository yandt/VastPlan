package credentialbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/callcontext"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
	"cdsoft.com.cn/VastPlan/core/shared/go/runtimeidentity"
)

const (
	DatabaseRuntimePluginID   = databasev1.RuntimePluginID
	DatabaseCredentialOwner   = databasev1.ConnectionManagerPluginID
	DatabaseCredentialPurpose = databasev1.CredentialPurpose
)

// RuntimeLease relays ciphertext from the credential custodian to a verified
// Database Runtime instance. It deliberately never constructs a Recipient and
// therefore cannot decrypt the returned material.
type RuntimeLease struct {
	invoke LeaseInvoker
	now    func() time.Time
}

func NewRuntimeLease(invoke LeaseInvoker) (*RuntimeLease, error) {
	if invoke == nil {
		return nil, errors.New("runtime material lease 调用器不能为空")
	}
	return &RuntimeLease{invoke: invoke, now: time.Now}, nil
}

func (b *RuntimeLease) IssueRuntimeLease(ctx context.Context, tenant string, identity runtimeidentity.Identity,
	request credentiallease.Request) (credentiallease.Envelope, error) {
	if b == nil || b.invoke == nil || ctx == nil || strings.TrimSpace(tenant) == "" {
		return credentiallease.Envelope{}, errors.New("runtime material lease 参数无效")
	}
	if err := authorizeDatabaseRuntime(identity, request); err != nil {
		return credentiallease.Envelope{}, err
	}
	if err := credentiallease.ValidateRequest(request); err != nil {
		return credentiallease.Envelope{}, err
	}
	audience, err := identity.Audience()
	if err != nil {
		return credentiallease.Envelope{}, err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return credentiallease.Envelope{}, err
	}
	operation, logicalService, routingDomain := "issue", materialLeaseLogicalService, materialLeaseRoutingDomain
	target := &contractv1.CallTarget{
		ExtensionPoint: "tool.package", Capability: materialLeaseCapability, Operation: &operation,
		LogicalService: &logicalService, RoutingDomain: &routingDomain,
	}
	wire := &contractv1.CallContext{
		TenantId: tenant,
		Caller:   &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: audience},
		Scene:    "kernel.runtime.material-lease",
	}
	trusted := callcontext.MustAdopt(wire, callcontext.Provenance{
		Source: "credentialbroker.runtime-lease", AuthenticatedBy: "backend-kernel",
		Audience: materialLeaseLogicalService, IssuedAt: b.now().UTC(),
	})
	result, response, err := b.invoke(callcontext.WithTrusted(ctx, trusted), target, trusted.Wire(), payload)
	if err != nil {
		return credentiallease.Envelope{}, fmt.Errorf("申请 runtime material lease: %w", err)
	}
	if result == nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		message := "runtime material lease 签发失败"
		if result != nil && result.GetError().GetMessage() != "" {
			message = result.GetError().GetMessage()
		}
		return credentiallease.Envelope{}, errors.New(message)
	}
	var envelope credentiallease.Envelope
	decoder := json.NewDecoder(bytes.NewReader(response))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return credentiallease.Envelope{}, fmt.Errorf("解码 runtime material lease: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return credentiallease.Envelope{}, errors.New("runtime material lease 响应只能包含一个 JSON 文档")
	}
	if envelope.TenantID != tenant || envelope.Audience != audience || envelope.Ref != request.Ref {
		return credentiallease.Envelope{}, errors.New("runtime material lease claims 与可信启动身份不匹配")
	}
	return envelope, nil
}

func authorizeDatabaseRuntime(identity runtimeidentity.Identity, request credentiallease.Request) error {
	if err := identity.Validate(); err != nil {
		return err
	}
	if identity.PluginID != DatabaseRuntimePluginID || identity.Publisher != "vastplan" {
		return errors.New("runtime material lease 只授权首方 Database Runtime")
	}
	ref := request.Ref
	if ref.Scope != "tenant" || ref.Owner != DatabaseCredentialOwner || ref.Purpose != DatabaseCredentialPurpose || ref.Name != "" {
		return errors.New("Database Runtime 只能读取 connection-manager 拥有的 database.connection 凭证")
	}
	return nil
}
