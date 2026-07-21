// Package bootstrapinventory defines the root-owned bridge from an offline
// Seed repository to managed-repository GC protection. It contains identities
// only; every artifact is still fetched from Seed and verified before publish.
package bootstrapinventory

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactreference"
	"cdsoft.com.cn/VastPlan/core/shared/go/configfile"
)

const Version = 1

type Item struct {
	Ref    pluginv1.ArtifactRef `json:"ref"`
	SHA256 string               `json:"sha256"`
}

type Inventory struct {
	Version       int    `json:"version"`
	Generation    uint64 `json:"generation"`
	RepositoryID  string `json:"repositoryId"`
	Seed          []Item `json:"seed"`
	LastKnownGood []Item `json:"lastKnownGood"`
}

func ParseFile(filename string) (Inventory, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return Inventory{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o027 != 0 {
		return Inventory{}, errors.New("Bootstrap Inventory 必须是非符号链接且不可被 group/other 写入或被 other 读取的普通文件")
	}
	raw, err := configfile.Load(filename)
	if err != nil {
		return Inventory{}, err
	}
	var value Inventory
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return Inventory{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Inventory{}, errors.New("Bootstrap Inventory 只能包含一个文档")
	}
	return Normalize(value)
}

func Normalize(value Inventory) (Inventory, error) {
	if value.Version != Version || value.Generation == 0 || strings.TrimSpace(value.RepositoryID) == "" || len(value.Seed) == 0 || len(value.LastKnownGood) == 0 {
		return Inventory{}, errors.New("Bootstrap Inventory 身份、generation、Seed 或 LKG 无效")
	}
	seed, err := normalizeItems(value.Seed)
	if err != nil {
		return Inventory{}, fmt.Errorf("Seed inventory: %w", err)
	}
	lkg, err := normalizeItems(value.LastKnownGood)
	if err != nil {
		return Inventory{}, fmt.Errorf("LKG inventory: %w", err)
	}
	seedSet := map[string]struct{}{}
	for _, item := range seed {
		seedSet[itemKey(item)] = struct{}{}
	}
	for _, item := range lkg {
		if _, ok := seedSet[itemKey(item)]; !ok {
			return Inventory{}, errors.New("LKG 必须是 Seed 精确 inventory 的子集")
		}
	}
	value.Seed, value.LastKnownGood = seed, lkg
	if _, err := artifactreference.Seal(value.SeedSnapshot()); err != nil {
		return Inventory{}, err
	}
	if _, err := artifactreference.Seal(value.LastKnownGoodSnapshot()); err != nil {
		return Inventory{}, err
	}
	return value, nil
}

func (value Inventory) SeedSnapshot() pluginv1.ArtifactReferenceSnapshot {
	return snapshot(artifactreference.OwnerSeed, "seed/"+value.RepositoryID, value.Generation, value.Seed, "seed")
}

func (value Inventory) LastKnownGoodSnapshot() pluginv1.ArtifactReferenceSnapshot {
	return snapshot(artifactreference.OwnerLastKnownGood, "lkg/"+value.RepositoryID, value.Generation, value.LastKnownGood, "last-known-good")
}

func snapshot(kind, id string, generation uint64, items []Item, purpose string) pluginv1.ArtifactReferenceSnapshot {
	references := make([]pluginv1.ArtifactReference, len(items))
	for i, item := range items {
		references[i] = pluginv1.ArtifactReference{Ref: item.Ref, SHA256: item.SHA256, Purpose: purpose}
	}
	return pluginv1.ArtifactReferenceSnapshot{OwnerKind: kind, OwnerID: id, Generation: generation, References: references}
}

func normalizeItems(values []Item) ([]Item, error) {
	out := append([]Item(nil), values...)
	for i := range out {
		if out[i].Ref.Channel == "" {
			out[i].Ref.Channel = "stable"
		}
		out[i].SHA256 = strings.ToLower(out[i].SHA256)
	}
	sort.Slice(out, func(i, j int) bool { return itemKey(out[i]) < itemKey(out[j]) })
	for i := range out {
		if i > 0 && out[i].Ref == out[i-1].Ref {
			return nil, errors.New("Bootstrap Inventory 包含重复 ref")
		}
	}
	return out, nil
}

func itemKey(item Item) string {
	return item.Ref.PluginID + "@" + item.Ref.Version + "/" + item.Ref.Channel + "\x00" + item.SHA256
}
