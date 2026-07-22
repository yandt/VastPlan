package authorizationpolicy

import (
	"errors"
	"fmt"
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
)

func (s *Service) revoke(subject string, request RevokeRequest, decodeErr error) (any, error) {
	if decodeErr != nil {
		return nil, decodeErr
	}
	if err := validateManagedID(request.ID, "Revocation ID"); err != nil {
		return nil, err
	}
	if err := validateManagedID(request.TargetID, "Revocation target"); err != nil {
		return nil, err
	}
	if request.Kind != "subject" && request.Kind != "binding" && request.Kind != "role" {
		return nil, errors.New("Revocation kind 只允许 subject/binding/role")
	}
	if request.ReasonCode == "" || request.EffectiveAt.IsZero() {
		return nil, errors.New("Revocation 必须声明生效时间与 reasonCode")
	}
	state, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	if err := ensureExpected(state, request.ExpectedGeneration); err != nil {
		return nil, err
	}
	for _, existing := range state.Revocations {
		if existing.ID == request.ID {
			return nil, fmt.Errorf("Revocation ID 已存在")
		}
	}
	state.RevocationRevision++
	revocation := authorizationv1.Revocation{ID: request.ID, Revision: state.RevocationRevision, Kind: request.Kind, TargetID: request.TargetID, EffectiveAt: request.EffectiveAt.UTC(), ReasonCode: request.ReasonCode}
	state.Revocations = append(state.Revocations, revocation)
	publication, committed, err := s.publishState(
		state,
		request.ExpectedGeneration,
		s.defaultAudience,
		s.defaultTTL,
		s.audit(subject, "revokeAndPublish", request.Kind, request.TargetID, revocation.Revision, request.ReasonCode),
	)
	if err != nil {
		return nil, err
	}
	return struct {
		Revocation  authorizationv1.Revocation `json:"revocation"`
		Publication SnapshotPublication        `json:"publication"`
		Generation  uint64                     `json:"generation"`
	}{revocation, publication, committed.Generation}, nil
}

func (s *Service) publishSnapshot(subject string, request PublishSnapshotRequest, decodeErr error) (any, error) {
	if decodeErr != nil {
		return nil, decodeErr
	}
	if request.Reason == "" {
		return nil, errors.New("发布 Policy Snapshot 必须说明原因")
	}
	state, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	if err := ensureExpected(state, request.ExpectedGeneration); err != nil {
		return nil, err
	}
	audience := append([]string(nil), request.Audience...)
	if len(audience) == 0 {
		audience = append([]string(nil), s.defaultAudience...)
	}
	ttl := s.defaultTTL
	if request.TTLSeconds != 0 {
		ttl = time.Duration(request.TTLSeconds) * time.Second
	}
	publication, committed, err := s.publishState(
		state,
		request.ExpectedGeneration,
		audience,
		ttl,
		s.audit(subject, "publishSnapshot", "policy", "current", state.PolicyRevision+1, request.Reason),
	)
	if err != nil {
		return nil, err
	}
	return struct {
		Publication SnapshotPublication `json:"publication"`
		Generation  uint64              `json:"generation"`
	}{publication, committed.Generation}, nil
}

// publishState is the only path that advances PolicyRevision. Revocation uses
// this path too, so an acknowledged emergency revoke always has a freshly
// signed snapshot available to every local enforcer.
func (s *Service) publishState(state State, expected uint64, audience []string, ttl time.Duration, event AuditEvent) (SnapshotPublication, State, error) {
	state.PolicyRevision++
	snapshot, err := CompileSnapshot(state, audience, s.now(), ttl)
	if err != nil {
		return SnapshotPublication{}, State{}, err
	}
	publication, err := s.signer.Sign(snapshot)
	if err != nil {
		return SnapshotPublication{}, State{}, err
	}
	if err := s.snapshotWriter.Write(publication.Snapshot); err != nil {
		return SnapshotPublication{}, State{}, fmt.Errorf("写入 Policy Snapshot: %w", err)
	}
	state.CurrentSnapshot = &snapshot
	event.Revision = snapshot.Revision
	committed, err := s.commit(state, expected, event)
	if err != nil {
		return SnapshotPublication{}, State{}, err
	}
	return publication, committed, nil
}
