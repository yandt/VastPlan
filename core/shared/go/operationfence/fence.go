// Package operationfence carries host-only leader evidence into trusted
// kernel callbacks. Plugins cannot construct or read this context value.
package operationfence

import (
	"context"
	"errors"
	"strings"
)

const SchemaVersion = 1

// Evidence proves that the local runtime host currently owns one logical
// service leader lease. Token must never be copied into CallContext or logs.
type Evidence struct {
	LogicalService string
	UnitID         string
	Epoch          uint64
	Token          string
}

func (e Evidence) Validate() error {
	if strings.TrimSpace(e.LogicalService) == "" || strings.TrimSpace(e.UnitID) == "" || e.Epoch == 0 || strings.TrimSpace(e.Token) == "" || len(e.LogicalService) > 256 || len(e.UnitID) > 256 || len(e.Token) > 256 {
		return errors.New("操作 fencing evidence 无效")
	}
	return nil
}

// Fence is passed only between trusted kernel adapters. OperationID is a
// stable business checkpoint (for example a Bootstrap Job ID), not a bearer.
type Fence struct {
	SchemaVersion  int    `json:"schemaVersion"`
	LogicalService string `json:"logicalService"`
	UnitID         string `json:"unitId"`
	Epoch          uint64 `json:"epoch"`
	Token          string `json:"token"`
	OperationID    string `json:"operationId"`
}

func (f Fence) Validate() error {
	if f.SchemaVersion != SchemaVersion || strings.TrimSpace(f.OperationID) == "" || len(f.OperationID) > 256 {
		return errors.New("操作 fence 无效")
	}
	return (Evidence{LogicalService: f.LogicalService, UnitID: f.UnitID, Epoch: f.Epoch, Token: f.Token}).Validate()
}

func (e Evidence) ForOperation(operationID string) (Fence, error) {
	fence := Fence{SchemaVersion: SchemaVersion, LogicalService: e.LogicalService, UnitID: e.UnitID, Epoch: e.Epoch, Token: e.Token, OperationID: operationID}
	return fence, fence.Validate()
}

// Provider is owned by the trusted runtime. Current returns false immediately
// after lease loss; callers must not cache successful results across calls.
type Provider interface{ Current() (Evidence, bool) }

type contextKey struct{}

func WithEvidence(ctx context.Context, evidence Evidence) (context.Context, error) {
	if err := evidence.Validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contextKey{}, evidence), nil
}

func FromContext(ctx context.Context) (Evidence, bool) {
	if ctx == nil {
		return Evidence{}, false
	}
	evidence, ok := ctx.Value(contextKey{}).(Evidence)
	return evidence, ok && evidence.Validate() == nil
}
