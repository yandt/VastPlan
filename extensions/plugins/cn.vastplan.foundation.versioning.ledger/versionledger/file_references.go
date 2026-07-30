package versionledger

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

func (p *FileProvider) ListHeads(ctx context.Context, scope Scope, request versioningv1.ListHeadsRequest) (versioningv1.ListHeadsResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.ListHeadsResult{}, err
	}
	if err := validateScopedRequest(scope, versioningv1.OperationListHeads, request); err != nil {
		return versioningv1.ListHeadsResult{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	streamDir, loaded, err := p.referenceStream(scope, request.Stream)
	if err != nil {
		if providerCode(err) == versioningv1.ErrorNotFound {
			return versioningv1.ListHeadsResult{Heads: []versioningv1.Head{}}, nil
		}
		return versioningv1.ListHeadsResult{}, err
	}
	heads, err := readAllFileHeads(streamDir, request.Stream, loaded.versions)
	if err != nil {
		return versioningv1.ListHeadsResult{}, err
	}
	names := sortedNames(heads, request.Cursor)
	result := versioningv1.ListHeadsResult{Heads: []versioningv1.Head{}}
	for _, name := range names {
		if len(result.Heads) == request.Limit {
			result.NextCursor = result.Heads[len(result.Heads)-1].Name
			break
		}
		result.Heads = append(result.Heads, heads[name])
	}
	return result, nil
}

func (p *FileProvider) CreateHead(ctx context.Context, scope Scope, request versioningv1.CreateHeadRequest) (versioningv1.CreateHeadResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.CreateHeadResult{}, err
	}
	if err := validateScopedRequest(scope, versioningv1.OperationCreateHead, request); err != nil {
		return versioningv1.CreateHeadResult{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	streamDir, loaded, err := p.referenceStream(scope, request.Stream)
	if err != nil {
		return versioningv1.CreateHeadResult{}, err
	}
	if !exactVersionExists(loaded.versions, request.Target) {
		return versioningv1.CreateHeadResult{}, providerError(versioningv1.ErrorNotFound, false, errors.New("Head 目标版本不存在"))
	}
	current, exists, persisted, err := readFileHeadState(streamDir, request.Stream, request.Name)
	if err != nil {
		return versioningv1.CreateHeadResult{}, err
	}
	if exists {
		if current.Target != request.Target {
			return versioningv1.CreateHeadResult{}, providerError(versioningv1.ErrorConflict, false, errors.New("Version Head 已存在"))
		}
		return versioningv1.CreateHeadResult{Head: current, Reused: true}, nil
	}
	lastRevision := uint64(0)
	if persisted {
		lastRevision = current.Revision
	}
	if lastRevision == math.MaxUint64 {
		return versioningv1.CreateHeadResult{}, providerError(versioningv1.ErrorLimitExceeded, false, errors.New("Head revision 已耗尽"))
	}
	head := versioningv1.Head{Protocol: versioningv1.Protocol, Stream: request.Stream, Name: request.Name, Target: request.Target, Revision: lastRevision + 1, UpdatedAt: p.now()}
	headsDir, err := secureChildDirectory(streamDir, "heads")
	if err != nil {
		return versioningv1.CreateHeadResult{}, fileProviderError(err)
	}
	if err := writeAtomicJSON(headsDir, ".head-*", request.Name+".json", fileHeadEnvelope{FormatVersion: fileProviderFormatVersion, Head: head}); err != nil {
		return versioningv1.CreateHeadResult{}, fileProviderError(err)
	}
	return versioningv1.CreateHeadResult{Head: head}, nil
}

func (p *FileProvider) DeleteHead(ctx context.Context, scope Scope, request versioningv1.DeleteHeadRequest) (versioningv1.DeleteHeadResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.DeleteHeadResult{}, err
	}
	if err := validateScopedRequest(scope, versioningv1.OperationDeleteHead, request); err != nil {
		return versioningv1.DeleteHeadResult{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	streamDir, err := p.streamDirectory(scope, request.Stream, false)
	if err != nil {
		return versioningv1.DeleteHeadResult{}, notFoundOrFileError(err, "Version Head 不存在")
	}
	current, exists, err := readOptionalFileHead(streamDir, request.Stream, request.Name)
	if err != nil {
		return versioningv1.DeleteHeadResult{}, err
	}
	if !exists {
		return versioningv1.DeleteHeadResult{}, providerError(versioningv1.ErrorNotFound, false, errors.New("Version Head 不存在"))
	}
	if current.Revision != request.ExpectedRevision {
		return versioningv1.DeleteHeadResult{}, providerError(versioningv1.ErrorConflict, false, errors.New("Version Head CAS 冲突"))
	}
	headsDir, err := secureChildDirectory(streamDir, "heads")
	if err != nil {
		return versioningv1.DeleteHeadResult{}, fileProviderError(err)
	}
	if err := writeAtomicJSON(headsDir, ".head-*", request.Name+".json", fileHeadEnvelope{FormatVersion: fileProviderFormatVersion, Head: current, Deleted: true}); err != nil {
		return versioningv1.DeleteHeadResult{}, fileProviderError(err)
	}
	return versioningv1.DeleteHeadResult{Previous: current}, nil
}

func (p *FileProvider) CreateTag(ctx context.Context, scope Scope, request versioningv1.ProviderCreateTagRequest) (versioningv1.CreateTagResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.CreateTagResult{}, err
	}
	if err := validateProviderTagRequest(scope, request); err != nil {
		return versioningv1.CreateTagResult{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	streamDir, loaded, err := p.referenceStream(scope, request.Stream)
	if err != nil {
		return versioningv1.CreateTagResult{}, err
	}
	if !exactVersionExists(loaded.versions, request.Target) {
		return versioningv1.CreateTagResult{}, providerError(versioningv1.ErrorNotFound, false, errors.New("Tag 目标版本不存在"))
	}
	current, exists, err := readOptionalFileTag(streamDir, request.Stream, request.Name)
	if err != nil {
		return versioningv1.CreateTagResult{}, err
	}
	if exists {
		if current.Target != request.Target || current.ActorID != request.ActorID {
			return versioningv1.CreateTagResult{}, providerError(versioningv1.ErrorConflict, false, errors.New("Version Tag 不可修改"))
		}
		return versioningv1.CreateTagResult{Tag: current, Reused: true}, nil
	}
	tag := versioningv1.Tag{Protocol: versioningv1.Protocol, Stream: request.Stream, Name: request.Name, Target: request.Target, ActorID: request.ActorID, CreatedAt: p.now()}
	tagsDir, err := secureChildDirectory(streamDir, "tags")
	if err != nil {
		return versioningv1.CreateTagResult{}, fileProviderError(err)
	}
	if err := writeCreateOnlyJSON(tagsDir, ".tag-*", request.Name+".json", fileTagEnvelope{FormatVersion: fileProviderFormatVersion, Tag: tag}); err != nil {
		return versioningv1.CreateTagResult{}, fileProviderError(err)
	}
	return versioningv1.CreateTagResult{Tag: tag}, nil
}

func (p *FileProvider) GetTag(ctx context.Context, scope Scope, request versioningv1.GetTagRequest) (versioningv1.GetTagResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.GetTagResult{}, err
	}
	if err := validateScopedRequest(scope, versioningv1.OperationGetTag, request); err != nil {
		return versioningv1.GetTagResult{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	streamDir, loaded, err := p.referenceStream(scope, request.Stream)
	if err != nil {
		return versioningv1.GetTagResult{}, err
	}
	tag, exists, err := readOptionalFileTag(streamDir, request.Stream, request.Name)
	if err != nil {
		return versioningv1.GetTagResult{}, err
	}
	if !exists {
		return versioningv1.GetTagResult{}, providerError(versioningv1.ErrorNotFound, false, errors.New("Version Tag 不存在"))
	}
	if !exactVersionExists(loaded.versions, tag.Target) {
		return versioningv1.GetTagResult{}, providerError(versioningv1.ErrorCorrupted, false, errors.New("Version Tag 指向不存在的版本"))
	}
	return versioningv1.GetTagResult{Tag: tag}, nil
}

func (p *FileProvider) ListTags(ctx context.Context, scope Scope, request versioningv1.ListTagsRequest) (versioningv1.ListTagsResult, error) {
	if err := contextError(ctx); err != nil {
		return versioningv1.ListTagsResult{}, err
	}
	if err := validateScopedRequest(scope, versioningv1.OperationListTags, request); err != nil {
		return versioningv1.ListTagsResult{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	streamDir, loaded, err := p.referenceStream(scope, request.Stream)
	if err != nil {
		if providerCode(err) == versioningv1.ErrorNotFound {
			return versioningv1.ListTagsResult{Tags: []versioningv1.Tag{}}, nil
		}
		return versioningv1.ListTagsResult{}, err
	}
	tags, err := readAllFileTags(streamDir, request.Stream, loaded.versions)
	if err != nil {
		return versioningv1.ListTagsResult{}, err
	}
	names := sortedNames(tags, request.Cursor)
	result := versioningv1.ListTagsResult{Tags: []versioningv1.Tag{}}
	for _, name := range names {
		if len(result.Tags) == request.Limit {
			result.NextCursor = result.Tags[len(result.Tags)-1].Name
			break
		}
		result.Tags = append(result.Tags, tags[name])
	}
	return result, nil
}

func (p *FileProvider) referenceStream(scope Scope, stream versioningv1.StreamKey) (string, loadedFileStream, error) {
	streamDir, err := p.streamDirectory(scope, stream, false)
	if err != nil {
		return "", loadedFileStream{}, notFoundOrFileError(err, "版本 stream 不存在")
	}
	loaded, err := loadFileStream(streamDir, stream)
	if err != nil {
		return "", loadedFileStream{}, providerError(versioningv1.ErrorCorrupted, false, err)
	}
	return streamDir, loaded, nil
}

func readAllFileHeads(streamDir string, stream versioningv1.StreamKey, versions map[string]versioningv1.VersionRecord) (map[string]versioningv1.Head, error) {
	heads := map[string]versioningv1.Head{}
	entries, err := referenceEntries(filepath.Join(streamDir, "heads"), ".head-")
	if err != nil {
		return nil, err
	}
	for _, name := range entries {
		head, exists, persisted, err := readFileHeadState(streamDir, stream, name)
		if err != nil || !persisted {
			return nil, providerError(versioningv1.ErrorCorrupted, false, errors.New("Version Head 列表损坏"))
		}
		if !exactVersionExists(versions, head.Target) {
			return nil, providerError(versioningv1.ErrorCorrupted, false, errors.New("Version Head 列表损坏"))
		}
		if !exists {
			continue
		}
		heads[name] = head
	}
	return heads, nil
}

func readOptionalFileTag(streamDir string, stream versioningv1.StreamKey, name string) (versioningv1.Tag, bool, error) {
	tagsDir := filepath.Join(streamDir, "tags")
	if err := validatePrivateDirectory(tagsDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return versioningv1.Tag{}, false, nil
		}
		return versioningv1.Tag{}, false, fileProviderError(err)
	}
	var envelope fileTagEnvelope
	if err := readPrivateJSON(filepath.Join(tagsDir, name+".json"), maxHeadFileBytes, &envelope); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return versioningv1.Tag{}, false, nil
		}
		return versioningv1.Tag{}, false, providerError(versioningv1.ErrorCorrupted, false, err)
	}
	if envelope.FormatVersion != fileProviderFormatVersion || envelope.Tag.Stream != stream || envelope.Tag.Name != name || versioningv1.ValidateTag(envelope.Tag) != nil {
		return versioningv1.Tag{}, false, providerError(versioningv1.ErrorCorrupted, false, errors.New("Version Tag 文件无效"))
	}
	return envelope.Tag, true, nil
}

func readAllFileTags(streamDir string, stream versioningv1.StreamKey, versions map[string]versioningv1.VersionRecord) (map[string]versioningv1.Tag, error) {
	tags := map[string]versioningv1.Tag{}
	entries, err := referenceEntries(filepath.Join(streamDir, "tags"), ".tag-")
	if err != nil {
		return nil, err
	}
	for _, name := range entries {
		tag, exists, err := readOptionalFileTag(streamDir, stream, name)
		if err != nil || !exists || !exactVersionExists(versions, tag.Target) {
			return nil, providerError(versioningv1.ErrorCorrupted, false, errors.New("Version Tag 列表损坏"))
		}
		tags[name] = tag
	}
	return tags, nil
}

func referenceEntries(directory, temporaryPrefix string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fileProviderError(err)
	}
	if err := validatePrivateDirectory(directory); err != nil {
		return nil, fileProviderError(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, temporaryPrefix) {
			continue
		}
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			return nil, providerError(versioningv1.ErrorCorrupted, false, errors.New("版本引用目录包含未知条目"))
		}
		names = append(names, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(names)
	return names, nil
}
