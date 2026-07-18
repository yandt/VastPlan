// Package callcontext owns trusted CallContext validation, derivation and
// audience projection. The protobuf remains the single wire contract; this
// package is the only place where trust is attached to it.
package callcontext

import (
	"fmt"
	"sort"
	"strings"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"google.golang.org/protobuf/proto"
)

// Field is a stable, language-neutral context access path used by manifests
// and policy. It deliberately describes semantics rather than protobuf layout.
type Field string

const (
	FieldScopeTenant        Field = "scope.tenant"
	FieldScopeProject       Field = "scope.project"
	FieldCaller             Field = "caller"
	FieldScene              Field = "scene"
	FieldSubjectID          Field = "subject.id"
	FieldSubjectProfile     Field = "subject.profile"
	FieldAuthorizationRole  Field = "authorization.roles"
	FieldAuthorizationAdmin Field = "authorization.admin"
	FieldTrace              Field = "trace"
	FieldRequestDeadline    Field = "request.deadline"
	FieldRequestIdempotency Field = "request.idempotency"
	FieldGrantCredentials   Field = "grant.credentials"
	FieldBaggage            Field = "baggage"
	FieldPropagationPath    Field = "propagation.callPath"
)

var knownFields = map[Field]struct{}{
	FieldScopeTenant: {}, FieldScopeProject: {}, FieldCaller: {}, FieldScene: {},
	FieldSubjectID: {}, FieldSubjectProfile: {}, FieldAuthorizationRole: {},
	FieldAuthorizationAdmin: {}, FieldTrace: {}, FieldRequestDeadline: {},
	FieldRequestIdempotency: {}, FieldGrantCredentials: {}, FieldBaggage: {},
	FieldPropagationPath: {},
}

// AccessSet is an immutable-by-convention set. Constructors always return a
// fresh set and set operations never mutate their operands.
type AccessSet map[Field]struct{}

func NewAccess(fields ...Field) (AccessSet, error) {
	out := make(AccessSet, len(fields))
	for _, field := range fields {
		if _, ok := knownFields[field]; !ok {
			return nil, fmt.Errorf("未知 CallContext 访问字段 %q", field)
		}
		out[field] = struct{}{}
	}
	return out, nil
}

func MustAccess(fields ...Field) AccessSet {
	set, err := NewAccess(fields...)
	if err != nil {
		panic(err)
	}
	return set
}

func ParseAccess(fields []string) (AccessSet, error) {
	parsed := make([]Field, 0, len(fields))
	for _, field := range fields {
		parsed = append(parsed, Field(field))
	}
	return NewAccess(parsed...)
}

func AllFields() AccessSet {
	out := make(AccessSet, len(knownFields))
	for field := range knownFields {
		out[field] = struct{}{}
	}
	return out
}

func (s AccessSet) Has(field Field) bool { _, ok := s[field]; return ok }

func (s AccessSet) Clone() AccessSet {
	out := make(AccessSet, len(s))
	for field := range s {
		out[field] = struct{}{}
	}
	return out
}

func (s AccessSet) Strings() []string {
	out := make([]string, 0, len(s))
	for field := range s {
		out = append(out, string(field))
	}
	sort.Strings(out)
	return out
}

// Intersect returns the fields present in every supplied set. A nil/empty set
// is an explicit deny-all ceiling, not an omitted policy.
func Intersect(sets ...AccessSet) AccessSet {
	if len(sets) == 0 {
		return AccessSet{}
	}
	out := sets[0].Clone()
	for _, set := range sets[1:] {
		for field := range out {
			if !set.Has(field) {
				delete(out, field)
			}
		}
	}
	return out
}

func Union(sets ...AccessSet) AccessSet {
	out := AccessSet{}
	for _, set := range sets {
		for field := range set {
			out[field] = struct{}{}
		}
	}
	return out
}

// Projection controls the exact data disclosed to one audience.
type Projection struct {
	Fields          AccessSet
	BaggagePrefixes []string
}

func (p Projection) Validate() error {
	for field := range p.Fields {
		if _, ok := knownFields[field]; !ok {
			return fmt.Errorf("未知投影字段 %q", field)
		}
	}
	for _, prefix := range p.BaggagePrefixes {
		if prefix == "" || isReservedMetadataKey(prefix) {
			return fmt.Errorf("非法 baggage 前缀 %q", prefix)
		}
	}
	return nil
}

// EffectiveProjection applies every independent ceiling. Required fields must
// survive the complete intersection or the call fails closed.
func EffectiveProjection(required, optional AccessSet, baggage []string, ceilings ...AccessSet) (Projection, error) {
	requested := Union(required, optional)
	sets := append([]AccessSet{requested}, ceilings...)
	effective := Intersect(sets...)
	for field := range required {
		if !effective.Has(field) {
			return Projection{}, fmt.Errorf("CallContext 必需字段 %q 未获有效授权", field)
		}
	}
	p := Projection{Fields: effective, BaggagePrefixes: append([]string(nil), baggage...)}
	return p, p.Validate()
}

func projectWire(source *contractv1.CallContext, projection Projection) *contractv1.CallContext {
	out := &contractv1.CallContext{}
	if source == nil {
		return out
	}
	fields := projection.Fields
	if fields.Has(FieldScopeTenant) {
		out.TenantId = source.TenantId
	}
	if fields.Has(FieldScopeProject) && source.ProjectId != nil {
		value := source.GetProjectId()
		out.ProjectId = &value
	}
	if fields.Has(FieldCaller) && source.Caller != nil {
		out.Caller = proto.Clone(source.Caller).(*contractv1.Caller)
	}
	if fields.Has(FieldScene) {
		out.Scene = source.Scene
	}
	if fields.Has(FieldTrace) && source.Trace != nil {
		out.Trace = proto.Clone(source.Trace).(*contractv1.Trace)
	}
	if fields.Has(FieldRequestDeadline) && source.DeadlineUnixMs != nil {
		value := source.GetDeadlineUnixMs()
		out.DeadlineUnixMs = &value
	}
	if fields.Has(FieldRequestIdempotency) && source.IdempotencyKey != nil {
		value := source.GetIdempotencyKey()
		out.IdempotencyKey = &value
	}
	if fields.Has(FieldGrantCredentials) {
		out.Credentials = make([]*contractv1.CredentialRef, 0, len(source.Credentials))
		for _, ref := range source.Credentials {
			if ref != nil {
				out.Credentials = append(out.Credentials, proto.Clone(ref).(*contractv1.CredentialRef))
			}
		}
	}
	if fields.Has(FieldPropagationPath) {
		out.CallPath = append([]string(nil), source.CallPath...)
	}
	if fields.Has(FieldSubjectID) || fields.Has(FieldSubjectProfile) || fields.Has(FieldAuthorizationRole) || fields.Has(FieldAuthorizationAdmin) {
		principal := source.Principal
		if principal != nil {
			out.Principal = &contractv1.Principal{}
			if fields.Has(FieldSubjectID) {
				out.Principal.UserId = principal.UserId
				if principal.SessionId != nil {
					value := principal.GetSessionId()
					out.Principal.SessionId = &value
				}
			}
			if fields.Has(FieldSubjectProfile) {
				out.Principal.Username = principal.Username
			}
			if fields.Has(FieldAuthorizationAdmin) {
				out.Principal.IsAdmin = principal.IsAdmin
			}
			if fields.Has(FieldAuthorizationRole) {
				out.Principal.SystemRoles = append([]string(nil), principal.SystemRoles...)
				out.Principal.ProjectRoles = make(map[string]*contractv1.RoleList, len(principal.ProjectRoles))
				for project, roles := range principal.ProjectRoles {
					if roles != nil {
						out.Principal.ProjectRoles[project] = proto.Clone(roles).(*contractv1.RoleList)
					}
				}
			}
			// Principal tenant is a compatibility mirror, never a second source.
			if fields.Has(FieldScopeTenant) {
				out.Principal.TenantId = source.TenantId
			}
		}
	}
	if fields.Has(FieldBaggage) {
		for key, value := range source.Metadata {
			if isReservedMetadataKey(key) || !matchesPrefix(key, projection.BaggagePrefixes) {
				continue
			}
			if out.Metadata == nil {
				out.Metadata = map[string]string{}
			}
			out.Metadata[key] = value
		}
	}
	return out
}

func matchesPrefix(key string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasSuffix(prefix, "*") {
			if strings.HasPrefix(key, strings.TrimSuffix(prefix, "*")) {
				return true
			}
		} else if key == prefix {
			return true
		}
	}
	return false
}
