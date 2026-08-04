// Package sharedstatesqlv1 defines the host-only wire adapter between the
// kernel Shared State SPI and the SQL module in Database Runtime.
package sharedstatesqlv1

import "time"

const (
	Capability      = "foundation.state.shared.sql"
	ContractVersion = "1.0.0"
	OperationGet    = "get"
	OperationCreate = "create"
	OperationUpdate = "update"
	OperationDelete = "delete"
	OperationList   = "list"

	ErrorInvalid     = "state.shared.sql.invalid"
	ErrorNotFound    = "state.shared.sql.not_found"
	ErrorConflict    = "state.shared.sql.conflict"
	ErrorUnavailable = "state.shared.sql.unavailable"
)

type Scope struct {
	Kind         string `json:"kind"`
	TenantID     string `json:"tenantId,omitempty"`
	PluginID     string `json:"pluginId"`
	RuntimeScope string `json:"runtimeScope"`
	Namespace    string `json:"namespace"`
}

type KeyRequest struct {
	Scope Scope  `json:"scope"`
	Key   string `json:"key"`
}

type WriteRequest struct {
	Scope            Scope  `json:"scope"`
	Key              string `json:"key"`
	ValueBase64      string `json:"valueBase64"`
	ExpectedRevision uint64 `json:"expectedRevision,omitempty"`
}

type DeleteRequest struct {
	Scope            Scope  `json:"scope"`
	Key              string `json:"key"`
	ExpectedRevision uint64 `json:"expectedRevision"`
}

type ListRequest struct {
	Scope  Scope  `json:"scope"`
	Prefix string `json:"prefix,omitempty"`
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor,omitempty"`
}

type Entry struct {
	Key         string    `json:"key"`
	ValueBase64 string    `json:"valueBase64"`
	Revision    uint64    `json:"revision"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Page struct {
	Items      []Entry `json:"items"`
	NextCursor string  `json:"nextCursor,omitempty"`
}

type Ack struct{}
