package edge

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

type deliverySnapshot struct {
	SpecSHA256 string                `json:"specSha256"`
	Runtime    portalapi.RuntimeSpec `json:"runtime"`
}

// frontendDeliveryStore owns immutable, content-addressed browser objects and
// revision snapshots. An empty root is an in-memory store used by unit tests;
// production supplies a private persistent root.
type frontendDeliveryStore struct {
	root      string
	mu        sync.RWMutex
	snapshots map[string]deliverySnapshot
	objects   map[string]FrontendModuleAsset
}

func newFrontendDeliveryStore(root string) (*frontendDeliveryStore, error) {
	if root != "" {
		if err := os.MkdirAll(filepath.Join(root, "objects"), 0o700); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Join(root, "snapshots"), 0o700); err != nil {
			return nil, err
		}
	}
	return &frontendDeliveryStore{root: root, snapshots: map[string]deliverySnapshot{}, objects: map[string]FrontendModuleAsset{}}, nil
}

func (s *frontendDeliveryStore) put(tenantID string, spec portalapi.PortalSpec, assets []FrontendModuleAsset) error {
	digest, err := portalSpecDigest(spec)
	if err != nil {
		return err
	}
	runtime := portalapi.RuntimeSpec{Portal: spec, Modules: make([]portalapi.FrontendModule, 0, len(assets))}
	for i := range assets {
		asset := assets[i]
		asset.Descriptor.URL = fmt.Sprintf("/v1/portal-modules/%d/%s.js", spec.Revision, asset.Descriptor.SHA256)
		assets[i] = asset
		runtime.Modules = append(runtime.Modules, asset.Descriptor)
	}
	snapshot := deliverySnapshot{SpecSHA256: digest, Runtime: runtime}
	key := deliveryKey(tenantID, spec.ID, spec.Revision)

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, asset := range assets {
		s.objects[asset.Descriptor.SHA256] = cloneFrontendAsset(asset)
		if s.root != "" {
			if err := s.writeObject(asset); err != nil {
				return err
			}
		}
	}
	if s.root != "" {
		raw, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		if err := writeAtomic(s.snapshotPath(key), raw, 0o600); err != nil {
			return err
		}
	}
	s.snapshots[key] = snapshot
	return nil
}

func (s *frontendDeliveryStore) runtime(tenantID string, spec portalapi.PortalSpec) (portalapi.RuntimeSpec, error) {
	snapshot, err := s.snapshot(deliveryKey(tenantID, spec.ID, spec.Revision))
	if err != nil {
		return portalapi.RuntimeSpec{}, err
	}
	digest, err := portalSpecDigest(spec)
	if err != nil || digest != snapshot.SpecSHA256 {
		return portalapi.RuntimeSpec{}, errors.New("Portal 交付快照与活动解析锁不一致")
	}
	return snapshot.Runtime, nil
}

func (s *frontendDeliveryStore) module(tenantID string, spec portalapi.PortalSpec, digest string) (FrontendModuleAsset, error) {
	runtime, err := s.runtime(tenantID, spec)
	if err != nil {
		return FrontendModuleAsset{}, err
	}
	var descriptor portalapi.FrontendModule
	for _, candidate := range runtime.Modules {
		if candidate.SHA256 == digest {
			descriptor = candidate
			break
		}
	}
	if descriptor.ID == "" {
		return FrontendModuleAsset{}, errors.New("Portal 快照未授权该内容对象")
	}

	s.mu.RLock()
	asset, ok := s.objects[digest]
	s.mu.RUnlock()
	if ok {
		return cloneFrontendAsset(asset), nil
	}
	if s.root == "" {
		return FrontendModuleAsset{}, os.ErrNotExist
	}
	raw, err := os.ReadFile(s.objectPath(digest, ".js"))
	if err != nil {
		return FrontendModuleAsset{}, err
	}
	actual := sha256.Sum256(raw)
	if hex.EncodeToString(actual[:]) != digest {
		return FrontendModuleAsset{}, errors.New("Portal 内容寻址对象摘要失配")
	}
	gz, _ := os.ReadFile(s.objectPath(digest, ".js.gz"))
	asset = FrontendModuleAsset{Descriptor: descriptor, Content: raw, GzipContent: gz}
	s.mu.Lock()
	s.objects[digest] = cloneFrontendAsset(asset)
	s.mu.Unlock()
	return asset, nil
}

func (s *frontendDeliveryStore) snapshot(key string) (deliverySnapshot, error) {
	s.mu.RLock()
	snapshot, ok := s.snapshots[key]
	s.mu.RUnlock()
	if ok {
		return snapshot, nil
	}
	if s.root == "" {
		return deliverySnapshot{}, errors.New("Portal revision 尚未物化")
	}
	raw, err := os.ReadFile(s.snapshotPath(key))
	if err != nil {
		return deliverySnapshot{}, fmt.Errorf("读取 Portal 交付快照: %w", err)
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return deliverySnapshot{}, fmt.Errorf("解析 Portal 交付快照: %w", err)
	}
	s.mu.Lock()
	s.snapshots[key] = snapshot
	s.mu.Unlock()
	return snapshot, nil
}

func (s *frontendDeliveryStore) writeObject(asset FrontendModuleAsset) error {
	if len(asset.Descriptor.SHA256) != 64 {
		return errors.New("Portal 对象摘要无效")
	}
	if err := writeAtomic(s.objectPath(asset.Descriptor.SHA256, ".js"), asset.Content, 0o600); err != nil {
		return err
	}
	if len(asset.GzipContent) > 0 {
		return writeAtomic(s.objectPath(asset.Descriptor.SHA256, ".js.gz"), asset.GzipContent, 0o600)
	}
	return nil
}

func (s *frontendDeliveryStore) objectPath(digest, suffix string) string {
	return filepath.Join(s.root, "objects", digest[:2], digest+suffix)
}
func (s *frontendDeliveryStore) snapshotPath(key string) string {
	return filepath.Join(s.root, "snapshots", key+".json")
}

func deliveryKey(tenantID, portalID string, revision uint64) string {
	digest := sha256.Sum256([]byte(tenantID + "\x00" + portalID))
	return filepath.Join(hex.EncodeToString(digest[:]), fmt.Sprint(revision))
}
func portalSpecDigest(spec portalapi.PortalSpec) (string, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}
func gzipBytes(raw []byte) ([]byte, error) {
	var target bytes.Buffer
	writer, err := gzip.NewWriterLevel(&target, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(raw); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return target.Bytes(), nil
}
func writeAtomic(path string, raw []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil {
		if !bytes.Equal(existing, raw) {
			return errors.New("Portal 不可变交付对象发生内容冲突")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".delivery-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
func cloneFrontendAsset(asset FrontendModuleAsset) FrontendModuleAsset {
	asset.Content = append([]byte(nil), asset.Content...)
	asset.GzipContent = append([]byte(nil), asset.GzipContent...)
	return asset
}
