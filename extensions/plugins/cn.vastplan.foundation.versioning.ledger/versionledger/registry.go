package versionledger

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"sync"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

var providerInstancePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

type Registry struct {
	mu        sync.RWMutex
	providers map[string]registeredProvider
}

type registeredProvider struct {
	provider   Provider
	descriptor versioningv1.ProviderDescriptor
}

func NewRegistry() *Registry { return &Registry{providers: map[string]registeredProvider{}} }

func (r *Registry) Register(instanceID string, provider Provider) error {
	if r == nil || !providerInstancePattern.MatchString(instanceID) || nilInterface(provider) {
		return errors.New("Version Provider instance 或实现无效")
	}
	descriptor := provider.Descriptor()
	if err := versioningv1.ValidateProviderDescriptor(descriptor); err != nil {
		return fmt.Errorf("Version Provider descriptor: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[instanceID]; exists {
		return fmt.Errorf("Version Provider instance %q 重复注册", instanceID)
	}
	for _, registered := range r.providers {
		if registered.descriptor.ID == descriptor.ID && !reflect.DeepEqual(registered.descriptor, descriptor) {
			return fmt.Errorf("Version Provider 类型 %q 的 descriptor 在实例间不一致", descriptor.ID)
		}
	}
	descriptor.ConfigurationSchema = append([]byte(nil), descriptor.ConfigurationSchema...)
	r.providers[instanceID] = registeredProvider{provider: provider, descriptor: descriptor}
	return nil
}

func (r *Registry) Resolve(instanceID string) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	registered, ok := r.providers[instanceID]
	r.mu.RUnlock()
	return registered.provider, ok
}

func (r *Registry) Descriptors() []versioningv1.ProviderDescriptor {
	if r == nil {
		return []versioningv1.ProviderDescriptor{}
	}
	r.mu.RLock()
	byType := make(map[string]versioningv1.ProviderDescriptor, len(r.providers))
	for _, registered := range r.providers {
		descriptor := registered.descriptor
		descriptor.ConfigurationSchema = append([]byte(nil), descriptor.ConfigurationSchema...)
		byType[descriptor.ID] = descriptor
	}
	r.mu.RUnlock()
	descriptors := make([]versioningv1.ProviderDescriptor, 0, len(byType))
	for _, descriptor := range byType {
		descriptors = append(descriptors, descriptor)
	}
	sort.Slice(descriptors, func(i, j int) bool { return descriptors[i].ID < descriptors[j].ID })
	return descriptors
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
