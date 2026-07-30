package versionledger

import (
	"context"
	"errors"
	"math"
	"sort"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

func (p *MemoryProvider) ListHeads(ctx context.Context, scope Scope, request versioningv1.ListHeadsRequest) (versioningv1.ListHeadsResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.ListHeadsResult{}, err
	}
	if err := validateScopedRequest(scope, versioningv1.OperationListHeads, request); err != nil {
		return versioningv1.ListHeadsResult{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	stream := p.streams[memoryKey(scope, request.Stream)]
	if stream == nil {
		return versioningv1.ListHeadsResult{Heads: []versioningv1.Head{}}, nil
	}
	names := sortedNames(stream.heads, request.Cursor)
	result := versioningv1.ListHeadsResult{Heads: []versioningv1.Head{}}
	for _, name := range names {
		if len(result.Heads) == request.Limit {
			result.NextCursor = result.Heads[len(result.Heads)-1].Name
			break
		}
		result.Heads = append(result.Heads, stream.heads[name])
	}
	return result, nil
}

func (p *MemoryProvider) CreateHead(ctx context.Context, scope Scope, request versioningv1.CreateHeadRequest) (versioningv1.CreateHeadResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.CreateHeadResult{}, err
	}
	if err := validateScopedRequest(scope, versioningv1.OperationCreateHead, request); err != nil {
		return versioningv1.CreateHeadResult{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	stream := p.streams[memoryKey(scope, request.Stream)]
	if stream == nil || !exactVersionExists(stream.versions, request.Target) {
		return versioningv1.CreateHeadResult{}, providerError(versioningv1.ErrorNotFound, false, errors.New("Head 目标版本不存在"))
	}
	if current, exists := stream.heads[request.Name]; exists {
		if current.Target != request.Target {
			return versioningv1.CreateHeadResult{}, providerError(versioningv1.ErrorConflict, false, errors.New("Version Head 已存在"))
		}
		return versioningv1.CreateHeadResult{Head: current, Reused: true}, nil
	}
	lastRevision := stream.headRevisions[request.Name]
	if lastRevision == math.MaxUint64 {
		return versioningv1.CreateHeadResult{}, providerError(versioningv1.ErrorLimitExceeded, false, errors.New("Head revision 已耗尽"))
	}
	head := versioningv1.Head{Protocol: versioningv1.Protocol, Stream: request.Stream, Name: request.Name, Target: request.Target, Revision: lastRevision + 1, UpdatedAt: p.now()}
	stream.heads[request.Name] = head
	stream.headRevisions[request.Name] = head.Revision
	return versioningv1.CreateHeadResult{Head: head}, nil
}

func (p *MemoryProvider) DeleteHead(ctx context.Context, scope Scope, request versioningv1.DeleteHeadRequest) (versioningv1.DeleteHeadResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.DeleteHeadResult{}, err
	}
	if err := validateScopedRequest(scope, versioningv1.OperationDeleteHead, request); err != nil {
		return versioningv1.DeleteHeadResult{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	stream := p.streams[memoryKey(scope, request.Stream)]
	if stream == nil {
		return versioningv1.DeleteHeadResult{}, providerError(versioningv1.ErrorNotFound, false, errors.New("Version Head 不存在"))
	}
	current, exists := stream.heads[request.Name]
	if !exists {
		return versioningv1.DeleteHeadResult{}, providerError(versioningv1.ErrorNotFound, false, errors.New("Version Head 不存在"))
	}
	if current.Revision != request.ExpectedRevision {
		return versioningv1.DeleteHeadResult{}, providerError(versioningv1.ErrorConflict, false, errors.New("Version Head CAS 冲突"))
	}
	delete(stream.heads, request.Name)
	stream.headRevisions[request.Name] = current.Revision
	return versioningv1.DeleteHeadResult{Previous: current}, nil
}

func (p *MemoryProvider) CreateTag(ctx context.Context, scope Scope, request versioningv1.ProviderCreateTagRequest) (versioningv1.CreateTagResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.CreateTagResult{}, err
	}
	if err := validateProviderTagRequest(scope, request); err != nil {
		return versioningv1.CreateTagResult{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	stream := p.streams[memoryKey(scope, request.Stream)]
	if stream == nil || !exactVersionExists(stream.versions, request.Target) {
		return versioningv1.CreateTagResult{}, providerError(versioningv1.ErrorNotFound, false, errors.New("Tag 目标版本不存在"))
	}
	if current, exists := stream.tags[request.Name]; exists {
		if current.Target != request.Target || current.ActorID != request.ActorID {
			return versioningv1.CreateTagResult{}, providerError(versioningv1.ErrorConflict, false, errors.New("Version Tag 不可修改"))
		}
		return versioningv1.CreateTagResult{Tag: current, Reused: true}, nil
	}
	tag := versioningv1.Tag{Protocol: versioningv1.Protocol, Stream: request.Stream, Name: request.Name, Target: request.Target, ActorID: request.ActorID, CreatedAt: p.now()}
	stream.tags[request.Name] = tag
	return versioningv1.CreateTagResult{Tag: tag}, nil
}

func (p *MemoryProvider) GetTag(ctx context.Context, scope Scope, request versioningv1.GetTagRequest) (versioningv1.GetTagResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.GetTagResult{}, err
	}
	if err := validateScopedRequest(scope, versioningv1.OperationGetTag, request); err != nil {
		return versioningv1.GetTagResult{}, err
	}
	p.mu.RLock()
	stream := p.streams[memoryKey(scope, request.Stream)]
	tag, exists := versioningv1.Tag{}, false
	if stream != nil {
		tag, exists = stream.tags[request.Name]
	}
	p.mu.RUnlock()
	if !exists {
		return versioningv1.GetTagResult{}, providerError(versioningv1.ErrorNotFound, false, errors.New("Version Tag 不存在"))
	}
	return versioningv1.GetTagResult{Tag: tag}, nil
}

func (p *MemoryProvider) ListTags(ctx context.Context, scope Scope, request versioningv1.ListTagsRequest) (versioningv1.ListTagsResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.ListTagsResult{}, err
	}
	if err := validateScopedRequest(scope, versioningv1.OperationListTags, request); err != nil {
		return versioningv1.ListTagsResult{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	stream := p.streams[memoryKey(scope, request.Stream)]
	if stream == nil {
		return versioningv1.ListTagsResult{Tags: []versioningv1.Tag{}}, nil
	}
	names := sortedNames(stream.tags, request.Cursor)
	result := versioningv1.ListTagsResult{Tags: []versioningv1.Tag{}}
	for _, name := range names {
		if len(result.Tags) == request.Limit {
			result.NextCursor = result.Tags[len(result.Tags)-1].Name
			break
		}
		result.Tags = append(result.Tags, stream.tags[name])
	}
	return result, nil
}

func sortedNames[T any](values map[string]T, cursor string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		if name > cursor {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func exactVersionExists(versions map[string]versioningv1.VersionRecord, ref versioningv1.VersionRef) bool {
	record, ok := versions[ref.VersionID]
	return ok && record.Ref == ref
}
