// Package contentstaging manages lease-bound streamed content before it enters
// immutable version manifests.
package contentstaging

import (
	"errors"
	"strings"
	"time"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
)

const (
	PluginID      = "cn.vastplan.foundation.versioning.content-staging"
	PluginVersion = "0.1.0"
)

type Scope struct {
	TenantID string `json:"tenantId"`
	ActorID  string `json:"actorId"`
}

func (s Scope) Validate() error {
	if versioningv1.ValidateVersionIdentityTenant(s.TenantID) != nil || strings.TrimSpace(s.ActorID) == "" || len(s.ActorID) > 160 {
		return errors.New("Content Staging tenant 或 actor 无效")
	}
	return nil
}

type Limits struct {
	MaxFileBytes              int64
	MaxTenantBytes            int64
	MaxTotalBytes             int64
	MaxActiveUploadsPerTenant int
	MaxLeaseSeconds           int
	TerminalRetention         time.Duration
}

func (l Limits) Validate() error {
	if l.MaxFileBytes <= 0 || l.MaxFileBytes > stagingv1.MaximumDeclaredFileBytes || l.MaxTenantBytes < l.MaxFileBytes || l.MaxTotalBytes < l.MaxTenantBytes {
		return errors.New("Content Staging 字节配额无效")
	}
	if l.MaxActiveUploadsPerTenant <= 0 || l.MaxActiveUploadsPerTenant > 10000 || l.MaxLeaseSeconds < stagingv1.MinimumLeaseSeconds || l.MaxLeaseSeconds > stagingv1.MaximumLeaseSeconds {
		return errors.New("Content Staging 并发或 Lease 配额无效")
	}
	if l.TerminalRetention < 0 || l.TerminalRetention > 30*24*time.Hour {
		return errors.New("Content Staging 终态保留时间无效")
	}
	return nil
}

type uploadRecord struct {
	FormatVersion     int                          `json:"formatVersion"`
	Owner             Scope                        `json:"owner"`
	BeginDigest       string                       `json:"beginDigest"`
	SessionRevision   uint64                       `json:"sessionRevision"`
	BeginLeaseSeconds int                          `json:"beginLeaseSeconds"`
	Upload            stagingv1.UploadLease        `json:"upload"`
	ActualDigest      string                       `json:"actualDigest,omitempty"`
	Written           bool                         `json:"written,omitempty"`
	Content           *stagingv1.ContentDescriptor `json:"content,omitempty"`
	FailureCode       string                       `json:"failureCode,omitempty"`
}

func (r uploadRecord) result() stagingv1.UploadStatusResult {
	result := stagingv1.UploadStatusResult{Upload: r.Upload, FailureCode: r.FailureCode}
	if r.Content != nil {
		content := *r.Content
		result.Content = &content
	}
	return result
}

func cloneRecord(record uploadRecord) uploadRecord {
	copy := record
	if record.Content != nil {
		content := *record.Content
		copy.Content = &content
	}
	return copy
}

func protectedState(state string) bool {
	switch state {
	case stagingv1.StatePending, stagingv1.StateUploading, stagingv1.StateVerifying, stagingv1.StateReady:
		return true
	default:
		return false
	}
}

func activeUploadState(state string) bool {
	return state == stagingv1.StatePending || state == stagingv1.StateUploading || state == stagingv1.StateVerifying
}

func terminalState(state string) bool {
	return state == stagingv1.StateRejected || state == stagingv1.StateAborted || state == stagingv1.StateExpired
}

func validSHA256(value string) bool {
	return len(value) == 64 && isLowerHex(value)
}

func validateUploadID(value string) error {
	return stagingv1.ValidateUploadStatusRequest(stagingv1.UploadStatusRequest{UploadID: value})
}
