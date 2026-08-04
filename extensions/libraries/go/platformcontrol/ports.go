// Package platformcontrol defines the neutral ports shared by the trusted
// Platform Control workflow and a Database Runtime bootstrap adapter.
//
// It intentionally contains no profile persistence, secret provider, database
// driver, connection pool implementation, or bootstrap workflow ordering.
package platformcontrol

import (
	"context"

	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

// SecretSource lends secret material only for the duration of use.
type SecretSource interface {
	WithSecret(context.Context, func([]byte) error) error
}

// ManagedStore is a Shared State provider that owns disposable resources such
// as a Database Runtime connection pool.
type ManagedStore interface {
	sharedstate.Store
	Close() error
}

// Bootstrapper is implemented by a trusted Database Runtime adapter. The
// caller owns workflow ordering but never imports a driver or pool type.
type Bootstrapper interface {
	Test(context.Context, platformcontrolv1.Profile, SecretSource) error
	Initialize(context.Context, platformcontrolv1.Profile, SecretSource) (ManagedStore, error)
	Open(context.Context, platformcontrolv1.Profile, SecretSource) (ManagedStore, error)
}
