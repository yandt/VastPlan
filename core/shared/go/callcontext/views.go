package callcontext

import (
	"context"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

type ScopeView struct{ tenant, project string }

func (v ScopeView) TenantID() string  { return v.tenant }
func (v ScopeView) ProjectID() string { return v.project }

type CallerView struct {
	kind      contractv1.CallerKind
	id, scene string
}

func (v CallerView) Kind() contractv1.CallerKind { return v.kind }
func (v CallerView) ID() string                  { return v.id }
func (v CallerView) Scene() string               { return v.scene }

type SubjectView struct{ id, username, session string }

func (v SubjectView) ID() string        { return v.id }
func (v SubjectView) Username() string  { return v.username }
func (v SubjectView) SessionID() string { return v.session }

type AuthorizationView struct {
	admin        bool
	systemRoles  []string
	projectRoles map[string][]string
}

func (v AuthorizationView) IsAdmin() bool         { return v.admin }
func (v AuthorizationView) SystemRoles() []string { return append([]string(nil), v.systemRoles...) }
func (v AuthorizationView) ProjectRoles(project string) []string {
	return append([]string(nil), v.projectRoles[project]...)
}

type TraceView struct{ traceID, spanID, parentSpanID string }

func (v TraceView) TraceID() string      { return v.traceID }
func (v TraceView) SpanID() string       { return v.spanID }
func (v TraceView) ParentSpanID() string { return v.parentSpanID }

type RequestControlView struct {
	deadlineUnixMs *int64
	idempotencyKey string
	callPath       []string
}

func (v RequestControlView) DeadlineUnixMs() (int64, bool) {
	if v.deadlineUnixMs == nil {
		return 0, false
	}
	return *v.deadlineUnixMs, true
}
func (v RequestControlView) IdempotencyKey() string { return v.idempotencyKey }
func (v RequestControlView) CallPath() []string     { return append([]string(nil), v.callPath...) }

type GrantView struct{ credentials []Credential }
type Credential struct{ Name, Scope string }

func (v GrantView) Credentials() []Credential { return append([]Credential(nil), v.credentials...) }

type BaggageView struct{ values map[string]string }

func (v BaggageView) Get(key string) (string, bool) { value, ok := v.values[key]; return value, ok }
func (v BaggageView) All() map[string]string {
	out := make(map[string]string, len(v.values))
	for k, value := range v.values {
		out[k] = value
	}
	return out
}

type Views struct {
	Scope         ScopeView
	Caller        CallerView
	Subject       SubjectView
	Authorization AuthorizationView
	Trace         TraceView
	Request       RequestControlView
	Grant         GrantView
	Baggage       BaggageView
}

func (t Trusted) Views() Views { return viewsFromWire(t.wire) }

// ReadOnlyViews creates defensive views over an already projected wire
// context. It does not confer trust and is intended for SDK/business readers.
func ReadOnlyViews(wire *contractv1.CallContext) Views { return viewsFromWire(wire) }

func viewsFromWire(w *contractv1.CallContext) Views {
	if w == nil {
		w = &contractv1.CallContext{}
	}
	views := Views{
		Scope:         ScopeView{tenant: w.TenantId, project: w.GetProjectId()},
		Caller:        CallerView{kind: w.GetCaller().GetKind(), id: w.GetCaller().GetId(), scene: w.Scene},
		Subject:       SubjectView{id: w.GetPrincipal().GetUserId(), username: w.GetPrincipal().GetUsername(), session: w.GetPrincipal().GetSessionId()},
		Authorization: AuthorizationView{admin: w.GetPrincipal().GetIsAdmin(), systemRoles: append([]string(nil), w.GetPrincipal().GetSystemRoles()...), projectRoles: map[string][]string{}},
		Trace:         TraceView{traceID: w.GetTrace().GetTraceId(), spanID: w.GetTrace().GetSpanId(), parentSpanID: w.GetTrace().GetParentSpanId()},
		Request:       RequestControlView{idempotencyKey: w.GetIdempotencyKey(), callPath: append([]string(nil), w.CallPath...)},
		Baggage:       BaggageView{values: map[string]string{}},
	}
	if w.DeadlineUnixMs != nil {
		value := w.GetDeadlineUnixMs()
		views.Request.deadlineUnixMs = &value
	}
	for project, roles := range w.GetPrincipal().GetProjectRoles() {
		views.Authorization.projectRoles[project] = append([]string(nil), roles.GetRoles()...)
	}
	for _, ref := range w.Credentials {
		if ref != nil {
			views.Grant.credentials = append(views.Grant.credentials, Credential{Name: ref.Name, Scope: ref.GetScope()})
		}
	}
	for key, value := range w.Metadata {
		if !isReservedMetadataKey(key) {
			views.Baggage.values[key] = value
		}
	}
	return views
}

// ContextHandle and HandleResolver reserve the zero-trust extension seam for
// future isolated runtimes. V1 has no resolver implementation: a handle is not
// accepted until a runtime supplies audience-bound, expiring resolution.
type ContextHandle string

type HandleResolver interface {
	Resolve(context.Context, ContextHandle, Projection) (Views, error)
}
