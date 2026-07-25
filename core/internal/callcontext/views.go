package callcontext

import (
	"context"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/contextview"
)

type Views = contextview.Views

func (t Trusted) Views() Views { return contextview.FromWire(t.wire) }

func ReadOnlyViews(wire *contractv1.CallContext) Views { return contextview.FromWire(wire) }

// ContextHandle and HandleResolver reserve the zero-trust extension seam for
// future isolated runtimes. V1 has no resolver implementation: a handle is not
// accepted until a runtime supplies audience-bound, expiring resolution.
type ContextHandle string

type HandleResolver interface {
	Resolve(context.Context, ContextHandle, Projection) (Views, error)
}
