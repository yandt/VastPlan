package versionworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"sort"
	"strings"

	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
)

const maxStandardFilesContentBytes = int64(1 << 40)

type fileAdapterKind uint8

const (
	textAdapter fileAdapterKind = iota
	blobAdapter
	filesAdapter
)

type StandardFilesAdapter struct {
	id   string
	kind fileAdapterKind
}

func NewTextAdapter() *StandardFilesAdapter {
	return &StandardFilesAdapter{id: TextAdapterID, kind: textAdapter}
}
func NewBlobAdapter() *StandardFilesAdapter {
	return &StandardFilesAdapter{id: BlobAdapterID, kind: blobAdapter}
}
func NewFilesAdapter() *StandardFilesAdapter {
	return &StandardFilesAdapter{id: FilesAdapterID, kind: filesAdapter}
}

func (a *StandardFilesAdapter) Descriptor() resourcev1.AdapterDescriptor {
	return resourcev1.AdapterDescriptor{
		Protocol: resourcev1.Protocol, ID: a.id, Version: "1.0.0", ContentKind: resourcev1.ContentFiles,
		SupportedModes: []string{resourcev1.ModeSnapshot}, DefaultMode: resourcev1.ModeSnapshot, MaxSnapshotBytes: maxStandardFilesContentBytes,
		SecretPolicy: resourcev1.SecretPolicyForbidden, Capabilities: resourcev1.AdapterCapabilities{Normalize: true, Diff: true},
		ConfigurationSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
	}
}

func (a *StandardFilesAdapter) Normalize(ctx context.Context, request resourcev1.AdapterNormalizeRequest) (resourcev1.AdapterNormalizeResult, error) {
	if err := ctx.Err(); err != nil {
		return resourcev1.AdapterNormalizeResult{}, err
	}
	if err := resourcev1.ValidateAdapterNormalizeRequest(request, a.Descriptor().MaxSnapshotBytes); err != nil {
		return resourcev1.AdapterNormalizeResult{}, err
	}
	if request.Mode != resourcev1.ModeSnapshot || request.Snapshot.Kind != resourcev1.ContentFiles {
		return resourcev1.AdapterNormalizeResult{}, errors.New("标准文件 Adapter 当前只支持 snapshot files")
	}
	if err := validateEmptyAdapterConfiguration(request.Configuration); err != nil {
		return resourcev1.AdapterNormalizeResult{}, err
	}
	if err := a.validateShape(request.Snapshot.Files); err != nil {
		return resourcev1.AdapterNormalizeResult{}, err
	}
	normalized := resourcev1.Snapshot{Kind: resourcev1.ContentFiles, MediaType: resourcev1.FilesManifestMediaType, Files: append([]resourcev1.FileEntry(nil), request.Snapshot.Files...)}
	digest, err := resourcev1.SnapshotDigest(normalized, a.Descriptor().MaxSnapshotBytes)
	if err != nil {
		return resourcev1.AdapterNormalizeResult{}, err
	}
	return resourcev1.AdapterNormalizeResult{Snapshot: normalized, Digest: digest}, nil
}

func (a *StandardFilesAdapter) validateShape(files []resourcev1.FileEntry) error {
	switch a.kind {
	case textAdapter:
		if len(files) > 1 || len(files) == 1 && !isTextMediaType(files[0].MediaType) {
			return errors.New("Text Adapter 最多允许一个文本内容文件")
		}
	case blobAdapter:
		if len(files) > 1 {
			return errors.New("Blob Adapter 最多允许一个内容文件")
		}
	case filesAdapter:
		return nil
	default:
		return errors.New("标准文件 Adapter 类型无效")
	}
	return nil
}

func isTextMediaType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	return strings.HasPrefix(mediaType, "text/") || strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml") ||
		mediaType == "application/json" || mediaType == "application/xml" || mediaType == "application/javascript"
}

func (a *StandardFilesAdapter) Diff(ctx context.Context, request resourcev1.AdapterDiffRequest) (resourcev1.AdapterDiffResult, error) {
	if err := ctx.Err(); err != nil {
		return resourcev1.AdapterDiffResult{}, err
	}
	if err := resourcev1.ValidateAdapterDiffRequest(request, a.Descriptor().MaxSnapshotBytes); err != nil {
		return resourcev1.AdapterDiffResult{}, err
	}
	if err := a.validateShape(request.Left.Files); err != nil {
		return resourcev1.AdapterDiffResult{}, err
	}
	if err := a.validateShape(request.Right.Files); err != nil {
		return resourcev1.AdapterDiffResult{}, err
	}
	left := make(map[string]resourcev1.FileEntry, len(request.Left.Files))
	right := make(map[string]resourcev1.FileEntry, len(request.Right.Files))
	paths := map[string]struct{}{}
	for _, entry := range request.Left.Files {
		left[entry.Path], paths[entry.Path] = entry, struct{}{}
	}
	for _, entry := range request.Right.Files {
		right[entry.Path], paths[entry.Path] = entry, struct{}{}
	}
	result := resourcev1.AdapterDiffResult{ChangedPaths: []string{}}
	for path := range paths {
		before, beforeExists := left[path]
		after, afterExists := right[path]
		switch {
		case !beforeExists:
			result.Summary.Added++
		case !afterExists:
			result.Summary.Removed++
		case before != after:
			result.Summary.Modified++
		default:
			continue
		}
		result.ChangedPaths = append(result.ChangedPaths, path)
	}
	sort.Strings(result.ChangedPaths)
	result.Summary.Total = len(result.ChangedPaths)
	return result, resourcev1.ValidateAdapterDiffResult(result)
}
