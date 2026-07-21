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
	Server     serverRuntimeSpec     `json:"server,omitempty"`
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

func (s *frontendDeliveryStore) put(tenantID string, spec portalapi.PortalSpec, runtime portalapi.RuntimeSpec, assets []FrontendModuleAsset) error {
	return s.putSealed(tenantID, spec, runtime, serverRuntimeSpec{}, assets)
}

func (s *frontendDeliveryStore) putSealed(tenantID string, spec portalapi.PortalSpec, runtime portalapi.RuntimeSpec, server serverRuntimeSpec, assets []FrontendModuleAsset) error {
	digest, err := portalSpecDigest(spec)
	if err != nil {
		return err
	}
	runtime.Portal = spec
	assetsByDigest := make(map[string]FrontendModuleAsset, len(assets))
	for i := range assets {
		asset := assets[i]
		actual := sha256.Sum256(asset.Content)
		if hex.EncodeToString(actual[:]) != asset.Descriptor.SHA256 {
			return errors.New("Portal 交付对象与声明摘要不一致")
		}
		assets[i] = asset
		assetsByDigest[asset.Descriptor.SHA256] = asset
	}
	browserURLs := make(map[string]string)
	for _, descriptor := range runtimeFrontendObjects(runtime) {
		browserURLs[descriptor.SHA256] = frontendObjectURL(spec.Revision, descriptor.SHA256, descriptor.MediaType)
	}
	serverURLs := make(map[string]string)
	for _, descriptor := range serverRuntimeObjects(server) {
		serverURLs[descriptor.SHA256] = serverObjectURL(descriptor.SHA256)
	}
	if err := applyFrontendObjectURLs(&runtime, browserURLs); err != nil {
		return err
	}
	if err := applyServerObjectURLs(&server, serverURLs); err != nil {
		return err
	}
	referenced := make(map[string]struct{}, len(browserURLs)+len(serverURLs))
	for digest := range browserURLs {
		referenced[digest] = struct{}{}
	}
	for digest := range serverURLs {
		referenced[digest] = struct{}{}
	}
	if len(referenced) != len(assetsByDigest) {
		return fmt.Errorf("Portal 交付对象与 browser/server Runtime 引用集合不一致")
	}
	for digest := range referenced {
		if _, ok := assetsByDigest[digest]; !ok {
			return fmt.Errorf("Portal 交付缺少内容对象: %s", digest)
		}
	}
	snapshot := deliverySnapshot{SpecSHA256: digest, Runtime: cloneFrontendRuntime(runtime), Server: cloneServerRuntime(server)}
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
	snapshot, err := s.sealedSnapshot(tenantID, spec)
	if err != nil {
		return portalapi.RuntimeSpec{}, err
	}
	return cloneFrontendRuntime(snapshot.Runtime), nil
}

func (s *frontendDeliveryStore) serverRuntime(tenantID string, spec portalapi.PortalSpec) (serverRuntimeSpec, error) {
	snapshot, err := s.sealedSnapshot(tenantID, spec)
	if err != nil {
		return serverRuntimeSpec{}, err
	}
	return cloneServerRuntime(snapshot.Server), nil
}

func (s *frontendDeliveryStore) sealedSnapshot(tenantID string, spec portalapi.PortalSpec) (deliverySnapshot, error) {
	snapshot, err := s.snapshot(deliveryKey(tenantID, spec.ID, spec.Revision))
	if err != nil {
		return deliverySnapshot{}, err
	}
	digest, err := portalSpecDigest(spec)
	if err != nil || digest != snapshot.SpecSHA256 {
		return deliverySnapshot{}, errors.New("Portal 交付快照与活动解析锁不一致")
	}
	return deliverySnapshot{SpecSHA256: snapshot.SpecSHA256, Runtime: cloneFrontendRuntime(snapshot.Runtime), Server: cloneServerRuntime(snapshot.Server)}, nil
}

func (s *frontendDeliveryStore) prefetchFrom(origin *frontendDeliveryStore, tenantID string, spec portalapi.PortalSpec) error {
	if origin == nil {
		return errors.New("Portal 中央交付 origin 未配置")
	}
	snapshot, err := origin.sealedSnapshot(tenantID, spec)
	if err != nil {
		return fmt.Errorf("读取 Portal 中央快照: %w", err)
	}
	descriptors := append(runtimeFrontendObjects(snapshot.Runtime), serverRuntimeObjects(snapshot.Server)...)
	assets := make([]FrontendModuleAsset, 0, len(descriptors))
	seen := make(map[string]struct{}, len(descriptors))
	for _, descriptor := range descriptors {
		if _, ok := seen[descriptor.SHA256]; ok {
			continue
		}
		seen[descriptor.SHA256] = struct{}{}
		asset, err := origin.sealedObject(snapshot, descriptor.SHA256)
		if err != nil {
			return fmt.Errorf("预取 Portal 内容对象 %s: %w", descriptor.SHA256, err)
		}
		actual := sha256.Sum256(asset.Content)
		if hex.EncodeToString(actual[:]) != descriptor.SHA256 {
			return errors.New("Portal 预取内容对象与中央快照摘要不一致")
		}
		// PackageSHA256 belongs to the Runtime descriptor, not the content
		// object. Two packages can produce the same JavaScript bytes while
		// retaining different package provenance in the snapshot.
		asset.Descriptor = descriptor
		assets = append(assets, asset)
	}
	// put writes every object first and the revision snapshot last. The local
	// revision therefore becomes visible only after the full module set exists.
	return s.putSealed(tenantID, spec, snapshot.Runtime, snapshot.Server, assets)
}

func (s *frontendDeliveryStore) module(tenantID string, spec portalapi.PortalSpec, digest string) (FrontendModuleAsset, error) {
	runtime, err := s.runtime(tenantID, spec)
	if err != nil {
		return FrontendModuleAsset{}, err
	}
	descriptor := findRuntimeFrontendObject(runtime, digest)
	if descriptor.ID == "" {
		return FrontendModuleAsset{}, errors.New("Portal 快照未授权该内容对象")
	}

	return s.readObject(descriptor)
}

func (s *frontendDeliveryStore) sealedObject(snapshot deliverySnapshot, digest string) (FrontendModuleAsset, error) {
	descriptor := findRuntimeFrontendObject(snapshot.Runtime, digest)
	if descriptor.ID == "" {
		for _, candidate := range serverRuntimeObjects(snapshot.Server) {
			if candidate.SHA256 == digest {
				descriptor = candidate
				break
			}
		}
	}
	if descriptor.ID == "" {
		return FrontendModuleAsset{}, errors.New("Portal 密封快照未授权该内容对象")
	}
	return s.readObject(descriptor)
}

func (s *frontendDeliveryStore) readObject(descriptor portalapi.FrontendModule) (FrontendModuleAsset, error) {
	digest := descriptor.SHA256
	s.mu.RLock()
	asset, ok := s.objects[digest]
	s.mu.RUnlock()
	if ok {
		// The content digest identifies bytes, not a plugin. Different plugin
		// packages may intentionally produce identical entry bytes, so always
		// take authorization metadata from this revision's Runtime snapshot.
		asset.Descriptor = descriptor
		return cloneFrontendAsset(asset), nil
	}
	if s.root == "" {
		return FrontendModuleAsset{}, os.ErrNotExist
	}
	raw, err := os.ReadFile(s.objectPath(digest, ".blob"))
	if err != nil {
		return FrontendModuleAsset{}, err
	}
	actual := sha256.Sum256(raw)
	if hex.EncodeToString(actual[:]) != digest {
		return FrontendModuleAsset{}, errors.New("Portal 内容寻址对象摘要失配")
	}
	gz, _ := os.ReadFile(s.objectPath(digest, ".blob.gz"))
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
	if err := writeAtomic(s.objectPath(asset.Descriptor.SHA256, ".blob"), asset.Content, 0o600); err != nil {
		return err
	}
	if len(asset.GzipContent) > 0 {
		return writeAtomic(s.objectPath(asset.Descriptor.SHA256, ".blob.gz"), asset.GzipContent, 0o600)
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
