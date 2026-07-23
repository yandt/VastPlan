package platformcatalog

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
)

const maxCASAttempts = 8

func (s *Store) Prepare(ctx context.Context, request PrepareRequest) (Candidate, error) {
	normalized, err := normalizePrepareRequest(request)
	if err != nil {
		return Candidate{}, err
	}
	request = normalized
	return s.mutateCandidate(ctx, func(snapshot *persistedSnapshot) (Candidate, error) {
		if snapshot.Candidate != nil && snapshot.Candidate.CandidateID == request.CandidateID && snapshot.Candidate.RequestDigest == request.RequestDigest {
			if !reflect.DeepEqual(snapshot.Candidate.prepareRequest(), request) {
				return Candidate{}, ErrCatalogConflict
			}
			return *snapshot.Candidate, errNoMutation
		}
		if snapshot.Candidate != nil && (snapshot.Candidate.Status == CandidatePrepared || snapshot.Candidate.Status == CandidateActivated) {
			return Candidate{}, ErrCandidateLocked
		}
		if snapshot.Digest != request.ExpectedCatalogDigest {
			return Candidate{}, ErrCatalogConflict
		}
		nextCatalog, previousProfile, err := buildCandidateCatalog(snapshot.Catalog, request)
		if err != nil {
			return Candidate{}, err
		}
		candidateProfile, _, err := nextCatalog.Resolve(request.TenantID, request.DeploymentName)
		if err != nil {
			return Candidate{}, ErrCatalogConflict
		}
		now := time.Now().UTC()
		candidate := Candidate{
			CandidateID: request.CandidateID, RequestDigest: request.RequestDigest,
			TenantID: request.TenantID, DeploymentName: request.DeploymentName,
			ExpectedCatalogDigest: request.ExpectedCatalogDigest, PreviousProfile: previousProfile,
			NextProfile: candidateProfile, NextCatalogRevision: request.NextCatalogRevision,
			NextCatalogDigest: nextCatalog.Digest(), Status: CandidatePrepared,
			CreatedAt: now, UpdatedAt: now,
		}
		snapshot.Candidate = &candidate
		return candidate, nil
	})
}

func (s *Store) Activate(ctx context.Context, candidateID, requestDigest string) (Candidate, error) {
	return s.mutateCandidate(ctx, func(snapshot *persistedSnapshot) (Candidate, error) {
		candidate, err := requireCandidate(snapshot.Candidate, candidateID, requestDigest)
		if err != nil {
			return Candidate{}, err
		}
		if candidate.Status == CandidateActivated {
			return candidate, errNoMutation
		}
		if candidate.Status != CandidatePrepared || snapshot.Digest != candidate.ExpectedCatalogDigest {
			return Candidate{}, ErrInvalidTransition
		}
		next, _, err := buildCandidateCatalog(snapshot.Catalog, candidate.prepareRequest())
		if err != nil || next.Digest() != candidate.NextCatalogDigest {
			return Candidate{}, ErrCatalogConflict
		}
		snapshot.Catalog, snapshot.Digest = next, next.Digest()
		candidate.Status, candidate.UpdatedAt = CandidateActivated, time.Now().UTC()
		snapshot.Candidate = &candidate
		return candidate, nil
	})
}

func (s *Store) Finalize(ctx context.Context, candidateID, requestDigest string) (Candidate, error) {
	return s.transitionTerminal(ctx, candidateID, requestDigest, CandidateActivated, CandidateFinalized)
}

func (s *Store) Abort(ctx context.Context, candidateID, requestDigest string) (Candidate, error) {
	return s.transitionTerminal(ctx, candidateID, requestDigest, CandidatePrepared, CandidateAborted)
}

func (s *Store) Rollback(ctx context.Context, candidateID, requestDigest string) (Candidate, error) {
	return s.mutateCandidate(ctx, func(snapshot *persistedSnapshot) (Candidate, error) {
		candidate, err := requireCandidate(snapshot.Candidate, candidateID, requestDigest)
		if err != nil {
			return Candidate{}, err
		}
		if candidate.Status == CandidateRolledBack {
			return candidate, errNoMutation
		}
		if candidate.Status != CandidateActivated || snapshot.Digest != candidate.NextCatalogDigest {
			return Candidate{}, ErrInvalidTransition
		}
		rollback := cloneCatalog(snapshot.Catalog)
		rollback.Revision++
		if !replaceBinding(&rollback, candidate.TenantID, candidate.DeploymentName, candidate.PreviousProfile) {
			return Candidate{}, ErrCatalogConflict
		}
		rollback, err = backendcompositionv1.ValidateBackendPlatformCatalog(rollback)
		if err != nil {
			return Candidate{}, fmt.Errorf("构造 Backend Platform Catalog 回滚修订: %w", err)
		}
		snapshot.Catalog, snapshot.Digest = rollback, rollback.Digest()
		candidate.Status, candidate.RollbackCatalogDigest, candidate.UpdatedAt = CandidateRolledBack, snapshot.Digest, time.Now().UTC()
		snapshot.Candidate = &candidate
		return candidate, nil
	})
}

func (s *Store) Candidate(ctx context.Context, candidateID, requestDigest string) (Candidate, error) {
	snapshot, err := s.readPersisted(ctx)
	if err != nil {
		return Candidate{}, err
	}
	return requireCandidate(snapshot.Candidate, candidateID, requestDigest)
}

func (s *Store) transitionTerminal(ctx context.Context, candidateID, requestDigest string, from, to CandidateStatus) (Candidate, error) {
	return s.mutateCandidate(ctx, func(snapshot *persistedSnapshot) (Candidate, error) {
		candidate, err := requireCandidate(snapshot.Candidate, candidateID, requestDigest)
		if err != nil {
			return Candidate{}, err
		}
		if candidate.Status == to {
			return candidate, errNoMutation
		}
		if candidate.Status != from {
			return Candidate{}, ErrInvalidTransition
		}
		candidate.Status, candidate.UpdatedAt = to, time.Now().UTC()
		snapshot.Candidate = &candidate
		return candidate, nil
	})
}

var errNoMutation = errors.New("candidate mutation is already complete")

func (s *Store) mutateCandidate(ctx context.Context, mutate func(*persistedSnapshot) (Candidate, error)) (Candidate, error) {
	if s == nil || s.KV == nil || s.writer == nil || s.key == "" {
		return Candidate{}, errors.New("Backend Platform Catalog Store 未初始化")
	}
	for attempt := 0; attempt < maxCASAttempts; attempt++ {
		entry, err := s.writer.Get(ctx, s.key)
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return Candidate{}, ErrCatalogNotSeeded
		}
		if err != nil {
			return Candidate{}, fmt.Errorf("读取 Backend Platform Catalog CAS 快照: %w", err)
		}
		snapshot, err := parseSnapshot(entry.Value())
		if err != nil || snapshot.Catalog.ID != s.catalogID {
			return Candidate{}, errors.New("持久 Backend Platform Catalog 快照损坏或身份不匹配")
		}
		candidate, err := mutate(&snapshot)
		if errors.Is(err, errNoMutation) {
			return candidate, nil
		}
		if err != nil {
			return Candidate{}, err
		}
		raw, err := encodeState(snapshot)
		if err != nil {
			return Candidate{}, err
		}
		if _, err = s.writer.Update(ctx, s.key, raw, entry.Revision()); err == nil {
			return candidate, nil
		}
		if !errors.Is(err, jetstream.ErrKeyExists) {
			return Candidate{}, fmt.Errorf("CAS 更新 Backend Platform Catalog: %w", err)
		}
	}
	return Candidate{}, errors.New("Backend Platform Catalog CAS 竞争过多")
}
