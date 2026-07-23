// Package sharedstatev1 defines the identity-free JSON wire contract for the
// trusted kernel Shared State service.
package sharedstatev1

import "time"

const (
	Protocol            = "state.shared.v1"
	KernelServicePrefix = "kernel.state.shared."
	// FencedKernelServicePrefix exposes the same identity-free request shapes,
	// but mutating calls additionally require current host-only leader evidence.
	// It is a separate capability so active-active writers keep their existing
	// CAS contract and cannot accidentally become leader-only.
	FencedKernelServicePrefix = "kernel.state.shared.fenced."
	OperationGet              = "get"
	OperationCreate           = "create"
	OperationUpdate           = "update"
	OperationDelete           = "delete"
	OperationList             = "list"
)

func KernelService(operation string) string { return KernelServicePrefix + operation }

func FencedKernelService(operation string) string { return FencedKernelServicePrefix + operation }

type KeyRequest struct {
	Scope     string `json:"scope"`
	Namespace string `json:"namespace"`
	Key       string `json:"key"`
}

type WriteRequest struct {
	Scope            string `json:"scope"`
	Namespace        string `json:"namespace"`
	Key              string `json:"key"`
	Value            string `json:"value"`
	ExpectedRevision uint64 `json:"expectedRevision,omitempty"`
}

type DeleteRequest struct {
	Scope            string `json:"scope"`
	Namespace        string `json:"namespace"`
	Key              string `json:"key"`
	ExpectedRevision uint64 `json:"expectedRevision"`
}

type ListRequest struct {
	Scope      string `json:"scope"`
	Namespace  string `json:"namespace"`
	Prefix     string `json:"prefix,omitempty"`
	Limit      int    `json:"limit"`
	PageCursor string `json:"pageCursor,omitempty"`
}

type Entry struct {
	Protocol  string    `json:"protocol"`
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	Revision  uint64    `json:"revision"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Page struct {
	Protocol       string  `json:"protocol"`
	Items          []Entry `json:"items"`
	NextPageCursor string  `json:"nextPageCursor,omitempty"`
}

type Ack struct {
	Protocol string `json:"protocol"`
}
