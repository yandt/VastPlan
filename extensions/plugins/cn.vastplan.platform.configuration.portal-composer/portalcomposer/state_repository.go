package portalcomposer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	sharedstatesdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/sharedstate"
)

const (
	composerStateNamespace  = "portal.composition.v3"
	composerStateKey        = "tenant"
	composerBlobPrefix      = "blob/"
	composerRootFormat      = "portal-composer-root.v2"
	composerChunkBytes      = 512 << 10
	maximumComposerSnapshot = 64 << 20
)

type composerStateChunk struct {
	Digest string `json:"digest"`
	Size   int    `json:"size"`
}

type composerStateRoot struct {
	Format string               `json:"format"`
	Digest string               `json:"digest"`
	Size   int                  `json:"size"`
	Chunks []composerStateChunk `json:"chunks"`
}

type composerStateSession struct {
	ctx        context.Context
	call       *contractv1.CallContext
	repository *composerStateRepository
	tenant     string
	revision   uint64
}

type composerStateRepository struct{ client *sharedstatesdk.Client }

func newComposerStateRepository(host sdk.Host) (*composerStateRepository, error) {
	client, err := sharedstatesdk.New(host, "tenant", composerStateNamespace)
	if err != nil {
		return nil, err
	}
	return &composerStateRepository{client: client}, nil
}

func (r *composerStateRepository) load(ctx context.Context, call *contractv1.CallContext) (state, uint64, error) {
	entry, err := r.client.Get(ctx, call, composerStateKey)
	if sharedstatesdk.IsNotFound(err) {
		return emptyState(), 0, nil
	}
	if err != nil {
		return state{}, 0, fmt.Errorf("读取 Portal Composer Shared State: %w", err)
	}
	isRoot, err := composerDocumentIsRoot(entry.Value)
	if err != nil {
		return state{}, 0, err
	}
	if !isRoot {
		value, err := decodeComposerState(entry.Value)
		return value, entry.Revision, err
	}
	root, err := decodeComposerRoot(entry.Value)
	if err != nil {
		return state{}, 0, err
	}
	raw := make([]byte, 0, root.Size)
	for _, chunk := range root.Chunks {
		blob, err := r.client.Get(ctx, call, composerBlobPrefix+chunk.Digest)
		if err != nil {
			return state{}, 0, fmt.Errorf("读取 Portal Composer Shared State chunk: %w", err)
		}
		if len(blob.Value) != chunk.Size || composerDigest(blob.Value) != chunk.Digest {
			return state{}, 0, errors.New("Portal Composer Shared State chunk 摘要或大小不一致")
		}
		raw = append(raw, blob.Value...)
	}
	if len(raw) != root.Size || composerDigest(raw) != root.Digest {
		return state{}, 0, errors.New("Portal Composer Shared State 快照摘要或大小不一致")
	}
	value, err := decodeComposerState(raw)
	return value, entry.Revision, err
}

func (r *composerStateRepository) save(ctx context.Context, call *contractv1.CallContext, value state, expected uint64) (uint64, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	if len(raw) == 0 || len(raw) > maximumComposerSnapshot {
		return 0, fmt.Errorf("Portal Composer 租户快照必须为 1-%d 字节", maximumComposerSnapshot)
	}
	root := composerStateRoot{Format: composerRootFormat, Digest: composerDigest(raw), Size: len(raw), Chunks: []composerStateChunk{}}
	for offset := 0; offset < len(raw); offset += composerChunkBytes {
		end := offset + composerChunkBytes
		if end > len(raw) {
			end = len(raw)
		}
		chunk := raw[offset:end]
		digest := composerDigest(chunk)
		key := composerBlobPrefix + digest
		if _, err := r.client.Create(ctx, call, key, chunk); err != nil {
			if !sharedstatesdk.IsConflict(err) {
				return 0, fmt.Errorf("写入 Portal Composer Shared State chunk: %w", err)
			}
			existing, getErr := r.client.Get(ctx, call, key)
			if getErr != nil || !bytes.Equal(existing.Value, chunk) {
				return 0, errors.New("Portal Composer 内容寻址 chunk 冲突")
			}
		}
		root.Chunks = append(root.Chunks, composerStateChunk{Digest: digest, Size: len(chunk)})
	}
	rootRaw, err := json.Marshal(root)
	if err != nil {
		return 0, err
	}
	var entry sharedstatesdk.Entry
	if expected == 0 {
		entry, err = r.client.Create(ctx, call, composerStateKey, rootRaw)
	} else {
		entry, err = r.client.Update(ctx, call, composerStateKey, rootRaw, expected)
	}
	if sharedstatesdk.IsConflict(err) {
		return 0, ErrStateConflict
	}
	if err != nil {
		return 0, fmt.Errorf("保存 Portal Composer Shared State: %w", err)
	}
	return entry.Revision, nil
}

func decodeComposerState(raw []byte) (state, error) {
	value := emptyState()
	if err := decodeComposerJSON(raw, &value); err != nil {
		return state{}, fmt.Errorf("解析 Portal Composer Shared State: %w", err)
	}
	return value, nil
}

func decodeComposerRoot(raw []byte) (composerStateRoot, error) {
	var root composerStateRoot
	if err := decodeComposerJSON(raw, &root); err != nil {
		return composerStateRoot{}, fmt.Errorf("解析 Portal Composer Shared State Root: %w", err)
	}
	if root.Format != composerRootFormat || root.Size < 1 || root.Size > maximumComposerSnapshot || root.Digest == "" || len(root.Chunks) == 0 {
		return composerStateRoot{}, errors.New("Portal Composer Shared State Root 无效")
	}
	if len(root.Digest) != 64 {
		return composerStateRoot{}, errors.New("Portal Composer Shared State Root 摘要无效")
	}
	if _, err := hex.DecodeString(root.Digest); err != nil {
		return composerStateRoot{}, errors.New("Portal Composer Shared State Root 摘要无效")
	}
	total := 0
	for _, chunk := range root.Chunks {
		if len(chunk.Digest) != 64 || chunk.Size < 1 || chunk.Size > composerChunkBytes {
			return composerStateRoot{}, errors.New("Portal Composer Shared State Root chunk 无效")
		}
		if _, err := hex.DecodeString(chunk.Digest); err != nil {
			return composerStateRoot{}, errors.New("Portal Composer Shared State Root chunk 摘要无效")
		}
		total += chunk.Size
	}
	if total != root.Size {
		return composerStateRoot{}, errors.New("Portal Composer Shared State Root 大小不一致")
	}
	return root, nil
}

func composerDocumentIsRoot(raw []byte) (bool, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return false, fmt.Errorf("解析 Portal Composer Shared State 文档: %w", err)
	}
	_, ok := object["format"]
	return ok, nil
}

func decodeComposerJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("包含尾随数据")
	}
	return nil
}

func composerDigest(raw []byte) string { return fmt.Sprintf("%x", sha256.Sum256(raw)) }

func emptyState() state {
	return state{TestBindings: map[string]portalapi.TestTargetBinding{}, TestVersionOwners: map[uint64]uint64{}}
}

func validateComposerTenantState(value state, tenant string) error {
	if tenant == "" || value.TestBindings == nil || value.TestVersionOwners == nil {
		return errors.New("Portal Composer tenant 状态无效")
	}
	openPublications := map[string]int{}
	for _, revision := range value.Revisions {
		if revision.TenantID != tenant {
			return errors.New("Portal Composer Application 跨 tenant")
		}
		if _, testRevision := value.TestVersionOwners[revision.ID]; testRevision {
			continue
		}
		if revision.WorkingRevision == 0 {
			return errors.New("Portal WorkingCopy revision 无效")
		}
		if revision.Status == portalapi.StatusDraft {
			if revision.ConfigurationDigest != "" || revision.SubmittedBy != "" || revision.SubmittedAt != "" {
				return errors.New("Portal WorkingCopy 不得携带 Publication 冻结状态")
			}
			openPublications[revision.PortalID]++
			continue
		}
		if len(revision.ConfigurationDigest) != 64 || revision.SubmittedBy == "" || revision.SubmittedAt == "" {
			return errors.New("Portal Publication 冻结身份无效")
		}
		if _, err := hex.DecodeString(revision.ConfigurationDigest); err != nil {
			return errors.New("Portal Publication 摘要无效")
		}
		if revision.Status == portalapi.StatusPendingApproval || revision.Status == portalapi.StatusApproved {
			openPublications[revision.PortalID]++
		}
	}
	for portalID, count := range openPublications {
		if portalID == "" || count > 1 {
			return errors.New("Portal 只能有一个 WorkingCopy 或待结束 Publication")
		}
	}
	for _, revision := range value.Profiles {
		if revision.TenantID != "*" && revision.TenantID != tenant {
			return errors.New("Portal Composer Profile 跨 tenant")
		}
	}
	for _, revision := range value.Bindings {
		if revision.TenantID != tenant {
			return errors.New("Portal Composer Binding 跨 tenant")
		}
	}
	for _, activation := range value.Activations {
		if activation.TenantID != tenant {
			return errors.New("Portal Composer Activation 跨 tenant")
		}
	}
	for key, binding := range value.TestBindings {
		if key == "" || binding.TenantID != tenant {
			return errors.New("Portal Composer Test Binding 跨 tenant")
		}
	}
	for _, release := range value.TestReleases {
		if release.TenantID != tenant {
			return errors.New("Portal Composer Test Release 跨 tenant")
		}
	}
	versionIDs := make(map[uint64]struct{}, len(value.Revisions))
	for _, revision := range value.Revisions {
		versionIDs[revision.ID] = struct{}{}
	}
	releaseIDs := make(map[uint64]struct{}, len(value.TestReleases))
	for _, release := range value.TestReleases {
		releaseIDs[release.ID] = struct{}{}
	}
	for versionID, releaseID := range value.TestVersionOwners {
		_, versionFound := versionIDs[versionID]
		_, releaseFound := releaseIDs[releaseID]
		if !versionFound || !releaseFound {
			return errors.New("Portal Composer Test Version 归属引用无效")
		}
	}
	for _, event := range value.Audit {
		if event.TenantID != tenant {
			return errors.New("Portal Composer Audit 跨 tenant")
		}
	}
	return nil
}
