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

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	"cdsoft.com.cn/VastPlan/core/internal/callcontext"
	"cdsoft.com.cn/VastPlan/core/internal/runtimeidentity"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactassessment"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/credentiallease"
)

const (
	DatabaseRuntimePluginID     = databasev1.RuntimePluginID
	DatabaseCredentialOwner     = databasev1.ConnectionManagerPluginID
	DatabaseCredentialPurpose   = databasev1.CredentialPurpose
	OIDCProviderPluginID        = authenticationv1.OIDCProviderPluginID
	OIDCCredentialPurpose       = "oidc.client-secret"
	WebhookProviderPluginID     = authenticationv1.WebhookDeliveryPluginID
	WebhookCredentialPurpose    = "authentication.delivery.webhook"
	AssessmentProviderPluginID  = artifactassessment.AssessmentProviderPluginID
	AssessmentCredentialPurpose = "artifact.assessment.signing-key"
)

type runtimeCredentialGrant struct{ pluginID, owner, purpose string }

var runtimeCredentialGrants = []runtimeCredentialGrant{
	{DatabaseRuntimePluginID, DatabaseCredentialOwner, DatabaseCredentialPurpose},
	{OIDCProviderPluginID, OIDCProviderPluginID, OIDCCredentialPurpose},
	{WebhookProviderPluginID, WebhookProviderPluginID, WebhookCredentialPurpose},
	{AssessmentProviderPluginID, AssessmentProviderPluginID, AssessmentCredentialPurpose},
}

// RuntimeLease relays ciphertext from the credential custodian to a verified
// first-party runtime whose plugin/owner/purpose tuple is explicitly granted.
// It never constructs a Recipient and cannot decrypt the returned material.
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
		return credentiallease.Envelope{}, credentiallease.NewFailure(credentiallease.ErrorInvalid, false, errors.New("runtime material lease 参数无效"))
	}
	if err := authorizeRuntimeCredential(identity, request); err != nil {
		return credentiallease.Envelope{}, credentiallease.NewFailure(credentiallease.ErrorDenied, false, err)
	}
	if err := credentiallease.ValidateRequest(request); err != nil {
		return credentiallease.Envelope{}, credentiallease.NewFailure(credentiallease.ErrorInvalid, false, err)
	}
	audience, err := identity.Audience()
	if err != nil {
		return credentiallease.Envelope{}, err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return credentiallease.Envelope{}, err
	}
	operation, logicalService, routingDomain := credentiallease.OperationIssue, credentiallease.LogicalService, credentiallease.RoutingDomain
	target := &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: credentiallease.Capability, Operation: &operation,
		LogicalService: &logicalService, RoutingDomain: &routingDomain,
	}
	wire := &contractv1.CallContext{
		TenantId: tenant,
		Caller:   &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: audience},
		Scene:    "kernel.runtime.material-lease",
	}
	trusted := callcontext.MustAdopt(wire, callcontext.Provenance{
		Source: "credentialbroker.runtime-lease", AuthenticatedBy: "backend-kernel",
		Audience: credentiallease.LogicalService, IssuedAt: b.now().UTC(),
	})
	result, response, err := b.invoke(callcontext.WithTrusted(ctx, trusted), target, trusted.Wire(), payload)
	if err != nil {
		return credentiallease.Envelope{}, credentiallease.NewFailure(credentiallease.ErrorServiceUnavailable, true, fmt.Errorf("申请 runtime material lease: %w", err))
	}
	if result == nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		return credentiallease.Envelope{}, leaseFailureFromResult(result)
	}
	var envelope credentiallease.Envelope
	decoder := json.NewDecoder(bytes.NewReader(response))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return credentiallease.Envelope{}, credentiallease.NewFailure(credentiallease.ErrorInvalid, false, fmt.Errorf("解码 runtime material lease: %w", err))
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return credentiallease.Envelope{}, credentiallease.NewFailure(credentiallease.ErrorInvalid, false, errors.New("runtime material lease 响应只能包含一个 JSON 文档"))
	}
	if envelope.TenantID != tenant || envelope.Audience != audience || envelope.Ref != request.Ref {
		return credentiallease.Envelope{}, credentiallease.NewFailure(credentiallease.ErrorInvalid, false, errors.New("runtime material lease claims 与可信启动身份不匹配"))
	}
	return envelope, nil
}

func authorizeRuntimeCredential(identity runtimeidentity.Identity, request credentiallease.Request) error {
	if err := identity.Validate(); err != nil {
		return err
	}
	if identity.Publisher != "vastplan" {
		return errors.New("runtime material lease 只授权明确列出的首方 Runtime")
	}
	ref := request.Ref
	if ref.Scope != "tenant" || ref.Name != "" {
		return errors.New("runtime material lease 只接受 tenant scoped 无名称精确凭证")
	}
	for _, grant := range runtimeCredentialGrants {
		if identity.PluginID == grant.pluginID && ref.Owner == grant.owner && ref.Purpose == grant.purpose {
			return nil
		}
	}
	return errors.New("runtime material lease 插件、owner 与 purpose 不在精确授权表")
}
