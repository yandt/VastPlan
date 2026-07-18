package callcontext

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"google.golang.org/protobuf/proto"
)

const (
	ReservedInternalPrefix  = "vastplan.internal."
	ReservedTransportPrefix = "vastplan.transport."
)

// Provenance is host-only evidence about how a context became trusted. It is
// intentionally absent from the protobuf and must never cross a wire boundary.
type Provenance struct {
	Source            string
	AuthenticatedBy   string
	TransportIdentity string
	TransportRole     string
	Audience          string
	IssuedAt          time.Time
}

// Trusted is an immutable snapshot plus host-only provenance. Every accessor
// returns a copy so callers cannot mutate the trusted baseline.
type Trusted struct {
	wire       *contractv1.CallContext
	provenance Provenance
}

type trustedContextKey struct{}

func WithTrusted(ctx context.Context, trusted Trusted) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, trustedContextKey{}, trusted)
}

func FromContext(ctx context.Context) (Trusted, bool) {
	if ctx == nil {
		return Trusted{}, false
	}
	trusted, ok := ctx.Value(trustedContextKey{}).(Trusted)
	return trusted, ok
}

// ValidateIngress validates an untrusted wire object, normalizes the temporary
// duplicated tenant field, strips no data silently, and returns a trusted copy.
func ValidateIngress(untrusted *contractv1.CallContext, provenance Provenance) (Trusted, error) {
	wire := &contractv1.CallContext{}
	if untrusted != nil {
		wire = proto.Clone(untrusted).(*contractv1.CallContext)
	}
	principalTenant := wire.GetPrincipal().GetTenantId()
	switch {
	case wire.TenantId != "" && principalTenant != "" && wire.TenantId != principalTenant:
		return Trusted{}, errors.New("CallContext tenant_id 与 Principal.tenant_id 不一致")
	case wire.TenantId == "" && principalTenant != "":
		wire.TenantId = principalTenant
	case wire.Principal != nil && wire.TenantId != "":
		wire.Principal.TenantId = wire.TenantId
	}
	for key := range wire.Metadata {
		if isReservedMetadataKey(key) {
			return Trusted{}, fmt.Errorf("CallContext metadata 使用宿主保留键 %q", key)
		}
		if strings.TrimSpace(key) == "" || !strings.Contains(key, ".") {
			return Trusted{}, fmt.Errorf("CallContext metadata 键 %q 必须命名空间化", key)
		}
	}
	seenCredentials := map[string]struct{}{}
	for _, ref := range wire.Credentials {
		if ref == nil || strings.TrimSpace(ref.Name) == "" {
			return Trusted{}, errors.New("CredentialRef.name 不能为空")
		}
		key := ref.Name + "\x00" + ref.GetScope()
		if _, duplicate := seenCredentials[key]; duplicate {
			return Trusted{}, fmt.Errorf("CredentialRef 重复: %s", ref.Name)
		}
		seenCredentials[key] = struct{}{}
	}
	if provenance.IssuedAt.IsZero() {
		provenance.IssuedAt = time.Now().UTC()
	}
	return Trusted{wire: wire, provenance: provenance}, nil
}

// MustAdopt is for host-constructed contexts whose invalidity is a programming
// error. External input must use ValidateIngress and handle its error.
func MustAdopt(wire *contractv1.CallContext, provenance Provenance) Trusted {
	trusted, err := ValidateIngress(wire, provenance)
	if err != nil {
		panic(err)
	}
	return trusted
}

func (t Trusted) Wire() *contractv1.CallContext {
	if t.wire == nil {
		return &contractv1.CallContext{}
	}
	return proto.Clone(t.wire).(*contractv1.CallContext)
}

func (t Trusted) Provenance() Provenance { return t.provenance }

func (t Trusted) Project(projection Projection) (*contractv1.CallContext, error) {
	if err := projection.Validate(); err != nil {
		return nil, err
	}
	return projectWire(t.wire, projection), nil
}

// Derivation contains only fields a trusted host is allowed to rewrite per hop.
type Derivation struct {
	Caller         *contractv1.Caller
	DeadlineUnixMs *int64
	Trace          *contractv1.Trace
	AppendCallPath string
	Scene          *string
	Provenance     Provenance
}

func (t Trusted) Derive(derivation Derivation) (Trusted, error) {
	wire := t.Wire()
	if derivation.Caller != nil {
		wire.Caller = proto.Clone(derivation.Caller).(*contractv1.Caller)
	}
	if derivation.DeadlineUnixMs != nil {
		value := *derivation.DeadlineUnixMs
		wire.DeadlineUnixMs = &value
	}
	if derivation.Trace != nil {
		wire.Trace = proto.Clone(derivation.Trace).(*contractv1.Trace)
	}
	if derivation.AppendCallPath != "" {
		wire.CallPath = append(wire.CallPath, derivation.AppendCallPath)
	}
	if derivation.Scene != nil {
		wire.Scene = *derivation.Scene
	}
	provenance := t.provenance
	if derivation.Provenance != (Provenance{}) {
		provenance = derivation.Provenance
	}
	return ValidateIngress(wire, provenance)
}

func isReservedMetadataKey(key string) bool {
	return strings.HasPrefix(key, ReservedInternalPrefix) || strings.HasPrefix(key, ReservedTransportPrefix)
}
