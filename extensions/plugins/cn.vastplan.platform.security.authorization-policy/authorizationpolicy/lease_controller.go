package authorizationpolicy

import (
	"context"
	"errors"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const leaseRetryDelay = 2 * time.Second

// ReconcileSnapshotLease is the only autonomous path that renews signed
// authorization material. It uses the same authoritative Store and publication
// workflow as management mutations; callers merely trigger the protocol.
func (s *Service) ReconcileSnapshotLease(ctx context.Context, host sdk.Host, call *contractv1.CallContext) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	store := s.store
	var err error
	if s.storeFactory != nil {
		store, err = s.storeFactory(ctx, host, call)
		if err != nil {
			return time.Time{}, err
		}
	}
	if err := s.initialize(store); err != nil {
		return time.Time{}, err
	}
	state, err := store.Load()
	if err != nil {
		return time.Time{}, err
	}
	return nextSnapshotRenewal(state.CurrentSnapshot, s.leasePolicy, s.now().UTC()), nil
}

// RunSnapshotLeaseController waits until the exact renewal boundary. It does
// not poll healthy state. Failures use bounded retries so a transient shared
// store outage cannot silently let the current lease expire.
func (s *Service) RunSnapshotLeaseController(ctx context.Context, host sdk.Host, call *contractv1.CallContext, next time.Time, report func(error)) {
	for {
		delay := time.Until(next)
		if delay < 0 {
			delay = 0
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
		renewAt, err := s.ReconcileSnapshotLease(ctx, host, call)
		if err != nil {
			if report != nil && !errors.Is(err, context.Canceled) {
				report(err)
			}
			next = time.Now().Add(leaseRetryDelay)
			continue
		}
		next = renewAt
	}
}
