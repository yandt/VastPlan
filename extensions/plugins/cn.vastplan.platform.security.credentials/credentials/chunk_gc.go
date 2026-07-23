package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.credentials/credentialsstate"
	sharedstatesdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/sharedstate"
)

// collectOrphanChunks advances at most one bounded mark or sweep page. All
// mutations use the fenced writer, while reachability is derived exclusively
// from the current tenant Root owned by the Credentials schema.
func (r *credentialStateRepository) collectOrphanChunks(ctx context.Context, call *contractv1.CallContext, now time.Time, policy MaintenancePolicy) error {
	state, revision, err := r.loadChunkGCState(ctx, call, now)
	if err != nil {
		return err
	}
	if state.Phase == credentialsstate.GCPhaseIdle {
		if state.LastCompletedAt != nil && state.LastCompletedAt.Add(policy.Interval).After(now) {
			return nil
		}
		started := now.UTC()
		state.Phase, state.Cursor, state.CycleStartedAt = credentialsstate.GCPhaseMark, "", &started
	}
	switch state.Phase {
	case credentialsstate.GCPhaseMark:
		err = r.markOrphanChunkPage(ctx, call, now, policy.ChunkGCBatchSize, &state)
	case credentialsstate.GCPhaseSweep:
		err = r.sweepOrphanChunkPage(ctx, call, now, policy, &state)
	default:
		err = errors.New("Credentials chunk GC phase 无效")
	}
	if err != nil {
		return err
	}
	return r.saveChunkGCState(ctx, call, state, revision)
}

func (r *credentialStateRepository) markOrphanChunkPage(ctx context.Context, call *contractv1.CallContext, now time.Time, limit int, state *credentialsstate.ChunkGCState) error {
	referenced, err := r.currentChunkDigests(ctx, call)
	if err != nil {
		return err
	}
	page, err := r.client.List(ctx, call, credentialsBlobPrefix, limit, state.Cursor)
	if err != nil {
		return fmt.Errorf("列出 Credentials chunk: %w", err)
	}
	for _, blob := range page.Items {
		digest := strings.TrimPrefix(blob.Key, credentialsBlobPrefix)
		if digest == blob.Key || credentialsstate.DigestHex(blob.Value) != digest {
			return errors.New("Credentials chunk key 或摘要无效")
		}
		if _, live := referenced[digest]; live {
			continue
		}
		markerKey := credentialsstate.GCMarkerKey(digest)
		existing, getErr := r.client.Get(ctx, call, markerKey)
		if getErr == nil {
			marker, parseErr := credentialsstate.ParseChunkGCMarker(existing.Value)
			if parseErr != nil || marker.Digest != digest {
				return errors.New("Credentials chunk GC marker 损坏")
			}
			if marker.BlobRevision == blob.Revision {
				continue
			}
			marker, markerErr := credentialsstate.NewChunkGCMarker(digest, blob.Revision, now)
			if markerErr != nil {
				return markerErr
			}
			raw, marshalErr := json.Marshal(marker)
			if marshalErr != nil {
				return marshalErr
			}
			if _, err := r.writer.Update(ctx, call, markerKey, raw, existing.Revision); err != nil {
				return fmt.Errorf("重置 Credentials chunk GC marker: %w", err)
			}
			continue
		}
		if !sharedstatesdk.IsNotFound(getErr) {
			return fmt.Errorf("读取 Credentials chunk GC marker: %w", getErr)
		}
		marker, markerErr := credentialsstate.NewChunkGCMarker(digest, blob.Revision, now)
		if markerErr != nil {
			return markerErr
		}
		raw, marshalErr := json.Marshal(marker)
		if marshalErr != nil {
			return marshalErr
		}
		if _, err := r.writer.Create(ctx, call, markerKey, raw); err != nil && !sharedstatesdk.IsConflict(err) {
			return fmt.Errorf("创建 Credentials chunk GC marker: %w", err)
		}
		state.Marked++
	}
	state.Cursor = page.NextCursor
	if page.NextCursor == "" {
		state.Phase, state.Cursor = credentialsstate.GCPhaseSweep, ""
	}
	return nil
}

func (r *credentialStateRepository) sweepOrphanChunkPage(ctx context.Context, call *contractv1.CallContext, now time.Time, policy MaintenancePolicy, state *credentialsstate.ChunkGCState) error {
	page, err := r.client.List(ctx, call, credentialsstate.GCMarkerPrefix, policy.ChunkGCBatchSize, state.Cursor)
	if err != nil {
		return fmt.Errorf("列出 Credentials chunk GC marker: %w", err)
	}
	referenced, err := r.currentChunkDigests(ctx, call)
	if err != nil {
		return err
	}
	for _, entry := range page.Items {
		marker, err := credentialsstate.ParseChunkGCMarker(entry.Value)
		if err != nil || entry.Key != credentialsstate.GCMarkerKey(marker.Digest) {
			return errors.New("Credentials chunk GC marker key 或内容无效")
		}
		if _, live := referenced[marker.Digest]; live {
			if err := r.writer.Delete(ctx, call, entry.Key, entry.Revision); err != nil && !sharedstatesdk.IsNotFound(err) {
				return fmt.Errorf("删除已恢复可达的 Credentials chunk GC marker: %w", err)
			}
			continue
		}
		if marker.FirstObservedAt.Add(policy.OrphanChunkGrace).After(now) {
			continue
		}
		blobKey := credentialsBlobPrefix + marker.Digest
		blob, err := r.client.Get(ctx, call, blobKey)
		if sharedstatesdk.IsNotFound(err) {
			if deleteErr := r.writer.Delete(ctx, call, entry.Key, entry.Revision); deleteErr != nil && !sharedstatesdk.IsNotFound(deleteErr) {
				return fmt.Errorf("清理无 blob 的 Credentials chunk GC marker: %w", deleteErr)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("复核 Credentials orphan chunk: %w", err)
		}
		if blob.Revision != marker.BlobRevision || credentialsstate.DigestHex(blob.Value) != marker.Digest {
			fresh, markerErr := credentialsstate.NewChunkGCMarker(marker.Digest, blob.Revision, now)
			if markerErr != nil {
				return markerErr
			}
			raw, marshalErr := json.Marshal(fresh)
			if marshalErr != nil {
				return marshalErr
			}
			if _, err := r.writer.Update(ctx, call, entry.Key, raw, entry.Revision); err != nil {
				return fmt.Errorf("刷新 Credentials chunk GC marker: %w", err)
			}
			continue
		}
		// Re-read the authoritative Root immediately before delete. The fenced
		// writer and workflow lock exclude a concurrent current writer; this
		// final check also makes recovery from an older marker fail closed.
		referenced, err = r.currentChunkDigests(ctx, call)
		if err != nil {
			return err
		}
		if _, live := referenced[marker.Digest]; live {
			if err := r.writer.Delete(ctx, call, entry.Key, entry.Revision); err != nil && !sharedstatesdk.IsNotFound(err) {
				return fmt.Errorf("删除最终复核已可达的 Credentials chunk GC marker: %w", err)
			}
			continue
		}
		if err := r.writer.Delete(ctx, call, blobKey, blob.Revision); err != nil && !sharedstatesdk.IsNotFound(err) {
			return fmt.Errorf("删除 Credentials orphan chunk: %w", err)
		}
		state.Deleted++
		if err := r.writer.Delete(ctx, call, entry.Key, entry.Revision); err != nil && !sharedstatesdk.IsNotFound(err) {
			return fmt.Errorf("删除 Credentials chunk GC marker: %w", err)
		}
	}
	state.Cursor = page.NextCursor
	if page.NextCursor == "" {
		completed := now.UTC()
		state.Phase, state.Cursor, state.CycleStartedAt, state.LastCompletedAt = credentialsstate.GCPhaseIdle, "", nil, &completed
	}
	return nil
}

func (r *credentialStateRepository) currentChunkDigests(ctx context.Context, call *contractv1.CallContext) (map[string]struct{}, error) {
	entry, err := r.client.Get(ctx, call, credentialsRootKey)
	if sharedstatesdk.IsNotFound(err) {
		return map[string]struct{}{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 Credentials Root 以复核 GC: %w", err)
	}
	root, err := credentialsstate.ParseRoot(entry.Value)
	if err != nil {
		return nil, err
	}
	result := make(map[string]struct{}, len(root.Chunks))
	for _, chunk := range root.Chunks {
		result[chunk.Digest] = struct{}{}
	}
	return result, nil
}

func (r *credentialStateRepository) loadChunkGCState(ctx context.Context, call *contractv1.CallContext, now time.Time) (credentialsstate.ChunkGCState, uint64, error) {
	entry, err := r.client.Get(ctx, call, credentialsstate.GCStateKey)
	if sharedstatesdk.IsNotFound(err) {
		return credentialsstate.NewChunkGCState(now), 0, nil
	}
	if err != nil {
		return credentialsstate.ChunkGCState{}, 0, fmt.Errorf("读取 Credentials chunk GC state: %w", err)
	}
	state, err := credentialsstate.ParseChunkGCState(entry.Value)
	return state, entry.Revision, err
}

func (r *credentialStateRepository) saveChunkGCState(ctx context.Context, call *contractv1.CallContext, state credentialsstate.ChunkGCState, revision uint64) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if _, err := credentialsstate.ParseChunkGCState(raw); err != nil {
		return err
	}
	if revision == 0 {
		_, err = r.writer.Create(ctx, call, credentialsstate.GCStateKey, raw)
	} else {
		_, err = r.writer.Update(ctx, call, credentialsstate.GCStateKey, raw, revision)
	}
	if err != nil {
		return fmt.Errorf("提交 Credentials chunk GC state: %w", err)
	}
	return nil
}
