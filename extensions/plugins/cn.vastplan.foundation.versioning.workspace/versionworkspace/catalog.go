package versionworkspace

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

type environmentEntry struct {
	profile  resourcev1.EnvironmentProfile
	digest   string
	bindings map[string]resourcev1.ResourceBinding
}

type Catalog struct {
	mu           sync.RWMutex
	environments map[string]map[string]environmentEntry
	current      map[string]string
	revisions    map[string]map[uint64]string
	adapters     map[string]Adapter
}

func NewCatalog() *Catalog {
	return &Catalog{
		environments: map[string]map[string]environmentEntry{},
		current:      map[string]string{},
		revisions:    map[string]map[uint64]string{},
		adapters:     map[string]Adapter{},
	}
}

func (c *Catalog) RegisterAdapter(adapter Adapter) error {
	if c == nil || adapter == nil {
		return errors.New("Version Resource Adapter 不能为空")
	}
	descriptor := adapter.Descriptor()
	if err := resourcev1.ValidateAdapterDescriptor(descriptor); err != nil {
		return fmt.Errorf("Version Resource Adapter 描述无效: %w", err)
	}
	_, hasDiff := adapter.(DiffAdapter)
	if descriptor.Capabilities.Diff != hasDiff {
		return fmt.Errorf("Version Resource Adapter %q 的 diff 能力声明与实现不一致", descriptor.ID)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.adapters[descriptor.ID]; exists {
		return fmt.Errorf("Version Resource Adapter %q 重复", descriptor.ID)
	}
	c.adapters[descriptor.ID] = adapter
	return nil
}

func (c *Catalog) RegisterEnvironment(profile resourcev1.EnvironmentProfile) error {
	if c == nil {
		return errors.New("Version Environment Catalog 不能为空")
	}
	if err := resourcev1.ValidateEnvironmentProfile(profile); err != nil {
		return err
	}
	digest, err := resourcev1.EnvironmentDigest(profile)
	if err != nil {
		return err
	}
	profile = cloneProfile(profile)
	c.mu.Lock()
	defer c.mu.Unlock()
	bindings := make(map[string]resourcev1.ResourceBinding, len(profile.Bindings))
	for _, binding := range profile.Bindings {
		adapter, ok := c.adapters[binding.Adapter]
		if !ok {
			return fmt.Errorf("Version Environment %q 引用未注册 Adapter %q", profile.ID, binding.Adapter)
		}
		if err := validateBindingAdapter(binding, adapter.Descriptor(), profile.Limits); err != nil {
			return fmt.Errorf("Version Environment %q: %w", profile.ID, err)
		}
		bindings[binding.ResourceType] = cloneBinding(binding)
	}
	if c.environments[profile.ID] == nil {
		c.environments[profile.ID] = map[string]environmentEntry{}
		c.revisions[profile.ID] = map[uint64]string{}
	}
	if existingDigest, exists := c.revisions[profile.ID][profile.Revision]; exists {
		if existingDigest == digest {
			return fmt.Errorf("Version Environment %q revision %d 重复", profile.ID, profile.Revision)
		}
		return fmt.Errorf("Version Environment %q revision %d 已绑定其他摘要", profile.ID, profile.Revision)
	}
	c.environments[profile.ID][digest] = environmentEntry{profile: profile, digest: digest, bindings: bindings}
	c.revisions[profile.ID][profile.Revision] = digest
	currentDigest, hasCurrent := c.current[profile.ID]
	if !hasCurrent || profile.Revision > c.environments[profile.ID][currentDigest].profile.Revision {
		c.current[profile.ID] = digest
	}
	return nil
}

func (c *Catalog) resolve(environmentID, resourceType string) (environmentEntry, resourcev1.ResourceBinding, Adapter, error) {
	return c.resolveEntry(environmentID, "", resourceType)
}

func (c *Catalog) resolveExact(environmentID, environmentDigest, resourceType string) (environmentEntry, resourcev1.ResourceBinding, Adapter, error) {
	return c.resolveEntry(environmentID, environmentDigest, resourceType)
}

func (c *Catalog) resolveEntry(environmentID, environmentDigest, resourceType string) (environmentEntry, resourcev1.ResourceBinding, Adapter, error) {
	if c == nil {
		return environmentEntry{}, resourcev1.ResourceBinding{}, nil, workspaceError(workspacev1.ErrorEnvironmentNotFound, false, errors.New("Version Environment Catalog 不可用"))
	}
	c.mu.RLock()
	versions, exists := c.environments[environmentID]
	if !exists {
		c.mu.RUnlock()
		return environmentEntry{}, resourcev1.ResourceBinding{}, nil, workspaceError(workspacev1.ErrorEnvironmentNotFound, false, fmt.Errorf("Version Environment %q 不存在", environmentID))
	}
	if environmentDigest == "" {
		environmentDigest = c.current[environmentID]
	}
	entry, exists := versions[environmentDigest]
	if !exists {
		c.mu.RUnlock()
		return environmentEntry{}, resourcev1.ResourceBinding{}, nil, workspaceError(workspacev1.ErrorEnvironmentNotFound, false, fmt.Errorf("Version Environment %q 摘要 %q 不存在", environmentID, environmentDigest))
	}
	binding, exists := entry.bindings[resourceType]
	var adapter Adapter
	if exists {
		adapter = c.adapters[binding.Adapter]
	}
	c.mu.RUnlock()
	if !exists {
		return environmentEntry{}, resourcev1.ResourceBinding{}, nil, workspaceError(workspacev1.ErrorResourceNotBound, false, fmt.Errorf("资源类型 %q 未绑定到环境 %q", resourceType, environmentID))
	}
	if adapter == nil {
		return environmentEntry{}, resourcev1.ResourceBinding{}, nil, workspaceError(workspacev1.ErrorAdapterUnavailable, true, fmt.Errorf("Adapter %q 不可用", binding.Adapter))
	}
	entry.profile = cloneProfile(entry.profile)
	binding = cloneBinding(binding)
	return entry, binding, adapter, nil
}

func validateBindingAdapter(binding resourcev1.ResourceBinding, descriptor resourcev1.AdapterDescriptor, limits resourcev1.WorkspaceLimits) error {
	configuredLimit := limits.MaxSnapshotBytes
	if descriptor.ContentKind == resourcev1.ContentFiles {
		configuredLimit = limits.MaxOverlayBytes
	}
	if configuredLimit > descriptor.MaxSnapshotBytes {
		return fmt.Errorf("环境快照上限超过 Adapter %q 上限", descriptor.ID)
	}
	supported := make(map[string]struct{}, len(descriptor.SupportedModes))
	for _, mode := range descriptor.SupportedModes {
		supported[mode] = struct{}{}
	}
	for _, mode := range binding.AllowedModes {
		if _, ok := supported[mode]; !ok {
			return fmt.Errorf("Adapter %q 不支持模式 %q", descriptor.ID, mode)
		}
	}
	return nil
}

func cloneProfile(profile resourcev1.EnvironmentProfile) resourcev1.EnvironmentProfile {
	profile.Bindings = append([]resourcev1.ResourceBinding(nil), profile.Bindings...)
	for index := range profile.Bindings {
		profile.Bindings[index] = cloneBinding(profile.Bindings[index])
	}
	return profile
}

func cloneBinding(binding resourcev1.ResourceBinding) resourcev1.ResourceBinding {
	binding.AllowedModes = append([]string(nil), binding.AllowedModes...)
	binding.AdapterConfig = append([]byte(nil), binding.AdapterConfig...)
	return binding
}

func containsMode(modes []string, wanted string) bool {
	index := sort.SearchStrings(modes, wanted)
	if index < len(modes) && modes[index] == wanted {
		return true
	}
	for _, mode := range modes {
		if mode == wanted {
			return true
		}
	}
	return false
}
