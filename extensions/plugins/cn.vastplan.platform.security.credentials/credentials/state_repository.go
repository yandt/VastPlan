package credentials

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.credentials/credentialsstate"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	sharedstatesdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/sharedstate"
)

const (
	credentialsStateNamespace = credentialsstate.Namespace
	credentialsRootKey        = credentialsstate.RootKey
	credentialsBlobPrefix     = credentialsstate.BlobPrefix
	credentialsChunkBytes     = credentialsstate.ChunkBytes
	credentialsMaxSnapshot    = credentialsstate.MaxSnapshotSize
)

var errStateConflict = errors.New("Credentials Shared State 并发冲突")

type credentialStateSession struct {
	ctx        context.Context
	call       *contractv1.CallContext
	repository *credentialStateRepository
	tenant     string
	revision   uint64
}

type credentialStateRepository struct {
	client *sharedstatesdk.Client
	writer *sharedstatesdk.Client
}

type credentialSnapshot struct {
	Records     map[string]Record        `json:"records"`
	Managed     map[string]ManagedRecord `json:"managed"`
	Audit       managedAuditState        `json:"audit"`
	Maintenance ManagedMaintenanceStatus `json:"maintenance"`
}

func newCredentialStateRepository(host sdk.Host) (*credentialStateRepository, error) {
	client, err := sharedstatesdk.New(host, "tenant", credentialsStateNamespace)
	if err != nil {
		return nil, err
	}
	writer, err := sharedstatesdk.NewFenced(host, "tenant", credentialsStateNamespace)
	if err != nil {
		return nil, err
	}
	return &credentialStateRepository{client: client, writer: writer}, nil
}

func emptyCredentialSnapshot() credentialSnapshot {
	return credentialSnapshot{Records: map[string]Record{}, Managed: map[string]ManagedRecord{}, Audit: managedAuditState{Events: []ManagedAuditEvent{}}, Maintenance: ManagedMaintenanceStatus{Counts: map[string]int{}}}
}

func (r *credentialStateRepository) load(ctx context.Context, call *contractv1.CallContext) (credentialSnapshot, uint64, error) {
	entry, err := r.client.Get(ctx, call, credentialsRootKey)
	if sharedstatesdk.IsNotFound(err) {
		return emptyCredentialSnapshot(), 0, nil
	}
	if err != nil {
		return credentialSnapshot{}, 0, fmt.Errorf("读取 Credentials Shared State Root: %w", err)
	}
	root, err := credentialsstate.ParseRoot(entry.Value)
	if err != nil {
		return credentialSnapshot{}, 0, err
	}
	raw := make([]byte, 0, root.Size)
	for _, chunk := range root.Chunks {
		blob, err := r.client.Get(ctx, call, credentialsBlobPrefix+chunk.Digest)
		if err != nil {
			return credentialSnapshot{}, 0, fmt.Errorf("读取 Credentials Shared State chunk: %w", err)
		}
		if len(blob.Value) != chunk.Size || credentialsstate.DigestHex(blob.Value) != chunk.Digest {
			return credentialSnapshot{}, 0, errors.New("Credentials Shared State chunk 摘要或大小不一致")
		}
		raw = append(raw, blob.Value...)
	}
	if len(raw) != root.Size || credentialsstate.DigestHex(raw) != root.Digest {
		return credentialSnapshot{}, 0, errors.New("Credentials Shared State 快照摘要或大小不一致")
	}
	value := emptyCredentialSnapshot()
	if err := credentialsstate.DecodeStrictJSON(raw, &value); err != nil {
		return credentialSnapshot{}, 0, fmt.Errorf("解析 Credentials Shared State 快照: %w", err)
	}
	if err := validateCredentialSnapshot(value); err != nil {
		return credentialSnapshot{}, 0, err
	}
	return value, entry.Revision, nil
}

func (r *credentialStateRepository) save(ctx context.Context, call *contractv1.CallContext, value credentialSnapshot, expected uint64) (uint64, error) {
	if err := validateCredentialSnapshot(value); err != nil {
		return 0, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	if len(raw) == 0 || len(raw) > credentialsMaxSnapshot {
		return 0, fmt.Errorf("Credentials tenant 快照必须为 1-%d 字节", credentialsMaxSnapshot)
	}
	root, err := credentialsstate.NewRoot(raw)
	if err != nil {
		return 0, err
	}
	for offset := 0; offset < len(raw); offset += credentialsChunkBytes {
		end := offset + credentialsChunkBytes
		if end > len(raw) {
			end = len(raw)
		}
		chunk := raw[offset:end]
		digest := credentialsstate.DigestHex(chunk)
		key := credentialsBlobPrefix + digest
		if _, err := r.writer.Create(ctx, call, key, chunk); err != nil {
			if !sharedstatesdk.IsConflict(err) {
				return 0, fmt.Errorf("写入 Credentials Shared State chunk: %w", err)
			}
			existing, getErr := r.client.Get(ctx, call, key)
			if getErr != nil || !bytes.Equal(existing.Value, chunk) {
				return 0, errors.New("Credentials 内容寻址 chunk 冲突")
			}
		}
		root.Chunks = append(root.Chunks, credentialsstate.Chunk{Digest: digest, Size: len(chunk)})
	}
	rootRaw, err := json.Marshal(root)
	if err != nil {
		return 0, err
	}
	var entry sharedstatesdk.Entry
	if expected == 0 {
		entry, err = r.writer.Create(ctx, call, credentialsRootKey, rootRaw)
	} else {
		entry, err = r.writer.Update(ctx, call, credentialsRootKey, rootRaw, expected)
	}
	if sharedstatesdk.IsConflict(err) {
		return 0, errStateConflict
	}
	if err != nil {
		return 0, fmt.Errorf("提交 Credentials Shared State Root: %w", err)
	}
	return entry.Revision, nil
}

func validateCredentialSnapshot(value credentialSnapshot) error {
	if value.Records == nil || value.Managed == nil || value.Audit.Events == nil || value.Maintenance.Counts == nil {
		return errors.New("Credentials tenant 快照缺少必填集合")
	}
	for name, record := range value.Records {
		if validName(name) != nil || record.Name != name || record.Version < 1 || record.Ciphertext == "" || record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() || record.UpdatedAt.Before(record.CreatedAt) {
			return fmt.Errorf("Credentials 命名凭证 %q 无效", name)
		}
	}
	if err := validateManagedState(map[string]map[string]ManagedRecord{"tenant": value.Managed}, map[string]managedAuditState{"tenant": value.Audit}, map[string]ManagedMaintenanceStatus{"tenant": value.Maintenance}); err != nil {
		return err
	}
	return nil
}
