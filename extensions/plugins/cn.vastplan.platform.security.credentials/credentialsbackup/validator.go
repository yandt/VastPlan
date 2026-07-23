// Package credentialsbackup validates Credentials Root/chunk reachability as
// part of trusted Shared State backup and post-restore verification.
package credentialsbackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/nats-io/nats.go/jetstream"

	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstatebackup"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.credentials/credentialsstate"
)

const ValidatorName = "credentials.root-chunks.v1"

type Factory struct{}

func (Factory) NewValidator(kv jetstream.KeyValue) sharedstatebackup.Validator {
	return &validator{kv: kv, roots: map[scopeKey]credentialsstate.Root{}, blobs: map[scopeKey]map[string]blobMeta{}}
}

type scopeKey struct {
	Kind         sharedstate.ScopeKind
	TenantID     string
	RuntimeScope string
}

type blobMeta struct {
	physical string
	revision uint64
	size     int
}

type validator struct {
	kv    jetstream.KeyValue
	roots map[scopeKey]credentialsstate.Root
	blobs map[scopeKey]map[string]blobMeta
}

func (v *validator) Name() string { return ValidatorName }

func (v *validator) Observe(entry sharedstatebackup.LogicalEntry) error {
	if entry.Scope.PluginID != credentialsstate.PluginID || entry.Scope.Namespace != credentialsstate.Namespace {
		return nil
	}
	if entry.Scope.Kind != sharedstate.ScopeTenant || entry.Scope.TenantID == "" {
		return errors.New("Credentials Shared State 必须使用 tenant scope")
	}
	key := scopeKey{Kind: entry.Scope.Kind, TenantID: entry.Scope.TenantID, RuntimeScope: entry.Scope.RuntimeScope}
	switch {
	case entry.Key == credentialsstate.RootKey:
		if _, exists := v.roots[key]; exists {
			return errors.New("同一 Credentials scope 出现重复 Root")
		}
		root, err := credentialsstate.ParseRoot(entry.Value)
		if err != nil {
			return err
		}
		v.roots[key] = root
		return nil
	case strings.HasPrefix(entry.Key, credentialsstate.BlobPrefix):
		digest := strings.TrimPrefix(entry.Key, credentialsstate.BlobPrefix)
		if digest == "" || credentialsstate.DigestHex(entry.Value) != digest {
			return errors.New("Credentials chunk key、内容摘要不一致")
		}
		if v.blobs[key] == nil {
			v.blobs[key] = map[string]blobMeta{}
		}
		if _, exists := v.blobs[key][digest]; exists {
			return errors.New("同一 Credentials scope 出现重复 chunk")
		}
		v.blobs[key][digest] = blobMeta{physical: entry.PhysicalKey, revision: entry.Revision, size: len(entry.Value)}
		return nil
	default:
		return fmt.Errorf("Credentials Shared State 出现未知 key %q", entry.Key)
	}
}

func (v *validator) Finish(ctx context.Context) (sharedstatebackup.ValidationResult, error) {
	if v.kv == nil {
		return sharedstatebackup.ValidationResult{}, errors.New("Credentials 备份验证缺少 KV")
	}
	result := sharedstatebackup.ValidationResult{Name: ValidatorName, Counters: map[string]uint64{}}
	result.Counters["roots"] = uint64(len(v.roots))
	referenced := map[scopeKey]map[string]struct{}{}
	for scope, root := range v.roots {
		digest := sha256.New()
		total := 0
		for _, chunk := range root.Chunks {
			meta, exists := v.blobs[scope][chunk.Digest]
			if !exists || meta.size != chunk.Size {
				return sharedstatebackup.ValidationResult{}, errors.New("Credentials Root 引用缺失或大小不符的 chunk")
			}
			entry, err := v.kv.Get(ctx, meta.physical)
			if err != nil || entry.Revision() != meta.revision || len(entry.Value()) != chunk.Size || credentialsstate.DigestHex(entry.Value()) != chunk.Digest {
				return sharedstatebackup.ValidationResult{}, errors.New("Credentials chunk 在完整性复核期间发生变化")
			}
			_, _ = digest.Write(entry.Value())
			total += len(entry.Value())
			if referenced[scope] == nil {
				referenced[scope] = map[string]struct{}{}
			}
			referenced[scope][chunk.Digest] = struct{}{}
		}
		if total != root.Size || hex.EncodeToString(digest.Sum(nil)) != root.Digest {
			return sharedstatebackup.ValidationResult{}, errors.New("Credentials Root 完整快照摘要不一致")
		}
	}
	var chunks uint64
	var referencedChunks uint64
	for _, values := range v.blobs {
		chunks += uint64(len(values))
	}
	for _, values := range referenced {
		referencedChunks += uint64(len(values))
	}
	result.Counters["chunks"] = chunks
	result.Counters["referencedChunks"] = referencedChunks
	result.Counters["orphanChunks"] = chunks - referencedChunks
	return result, nil
}
