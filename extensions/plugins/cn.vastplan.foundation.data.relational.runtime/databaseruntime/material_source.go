package databaseruntime

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

// RuntimeMaterialSource asks the local Kernel to relay a lease encrypted to
// this process' one-use key. The Kernel sees only the CredentialRef, public key
// and ciphertext; Open runs exclusively inside Database Runtime.
type RuntimeMaterialSource struct {
	host     sdk.Host
	tenant   string
	audience string
	ref      commonv1.ManagedCredentialRef
	now      func() time.Time
}

func NewRuntimeMaterialSource(host sdk.Host, tenant string, ref commonv1.ManagedCredentialRef, audience string) (*RuntimeMaterialSource, error) {
	tenant, audience = strings.TrimSpace(tenant), strings.TrimSpace(audience)
	if host == nil || tenant == "" || runtimeidentity.ValidateAudience(audience) != nil {
		return nil, errors.New("Database Runtime material source 启动身份无效")
	}
	if err := credentiallease.ValidateCredentialRef(ref); err != nil {
		return nil, err
	}
	return &RuntimeMaterialSource{host: host, tenant: tenant, audience: audience, ref: ref, now: time.Now}, nil
}

// NewRuntimeMaterialSourceFromEnvironment reads the audience injected by the
// trusted Host. It is non-secret but reserved and cannot be inherited or
// overridden by a plugin manifest or execution driver.
func NewRuntimeMaterialSourceFromEnvironment(host sdk.Host, tenant string, ref commonv1.ManagedCredentialRef) (*RuntimeMaterialSource, error) {
	return NewRuntimeMaterialSource(host, tenant, ref, os.Getenv(protocol.RuntimeAudienceEnvKey))
}

func (s *RuntimeMaterialSource) WithMaterial(ctx context.Context, use func(CredentialMaterial) error) error {
	if s == nil || s.host == nil || ctx == nil || use == nil {
		return errors.New("Database Runtime material source 参数无效")
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
		return fmt.Errorf("申请 Database Runtime material lease: %w", err)
	}
	if result == nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		message := "Database Runtime material lease 被拒绝"
		if result != nil && result.GetError().GetMessage() != "" {
			message = result.GetError().GetMessage()
		}
		return errors.New(message)
	}
	var envelope credentiallease.Envelope
	decoder := json.NewDecoder(bytes.NewReader(response))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return fmt.Errorf("解码 Database Runtime material lease: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("Database Runtime material lease 响应只能包含一个 JSON 文档")
	}
	raw, err := recipient.Open(envelope, credentiallease.Claims{
		TenantID: s.tenant, Audience: s.audience, Ref: s.ref,
	}, s.now().UTC())
	if err != nil {
		return err
	}
	defer zeroRuntimeMaterial(raw)
	return use(runtimeMaterial(raw))
}

type runtimeMaterial []byte

func (m runtimeMaterial) Bytes() []byte { return m }

func zeroRuntimeMaterial(raw []byte) {
	for index := range raw {
		raw[index] = 0
	}
}
