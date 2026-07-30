package versionledger

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"time"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

type memoryStreamKey struct {
	tenant, namespace, streamID string
}

type memoryStream struct {
	sequence      uint64
	versions      map[string]versioningv1.VersionRecord
	idempotency   map[string]string
	heads         map[string]versioningv1.Head
	headRevisions map[string]uint64
	tags          map[string]versioningv1.Tag
}

// MemoryProvider is a deterministic conformance Provider for tests and
// development wiring. It is never registered from production configuration.
type MemoryProvider struct {
	mu      sync.RWMutex
	streams map[memoryStreamKey]*memoryStream
	now     func() time.Time
}

func NewMemoryProvider() *MemoryProvider {
	return newMemoryProvider(func() time.Time { return time.Now().UTC() })
}

func newMemoryProvider(now func() time.Time) *MemoryProvider {
	return &MemoryProvider{streams: map[memoryStreamKey]*memoryStream{}, now: now}
}

func (p *MemoryProvider) Descriptor() versioningv1.ProviderDescriptor {
	return versioningv1.ProviderDescriptor{
		ID: "memory", Protocol: versioningv1.StorageProtocolFile, Version: "1.0.0", DisplayName: "In-memory conformance provider",
		IdentityAlgorithm: versioningv1.VersionIdentityAlgorithm,
		Consistency:       versioningv1.ConsistencySingleWriter, Durability: versioningv1.DurabilityLocal,
		MaxContentBytes:     versioningv1.MaxContentBytes,
		ConfigurationSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
		Capabilities: versioningv1.ProviderCapabilities{
			DetachedVersions: true, NamedHeads: true, StableHistory: true, ImmutableTags: true, DAGParents: true,
		},
	}
}

func (p *MemoryProvider) PutVersion(ctx context.Context, scope Scope, request versioningv1.ProviderPutVersionRequest) (versioningv1.PutVersionResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.PutVersionResult{}, err
	}
	if err := validateProviderRequest(scope, &request); err != nil {
		return versioningv1.PutVersionResult{}, err
	}
	key := memoryKey(scope, request.Candidate.Stream)
	p.mu.Lock()
	defer p.mu.Unlock()
	stream := p.streams[key]
	if stream == nil {
		stream = &memoryStream{
			versions: map[string]versioningv1.VersionRecord{}, idempotency: map[string]string{},
			heads: map[string]versioningv1.Head{}, headRevisions: map[string]uint64{}, tags: map[string]versioningv1.Tag{},
		}
		p.streams[key] = stream
	}
	if versionID, exists := stream.idempotency[request.IdempotencyKey]; exists {
		record := stream.versions[versionID]
		if !sameCandidate(record, request.Candidate) {
			return versioningv1.PutVersionResult{}, providerError(versioningv1.ErrorConflict, false, errors.New("idempotencyKey 已绑定不同版本候选"))
		}
		return versioningv1.PutVersionResult{Version: cloneRecord(record), Reused: true}, nil
	}
	if stream.sequence == math.MaxUint64 {
		return versioningv1.PutVersionResult{}, providerError(versioningv1.ErrorLimitExceeded, false, errors.New("版本 sequence 已耗尽"))
	}
	next := stream.sequence + 1
	if err := requireStoredParents(stream.versions, next, request.Candidate); err != nil {
		return versioningv1.PutVersionResult{}, err
	}
	digest, err := versioningv1.ContentDigest(request.Candidate.Content)
	if err != nil {
		return versioningv1.PutVersionResult{}, providerError(versioningv1.ErrorInvalidRequest, false, err)
	}
	record := versioningv1.VersionRecord{
		Protocol: versioningv1.Protocol,
		Ref:      versioningv1.VersionRef{Stream: request.Candidate.Stream, VersionID: request.Candidate.VersionID, Sequence: next, ContentDigest: digest},
		Parents:  cloneRefs(request.Candidate.Parents), Content: append([]byte(nil), request.Candidate.Content...), Message: request.Candidate.Message,
		Labels: cloneLabels(request.Candidate.Labels), ActorID: request.Candidate.ActorID, CreatedAt: p.now(),
	}
	if err := versioningv1.ValidateVersionRecord(record); err != nil {
		return versioningv1.PutVersionResult{}, providerError(versioningv1.ErrorCorrupted, false, err)
	}
	stream.sequence = next
	stream.versions[record.Ref.VersionID] = cloneRecord(record)
	stream.idempotency[request.IdempotencyKey] = record.Ref.VersionID
	return versioningv1.PutVersionResult{Version: cloneRecord(record)}, nil
}

func (p *MemoryProvider) GetVersion(ctx context.Context, scope Scope, request versioningv1.GetVersionRequest) (versioningv1.GetVersionResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.GetVersionResult{}, err
	}
	if err := validateScopedRequest(scope, versioningv1.OperationGetVersion, request); err != nil {
		return versioningv1.GetVersionResult{}, err
	}
	p.mu.RLock()
	record, ok := p.record(scope, request.Ref)
	p.mu.RUnlock()
	if !ok {
		return versioningv1.GetVersionResult{}, providerError(versioningv1.ErrorNotFound, false, errors.New("版本不存在"))
	}
	return versioningv1.GetVersionResult{Version: record}, nil
}

func (p *MemoryProvider) ListHistory(ctx context.Context, scope Scope, request versioningv1.ListHistoryRequest) (versioningv1.ListHistoryResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.ListHistoryResult{}, err
	}
	if err := validateScopedRequest(scope, versioningv1.OperationListHistory, request); err != nil {
		return versioningv1.ListHistoryResult{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	stream := p.streams[memoryKey(scope, request.Stream)]
	if stream == nil || len(stream.versions) == 0 {
		return versioningv1.ListHistoryResult{Versions: []versioningv1.VersionRecord{}}, nil
	}
	current, ok := historyStart(stream.versions, request)
	if !ok {
		return versioningv1.ListHistoryResult{}, providerError(versioningv1.ErrorNotFound, false, errors.New("历史起点不存在"))
	}
	result := versioningv1.ListHistoryResult{Versions: make([]versioningv1.VersionRecord, 0, request.Limit)}
	for len(result.Versions) < request.Limit {
		result.Versions = append(result.Versions, cloneRecord(current))
		if len(current.Parents) == 0 {
			break
		}
		firstParent := current.Parents[0]
		next, exists := stream.versions[firstParent.VersionID]
		if !exists || next.Ref != firstParent {
			return versioningv1.ListHistoryResult{}, providerError(versioningv1.ErrorCorrupted, false, errors.New("版本父链损坏"))
		}
		if len(result.Versions) == request.Limit {
			result.NextCursor = next.Ref.VersionID
			break
		}
		current = next
	}
	return result, nil
}

func (p *MemoryProvider) GetHead(ctx context.Context, scope Scope, request versioningv1.GetHeadRequest) (versioningv1.GetHeadResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.GetHeadResult{}, err
	}
	if err := validateScopedRequest(scope, versioningv1.OperationGetHead, request); err != nil {
		return versioningv1.GetHeadResult{}, err
	}
	p.mu.RLock()
	stream := p.streams[memoryKey(scope, request.Stream)]
	head, ok := versioningv1.Head{}, false
	if stream != nil {
		head, ok = stream.heads[request.Name]
	}
	p.mu.RUnlock()
	if !ok {
		return versioningv1.GetHeadResult{}, providerError(versioningv1.ErrorNotFound, false, errors.New("Version Head 不存在"))
	}
	return versioningv1.GetHeadResult{Head: head}, nil
}

func (p *MemoryProvider) MoveHead(ctx context.Context, scope Scope, request versioningv1.MoveHeadRequest) (versioningv1.MoveHeadResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.MoveHeadResult{}, err
	}
	if err := validateScopedRequest(scope, versioningv1.OperationMoveHead, request); err != nil {
		return versioningv1.MoveHeadResult{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	stream := p.streams[memoryKey(scope, request.Stream)]
	if stream == nil {
		return versioningv1.MoveHeadResult{}, providerError(versioningv1.ErrorNotFound, false, errors.New("目标 stream 不存在"))
	}
	target, ok := stream.versions[request.Target.VersionID]
	if !ok || target.Ref != request.Target {
		return versioningv1.MoveHeadResult{}, providerError(versioningv1.ErrorNotFound, false, errors.New("Head 目标版本不存在"))
	}
	current, exists := stream.heads[request.Name]
	if (!exists && request.ExpectedRevision != 0) || (exists && current.Revision != request.ExpectedRevision) {
		return versioningv1.MoveHeadResult{}, providerError(versioningv1.ErrorConflict, false, errors.New("Version Head CAS 冲突"))
	}
	revision := stream.headRevisions[request.Name]
	if exists {
		revision = current.Revision
	}
	if revision == math.MaxUint64 {
		return versioningv1.MoveHeadResult{}, providerError(versioningv1.ErrorLimitExceeded, false, errors.New("Head revision 已耗尽"))
	}
	revision++
	head := versioningv1.Head{Protocol: versioningv1.Protocol, Stream: request.Stream, Name: request.Name, Target: request.Target, Revision: revision, UpdatedAt: p.now()}
	stream.heads[request.Name] = head
	stream.headRevisions[request.Name] = revision
	return versioningv1.MoveHeadResult{Head: head}, nil
}

func (p *MemoryProvider) record(scope Scope, ref versioningv1.VersionRef) (versioningv1.VersionRecord, bool) {
	stream := p.streams[memoryKey(scope, ref.Stream)]
	if stream == nil {
		return versioningv1.VersionRecord{}, false
	}
	record, ok := stream.versions[ref.VersionID]
	if !ok || record.Ref != ref {
		return versioningv1.VersionRecord{}, false
	}
	return cloneRecord(record), true
}

func memoryKey(scope Scope, stream versioningv1.StreamKey) memoryStreamKey {
	return memoryStreamKey{tenant: scope.TenantID, namespace: stream.Namespace, streamID: stream.StreamID}
}

func historyStart(versions map[string]versioningv1.VersionRecord, request versioningv1.ListHistoryRequest) (versioningv1.VersionRecord, bool) {
	if request.Start != nil {
		record, ok := versions[request.Start.VersionID]
		return record, ok && record.Ref == *request.Start
	}
	if request.Cursor != "" {
		record, ok := versions[request.Cursor]
		return record, ok
	}
	var latest versioningv1.VersionRecord
	for _, record := range versions {
		if record.Ref.Sequence > latest.Ref.Sequence {
			latest = record
		}
	}
	return latest, latest.Ref.Sequence != 0
}

func cloneLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return nil
	}
	cloned := make(map[string]string, len(labels))
	for key, value := range labels {
		cloned[key] = value
	}
	return cloned
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return providerError(versioningv1.ErrorInvalidRequest, false, errors.New("context 不能为空"))
	}
	if err := ctx.Err(); err != nil {
		return providerError(versioningv1.ErrorProviderUnavailable, true, err)
	}
	return nil
}
