// Package dynamicgo defines the narrow, first-party-only ABI exchanged by a
// signed Go module and its trusted Runtime Host. It exposes no Host internals.
package dynamicgo

import (
	"context"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	pluginhostv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/pluginhost/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

const (
	ABIV1  = "vastplan.dynamic-go.v1"
	Symbol = "VastPlanDynamicGo"
)

type Host interface {
	Call(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)
}

type Handler func(context.Context, Host, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)

type Contribution struct {
	ExtensionPoint string
	ID             string
	Priority       int32
	Descriptor     []byte
	Handlers       map[string]Handler
}

type MigrationOperation string

const (
	MigrationPrepare  MigrationOperation = "prepare"
	MigrationCommit   MigrationOperation = "commit"
	MigrationRollback MigrationOperation = "rollback"
)

type MigrationCommand struct {
	Operation     MigrationOperation
	TransactionID string
	From          pluginv1.StateIdentity
	To            pluginv1.StateIdentity
}

type Lifecycle func(context.Context, pluginhostv1.Lifecycle_Op, *MigrationCommand) error

type Plugin struct {
	ID            string
	Version       string
	Contributions []Contribution
	Lifecycle     Lifecycle
}

type Module struct {
	ABI              string
	BuildFingerprint string
	Plugin           Plugin
}

type Entrypoint func() Module
