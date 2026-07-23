// Package credentialmaterial implements the trusted-runtime side of encrypted
// Material Lease handoff. It is shared by first-party runtimes; authorization
// remains enforced by the kernel's exact plugin/owner/purpose grant table.
package credentialmaterial

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocol"
	"cdsoft.com.cn/VastPlan/core/shared/go/runtimeidentity"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type Material interface{ Bytes() []byte }

type Source struct {
	host     sdk.Host
	tenant   string
	audience string
	ref      commonv1.ManagedCredentialRef
}

func New(host sdk.Host, tenant string, ref commonv1.ManagedCredentialRef, audience string) (*Source, error) {
	tenant, audience = strings.TrimSpace(tenant), strings.TrimSpace(audience)
	if host == nil || tenant == "" || runtimeidentity.ValidateAudience(audience) != nil {
		return nil, errors.New("runtime material source 启动身份无效")
	}
	if err := credentiallease.ValidateCredentialRef(ref); err != nil {
		return nil, err
	}
	return &Source{host: host, tenant: tenant, audience: audience, ref: ref}, nil
}

func NewFromEnvironment(host sdk.Host, tenant string, ref commonv1.ManagedCredentialRef) (*Source, error) {
	return New(host, tenant, ref, os.Getenv(protocol.RuntimeAudienceEnvKey))
}

// WithMaterial opens one encrypted lease and zeroes plaintext immediately
// after use returns. now is explicit so deterministic callers can test expiry.
func (s *Source) WithMaterial(ctx context.Context, now time.Time, use func(Material) error) error {
	if s == nil || s.host == nil || ctx == nil || now.Location() != time.UTC || use == nil {
		return errors.New("runtime material source 参数无效")
	}
	request, recipient, err := credentiallease.NewRequest(s.ref)
	if err != nil {
		return err
	}
	defer recipient.Discard()
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	operation := "issue"
	result, response, err := s.host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.KernelService, Capability: credentiallease.RuntimeKernelService, Operation: &operation,
	}, &contractv1.CallContext{TenantId: s.tenant}, payload)
	if err != nil {
		return fmt.Errorf("申请 runtime material lease: %w", err)
	}
	if result == nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		message := "runtime material lease 被拒绝"
		if result != nil && result.GetError().GetMessage() != "" {
			message = result.GetError().GetMessage()
		}
		return errors.New(message)
	}
	var envelope credentiallease.Envelope
	decoder := json.NewDecoder(bytes.NewReader(response))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return fmt.Errorf("解码 runtime material lease: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("runtime material lease 响应只能包含一个 JSON 文档")
	}
	raw, err := recipient.Open(envelope, credentiallease.Claims{TenantID: s.tenant, Audience: s.audience, Ref: s.ref}, now)
	if err != nil {
		return err
	}
	defer zero(raw)
	return use(material(raw))
}

type material []byte

func (m material) Bytes() []byte { return m }
func zero(raw []byte) {
	for index := range raw {
		raw[index] = 0
	}
}
