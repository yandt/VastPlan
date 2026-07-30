package versionworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

type memoryLedger struct {
	versions         map[string]versioningv1.VersionRecord
	idempotency      map[string]versioningv1.PutVersionResult
	heads            map[string]versioningv1.Head
	nextSequence     uint64
	failPutOnce      bool
	loseHeadResponse bool
}

func newMemoryLedger() *memoryLedger {
	return &memoryLedger{versions: map[string]versioningv1.VersionRecord{}, idempotency: map[string]versioningv1.PutVersionResult{}, heads: map[string]versioningv1.Head{}}
}

func (l *memoryLedger) PutVersion(_ context.Context, request versioningv1.PutVersionRequest) (versioningv1.PutVersionResult, error) {
	if l.failPutOnce {
		l.failPutOnce = false
		return versioningv1.PutVersionResult{}, ledgerError(versioningv1.ErrorProviderUnavailable, true, errors.New("temporary"))
	}
	if result, ok := l.idempotency[request.IdempotencyKey]; ok {
		result.Reused = true
		return result, nil
	}
	if l.nextSequence > 0 && len(request.Parents) == 0 {
		return versioningv1.PutVersionResult{}, ledgerError(versioningv1.ErrorConflict, false, errors.New("missing parent"))
	}
	for _, parent := range request.Parents {
		if stored, ok := l.versions[parent.VersionID]; !ok || stored.Ref != parent {
			return versioningv1.PutVersionResult{}, ledgerError(versioningv1.ErrorNotFound, false, errors.New("parent missing"))
		}
	}
	l.nextSequence++
	versionID, err := versioningv1.DeriveVersionID("tenant-a", request.Stream, request.IdempotencyKey)
	if err != nil {
		return versioningv1.PutVersionResult{}, err
	}
	digest, err := versioningv1.ContentDigest(request.Content)
	if err != nil {
		return versioningv1.PutVersionResult{}, err
	}
	record := versioningv1.VersionRecord{
		Protocol: versioningv1.Protocol,
		Ref:      versioningv1.VersionRef{Stream: request.Stream, VersionID: versionID, Sequence: l.nextSequence, ContentDigest: digest},
		Parents:  append([]versioningv1.VersionRef(nil), request.Parents...), Content: append(json.RawMessage(nil), request.Content...),
		Message: request.Message, Labels: cloneLabels(request.Labels), ActorID: "plugin:test", CreatedAt: time.Date(2026, 7, 30, 12, 0, int(l.nextSequence), 0, time.UTC),
	}
	result := versioningv1.PutVersionResult{Version: record}
	l.versions[versionID] = record
	l.idempotency[request.IdempotencyKey] = result
	return result, nil
}

func (l *memoryLedger) GetVersion(_ context.Context, request versioningv1.GetVersionRequest) (versioningv1.GetVersionResult, error) {
	record, ok := l.versions[request.Ref.VersionID]
	if !ok || record.Ref != request.Ref {
		return versioningv1.GetVersionResult{}, ledgerError(versioningv1.ErrorNotFound, false, errors.New("version missing"))
	}
	return versioningv1.GetVersionResult{Version: record}, nil
}

func (l *memoryLedger) GetHead(_ context.Context, request versioningv1.GetHeadRequest) (versioningv1.GetHeadResult, error) {
	head, ok := l.heads[headKey(request.Stream, request.Name)]
	if !ok {
		return versioningv1.GetHeadResult{}, ledgerError(versioningv1.ErrorNotFound, false, errors.New("head missing"))
	}
	return versioningv1.GetHeadResult{Head: head}, nil
}

func (l *memoryLedger) CreateHead(_ context.Context, request versioningv1.CreateHeadRequest) (versioningv1.CreateHeadResult, error) {
	key := headKey(request.Stream, request.Name)
	if head, ok := l.heads[key]; ok {
		if head.Target == request.Target {
			return versioningv1.CreateHeadResult{Head: head, Reused: true}, nil
		}
		return versioningv1.CreateHeadResult{}, ledgerError(versioningv1.ErrorConflict, false, errors.New("head exists"))
	}
	head := versioningv1.Head{Protocol: versioningv1.Protocol, Stream: request.Stream, Name: request.Name, Target: request.Target, Revision: 1, UpdatedAt: time.Date(2026, 7, 30, 12, 1, 0, 0, time.UTC)}
	l.heads[key] = head
	if l.loseHeadResponse {
		l.loseHeadResponse = false
		return versioningv1.CreateHeadResult{}, ledgerError(versioningv1.ErrorProviderUnavailable, true, errors.New("response lost"))
	}
	return versioningv1.CreateHeadResult{Head: head}, nil
}

func (l *memoryLedger) MoveHead(_ context.Context, request versioningv1.MoveHeadRequest) (versioningv1.MoveHeadResult, error) {
	key := headKey(request.Stream, request.Name)
	head, ok := l.heads[key]
	if !ok {
		return versioningv1.MoveHeadResult{}, ledgerError(versioningv1.ErrorNotFound, false, errors.New("head missing"))
	}
	if head.Revision != request.ExpectedRevision {
		return versioningv1.MoveHeadResult{}, ledgerError(versioningv1.ErrorConflict, false, errors.New("head conflict"))
	}
	head.Target, head.Revision, head.UpdatedAt = request.Target, head.Revision+1, head.UpdatedAt.Add(time.Second)
	l.heads[key] = head
	if l.loseHeadResponse {
		l.loseHeadResponse = false
		return versioningv1.MoveHeadResult{}, ledgerError(versioningv1.ErrorProviderUnavailable, true, errors.New("response lost"))
	}
	return versioningv1.MoveHeadResult{Head: head}, nil
}

func headKey(stream versioningv1.StreamKey, name string) string {
	return stream.Namespace + "/" + stream.StreamID + "/" + name
}
