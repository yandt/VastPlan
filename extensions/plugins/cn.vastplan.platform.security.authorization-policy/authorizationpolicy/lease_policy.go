package authorizationpolicy

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
)

// SnapshotLeasePolicy is selected once by the trusted composition root. The
// policy service and its controller consume this interface; neither layer
// branches on development/production mode.
type SnapshotLeasePolicy interface {
	Audiences() []string
	SnapshotTTL() time.Duration
	RenewalLead() time.Duration
	RenewManagedBindings(*State, time.Time) []string
}

type FixedSnapshotLeasePolicy struct {
	audiences                 []string
	snapshotTTL               time.Duration
	renewalLead               time.Duration
	managedBindingCreators    map[string]struct{}
	managedBindingTTL         time.Duration
	managedBindingRenewalLead time.Duration
}

type SnapshotLeasePolicyOptions struct {
	Audiences                 []string
	SnapshotTTL               time.Duration
	RenewalLead               time.Duration
	ManagedBindingCreators    []string
	ManagedBindingTTL         time.Duration
	ManagedBindingRenewalLead time.Duration
}

func NewFixedSnapshotLeasePolicy(options SnapshotLeasePolicyOptions) (FixedSnapshotLeasePolicy, error) {
	audiences, err := normalizedLeaseValues(options.Audiences, "Snapshot audience")
	if err != nil || len(audiences) == 0 || len(audiences) > 32 {
		return FixedSnapshotLeasePolicy{}, errors.New("Snapshot Lease Policy audience 数量或格式无效")
	}
	if options.SnapshotTTL < time.Second || options.SnapshotTTL > 24*time.Hour {
		return FixedSnapshotLeasePolicy{}, errors.New("Snapshot Lease Policy TTL 必须在 1 秒到 24 小时之间")
	}
	if options.RenewalLead <= 0 || options.RenewalLead >= options.SnapshotTTL {
		return FixedSnapshotLeasePolicy{}, errors.New("Snapshot Lease Policy 续签提前量必须大于 0 且小于 TTL")
	}
	creators, err := normalizedLeaseValues(options.ManagedBindingCreators, "Managed Binding creator")
	if err != nil {
		return FixedSnapshotLeasePolicy{}, err
	}
	if len(creators) > 0 && (options.ManagedBindingTTL < time.Minute || options.ManagedBindingTTL > 365*24*time.Hour || options.ManagedBindingRenewalLead <= 0 || options.ManagedBindingRenewalLead >= options.ManagedBindingTTL) {
		return FixedSnapshotLeasePolicy{}, errors.New("Managed Binding Lease 的 TTL 或续签提前量无效")
	}
	managed := make(map[string]struct{}, len(creators))
	for _, creator := range creators {
		managed[creator] = struct{}{}
	}
	return FixedSnapshotLeasePolicy{
		audiences: audiences, snapshotTTL: options.SnapshotTTL, renewalLead: options.RenewalLead,
		managedBindingCreators: managed, managedBindingTTL: options.ManagedBindingTTL,
		managedBindingRenewalLead: options.ManagedBindingRenewalLead,
	}, nil
}

func (p FixedSnapshotLeasePolicy) Audiences() []string        { return append([]string(nil), p.audiences...) }
func (p FixedSnapshotLeasePolicy) SnapshotTTL() time.Duration { return p.snapshotTTL }
func (p FixedSnapshotLeasePolicy) RenewalLead() time.Duration { return p.renewalLead }

func (p FixedSnapshotLeasePolicy) RenewManagedBindings(state *State, now time.Time) []string {
	if state == nil || len(p.managedBindingCreators) == 0 {
		return nil
	}
	now = now.UTC()
	threshold := now.Add(p.managedBindingRenewalLead)
	renewed := make([]string, 0)
	for index := range state.Bindings {
		binding := &state.Bindings[index]
		if binding.State != StatePublished || binding.Revision != 1 {
			continue
		}
		if _, managed := p.managedBindingCreators[binding.CreatedBy]; !managed || binding.ExpiresAt.After(threshold) {
			continue
		}
		binding.ExpiresAt = now.Add(p.managedBindingTTL)
		binding.UpdatedAt = now
		renewed = append(renewed, binding.ID)
	}
	sort.Strings(renewed)
	return renewed
}

func snapshotLeaseRenewalRequired(state State, lease SnapshotLeasePolicy, now time.Time) (bool, error) {
	if state.CurrentSnapshot == nil {
		return false, nil
	}
	snapshot := state.CurrentSnapshot
	if snapshot.Revision != state.PolicyRevision || snapshot.Policy.CatalogDigest != state.Catalog.Digest {
		return false, fmt.Errorf("权威 Policy 状态与 CurrentSnapshot 不一致: stateRevision=%d snapshotRevision=%d stateCatalog=%s snapshotCatalog=%s", state.PolicyRevision, snapshot.Revision, state.Catalog.Digest, snapshot.Policy.CatalogDigest)
	}
	if !sameStrings(snapshot.Audience, lease.Audiences()) || snapshot.ExpiresAt.Sub(snapshot.IssuedAt) != lease.SnapshotTTL() {
		return true, nil
	}
	now = now.UTC()
	return now.Before(snapshot.NotBefore) || !snapshot.ExpiresAt.After(now.Add(lease.RenewalLead())), nil
}

func nextSnapshotRenewal(snapshot *authorizationv1.PolicySnapshot, lease SnapshotLeasePolicy, now time.Time) time.Time {
	if snapshot == nil {
		// 未发布快照不是健康态，但也不能通过后台 Lease 控制器隐式发布
		// 首个业务 revision；按失败重试窗口等待显式发布，避免零延迟空转。
		return now.UTC().Add(leaseRetryDelay)
	}
	next := snapshot.ExpiresAt.Add(-lease.RenewalLead())
	if next.Before(now) {
		return now.UTC()
	}
	return next.UTC()
}

func normalizedLeaseValues(values []string, label string) ([]string, error) {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || trimmed != value || len(trimmed) > 256 {
			return nil, fmt.Errorf("%s %q 格式无效", label, value)
		}
		if _, duplicate := seen[trimmed]; duplicate {
			return nil, fmt.Errorf("%s %q 重复", label, value)
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	sort.Strings(result)
	return result, nil
}

func sameStrings(left, right []string) bool {
	a := append([]string(nil), left...)
	b := append([]string(nil), right...)
	sort.Strings(a)
	sort.Strings(b)
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}
