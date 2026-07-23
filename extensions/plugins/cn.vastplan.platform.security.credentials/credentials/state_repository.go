package credentials

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	sharedstatesdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/sharedstate"
)

const (
	credentialsStateNamespace = "credentials.ledger"
	credentialsRootKey        = "root"
	credentialsBlobPrefix     = "blob."
	credentialsSnapshotFormat = "credentials.snapshot.v1"
	credentialsChunkBytes     = 512 << 10
	credentialsMaxSnapshot    = 64 << 20
)

var errStateConflict = errors.New("Credentials Shared State 并发冲突")

type credentialStateSession struct {
	ctx        context.Context
	call       *contractv1.CallContext
	repository *credentialStateRepository
	tenant     string
	revision   uint64
}

type credentialStateRepository struct{ client *sharedstatesdk.Client }

type credentialSnapshot struct {
	Records     map[string]Record        `json:"records"`
	Managed     map[string]ManagedRecord `json:"managed"`
	Audit       managedAuditState        `json:"audit"`
	Maintenance ManagedMaintenanceStatus `json:"maintenance"`
}

type credentialSnapshotChunk struct {
	Digest string `json:"digest"`
	Size   int    `json:"size"`
}

type credentialSnapshotRoot struct {
	Format string                    `json:"format"`
	Digest string                    `json:"digest"`
	Size   int                       `json:"size"`
	Chunks []credentialSnapshotChunk `json:"chunks"`
}

func newCredentialStateRepository(host sdk.Host) (*credentialStateRepository, error) {
	client, err := sharedstatesdk.New(host, "tenant", credentialsStateNamespace)
	if err != nil {
		return nil, err
	}
	return &credentialStateRepository{client: client}, nil
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
	root, err := parseCredentialSnapshotRoot(entry.Value)
	if err != nil {
		return credentialSnapshot{}, 0, err
	}
	raw := make([]byte, 0, root.Size)
	for _, chunk := range root.Chunks {
		blob, err := r.client.Get(ctx, call, credentialsBlobPrefix+chunk.Digest)
		if err != nil {
			return credentialSnapshot{}, 0, fmt.Errorf("读取 Credentials Shared State chunk: %w", err)
		}
		if len(blob.Value) != chunk.Size || digestHex(blob.Value) != chunk.Digest {
			return credentialSnapshot{}, 0, errors.New("Credentials Shared State chunk 摘要或大小不一致")
		}
		raw = append(raw, blob.Value...)
	}
	if len(raw) != root.Size || digestHex(raw) != root.Digest {
		return credentialSnapshot{}, 0, errors.New("Credentials Shared State 快照摘要或大小不一致")
	}
	value := emptyCredentialSnapshot()
	if err := decodeStrictJSON(raw, &value); err != nil {
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
	root := credentialSnapshotRoot{Format: credentialsSnapshotFormat, Digest: digestHex(raw), Size: len(raw)}
	for offset := 0; offset < len(raw); offset += credentialsChunkBytes {
		end := offset + credentialsChunkBytes
		if end > len(raw) {
			end = len(raw)
		}
		chunk := raw[offset:end]
		digest := digestHex(chunk)
		key := credentialsBlobPrefix + digest
		if _, err := r.client.Create(ctx, call, key, chunk); err != nil {
			if !sharedstatesdk.IsConflict(err) {
				return 0, fmt.Errorf("写入 Credentials Shared State chunk: %w", err)
			}
			existing, getErr := r.client.Get(ctx, call, key)
			if getErr != nil || !bytes.Equal(existing.Value, chunk) {
				return 0, errors.New("Credentials 内容寻址 chunk 冲突")
			}
		}
		root.Chunks = append(root.Chunks, credentialSnapshotChunk{Digest: digest, Size: len(chunk)})
	}
	rootRaw, err := json.Marshal(root)
	if err != nil {
		return 0, err
	}
	var entry sharedstatesdk.Entry
	if expected == 0 {
		entry, err = r.client.Create(ctx, call, credentialsRootKey, rootRaw)
	} else {
		entry, err = r.client.Update(ctx, call, credentialsRootKey, rootRaw, expected)
	}
	if sharedstatesdk.IsConflict(err) {
		return 0, errStateConflict
	}
	if err != nil {
		return 0, fmt.Errorf("提交 Credentials Shared State Root: %w", err)
	}
	return entry.Revision, nil
}

func parseCredentialSnapshotRoot(raw []byte) (credentialSnapshotRoot, error) {
	var root credentialSnapshotRoot
	if err := decodeStrictJSON(raw, &root); err != nil {
		return root, fmt.Errorf("解析 Credentials Shared State Root: %w", err)
	}
	if root.Format != credentialsSnapshotFormat || len(root.Digest) != sha256.Size*2 || root.Size < 1 || root.Size > credentialsMaxSnapshot || len(root.Chunks) == 0 || len(root.Chunks) > (credentialsMaxSnapshot+credentialsChunkBytes-1)/credentialsChunkBytes {
		return root, errors.New("Credentials Shared State Root 无效")
	}
	total := 0
	for _, chunk := range root.Chunks {
		if len(chunk.Digest) != sha256.Size*2 || chunk.Size < 1 || chunk.Size > credentialsChunkBytes {
			return root, errors.New("Credentials Shared State Root chunk 无效")
		}
		if _, err := hex.DecodeString(chunk.Digest); err != nil || strings.ToLower(chunk.Digest) != chunk.Digest {
			return root, errors.New("Credentials Shared State Root chunk 摘要无效")
		}
		total += chunk.Size
	}
	if total != root.Size {
		return root, errors.New("Credentials Shared State Root 总大小无效")
	}
	if _, err := hex.DecodeString(root.Digest); err != nil || strings.ToLower(root.Digest) != root.Digest {
		return root, errors.New("Credentials Shared State Root 摘要无效")
	}
	return root, nil
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

func decodeStrictJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("JSON 包含尾随数据")
	}
	return nil
}

func digestHex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
