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

	"cdsoft.com.cn/VastPlan/core/shared/go/callcontext"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
)

const (
	materialLeaseCapability     = "platform.credentials.material-lease"
	materialLeaseLogicalService = "platform.credentials"
	materialLeaseRoutingDomain  = "platform"
)

// LeaseInvoker is the narrow capability-router surface required by a trusted
// material adapter. It deliberately does not expose registration or discovery.
type LeaseInvoker func(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)

// ManagedLease exchanges a managed CredentialRef for a short-lived encrypted
// lease and opens it only inside the trusted host process. No plaintext is
// carried in the request or response payload.
type ManagedLease struct {
	audience string
	invoke   LeaseInvoker
	now      func() time.Time
}

func NewManagedLease(audience string, invoke LeaseInvoker) (*ManagedLease, error) {
	audience = strings.TrimSpace(audience)
	if audience == "" || len(audience) > 160 || invoke == nil {
		return nil, errors.New("material lease 宿主身份或调用器无效")
	}
	return &ManagedLease{audience: audience, invoke: invoke, now: time.Now}, nil
}

func (b *ManagedLease) WithCredential(ctx context.Context, scope kernelspi.Scope, ref kernelspi.CredentialRef, use func(kernelspi.CredentialMaterial) error) error {
	if b == nil || b.invoke == nil || ctx == nil || use == nil {
		return errors.New("material lease broker 参数无效")
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	if ref.Scope != "tenant" || ref.Owner != scope.PluginID || ref.Handle == "" || ref.Name != "" || ref.Purpose == "" || ref.Version < 1 {
		return errors.New("托管凭证引用与宿主 scope 不匹配")
	}
	request, recipient, err := credentiallease.NewRequest(ref)
	if err != nil {
		return err
	}
	defer recipient.Discard()
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("编码 material lease 请求: %w", err)
	}
	operation, logicalService, routingDomain := "issue", materialLeaseLogicalService, materialLeaseRoutingDomain
	target := &contractv1.CallTarget{
		ExtensionPoint: "tool.package", Capability: materialLeaseCapability, Operation: &operation,
		LogicalService: &logicalService, RoutingDomain: &routingDomain,
	}
	wire := &contractv1.CallContext{
		TenantId: scope.TenantID,
		Caller:   &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: b.audience},
		Scene:    "kernel.credential.material-lease",
	}
	trusted := callcontext.MustAdopt(wire, callcontext.Provenance{
		Source: "credentialbroker.managed-lease", AuthenticatedBy: "backend-kernel", Audience: materialLeaseLogicalService, IssuedAt: b.now().UTC(),
	})
	result, response, err := b.invoke(callcontext.WithTrusted(ctx, trusted), target, trusted.Wire(), payload)
	if err != nil {
		return fmt.Errorf("申请 material lease: %w", err)
	}
	if result == nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		message := "material lease 签发失败"
		if result != nil && result.GetError() != nil && result.GetError().GetMessage() != "" {
			message = result.GetError().GetMessage()
		}
		return errors.New(message)
	}
	var envelope credentiallease.Envelope
	decoder := json.NewDecoder(bytes.NewReader(response))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return fmt.Errorf("解码 material lease: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("material lease 响应只能包含一个 JSON 文档")
	}
	claims := credentiallease.Claims{TenantID: scope.TenantID, Audience: b.audience, Ref: ref}
	raw, err := recipient.Open(envelope, claims, b.now().UTC())
	if err != nil {
		return err
	}
	defer zeroMaterial(raw)
	return use(material(raw))
}

func zeroMaterial(raw []byte) {
	for index := range raw {
		raw[index] = 0
	}
}
