package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	sharedstatesdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/sharedstate"
)

const (
	managementStateNamespace = "authentication.provider.management.v1"
	managementStateKey       = "state"
	maximumManagementBytes   = 1 << 20
)

type ManagementStore interface {
	LoadState() (ManagementState, error)
	UpdateState(expected uint64, next ManagementState) (ManagementState, error)
}

type contextualManagementStore interface {
	Bind(context.Context, sdk.Host, *contractv1.CallContext) ManagementStore
}

func bindManagementStore(ctx context.Context, store ManagementStore, host sdk.Host, call *contractv1.CallContext) ManagementStore {
	if contextual, ok := store.(contextualManagementStore); ok {
		return contextual.Bind(ctx, host, call)
	}
	return store
}

// SharedManagementStore is the production SQL Shared State adapter. Bind
// captures only the current trusted invocation; workflows keep depending on
// the small synchronous ManagementStore port and remain storage-agnostic.
type SharedManagementStore struct{}

type boundSharedManagementStore struct {
	ctx  context.Context
	host sdk.Host
	call *contractv1.CallContext
}

func (*SharedManagementStore) Bind(ctx context.Context, host sdk.Host, call *contractv1.CallContext) ManagementStore {
	return &boundSharedManagementStore{ctx: ctx, host: host, call: call}
}

func (*SharedManagementStore) LoadState() (ManagementState, error) {
	return ManagementState{}, errors.New("Provider Management Shared State 尚未绑定可信调用")
}

func (*SharedManagementStore) UpdateState(uint64, ManagementState) (ManagementState, error) {
	return ManagementState{}, errors.New("Provider Management Shared State 尚未绑定可信调用")
}

func (s *boundSharedManagementStore) client() (*sharedstatesdk.Client, error) {
	return sharedstatesdk.NewFenced(s.host, "service", managementStateNamespace)
}

func (s *boundSharedManagementStore) LoadState() (ManagementState, error) {
	client, err := s.client()
	if err != nil {
		return ManagementState{}, err
	}
	entry, err := client.Get(s.ctx, s.call, managementStateKey)
	if sharedstatesdk.IsNotFound(err) {
		return emptyManagementState(), nil
	}
	if err != nil {
		return ManagementState{}, err
	}
	state, err := decodeManagementState(entry.Value)
	if err != nil {
		return ManagementState{}, err
	}
	if state.Generation != entry.Revision {
		return ManagementState{}, errors.New("Provider Management generation 与 Shared State revision 不一致")
	}
	return state, nil
}

func (s *boundSharedManagementStore) UpdateState(expected uint64, next ManagementState) (ManagementState, error) {
	client, err := s.client()
	if err != nil {
		return ManagementState{}, err
	}
	var current ManagementState
	var revision uint64
	entry, err := client.Get(s.ctx, s.call, managementStateKey)
	if sharedstatesdk.IsNotFound(err) {
		current = emptyManagementState()
	} else if err != nil {
		return ManagementState{}, err
	} else {
		current, err = decodeManagementState(entry.Value)
		if err != nil {
			return ManagementState{}, err
		}
		revision = entry.Revision
		if current.Generation != revision {
			return ManagementState{}, errors.New("Provider Management generation 与 Shared State revision 不一致")
		}
	}
	if current.Generation == next.Generation && reflect.DeepEqual(current, next) {
		return current, nil
	}
	if current.Generation != expected || revision != expected || next.Generation != expected+1 || next.Version != managementStateVersion {
		return ManagementState{}, fmt.Errorf("Provider Management CAS 冲突: expected=%d actual=%d next=%d", expected, current.Generation, next.Generation)
	}
	raw, err := json.Marshal(next)
	if err != nil || len(raw) > maximumManagementBytes {
		return ManagementState{}, errors.New("Provider Management 状态超过 Shared State 上限")
	}
	if expected == 0 {
		entry, err = client.Create(s.ctx, s.call, managementStateKey, raw)
	} else {
		entry, err = client.Update(s.ctx, s.call, managementStateKey, raw, revision)
	}
	if err != nil {
		return ManagementState{}, err
	}
	if entry.Revision != next.Generation {
		return ManagementState{}, errors.New("Provider Management Shared State revision 未按 generation 推进")
	}
	return next, nil
}

type FileManagementStore struct {
	Path string
	mu   sync.Mutex
}

func (s *FileManagementStore) LoadState() (ManagementState, error) {
	if err := validateManagementPath(s.Path); err != nil {
		return ManagementState{}, err
	}
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return emptyManagementState(), nil
	}
	if err != nil {
		return ManagementState{}, err
	}
	info, err := os.Lstat(s.Path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return ManagementState{}, errors.New("Provider Management 状态必须是 owner-only 普通文件")
	}
	return decodeManagementState(raw)
}

func (s *FileManagementStore) UpdateState(expected uint64, next ManagementState) (ManagementState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.LoadState()
	if err != nil {
		return ManagementState{}, err
	}
	if current.Generation == next.Generation && reflect.DeepEqual(current, next) {
		return current, nil
	}
	if current.Generation != expected || next.Generation != expected+1 || next.Version != managementStateVersion {
		return ManagementState{}, fmt.Errorf("Provider Management CAS 冲突: expected=%d actual=%d next=%d", expected, current.Generation, next.Generation)
	}
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return ManagementState{}, err
	}
	dir := filepath.Dir(s.Path)
	file, err := os.CreateTemp(dir, ".authentication-providers-*")
	if err != nil {
		return ManagementState{}, err
	}
	temporary := file.Name()
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return ManagementState{}, err
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		return ManagementState{}, err
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		return ManagementState{}, err
	}
	if err := os.Rename(temporary, s.Path); err != nil {
		return ManagementState{}, err
	}
	committed = true
	directory, err := os.Open(dir)
	if err != nil {
		return ManagementState{}, err
	}
	if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
		return ManagementState{}, err
	}
	return next, nil
}

func emptyManagementState() ManagementState {
	return ManagementState{Version: managementStateVersion, Providers: []ManagedProvider{}}
}

func decodeManagementState(raw []byte) (ManagementState, error) {
	if len(raw) == 0 || len(raw) > maximumManagementBytes {
		return ManagementState{}, errors.New("Provider Management 状态大小无效")
	}
	var state ManagementState
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return ManagementState{}, fmt.Errorf("解析 Provider Management 状态: %w", err)
	}
	if state.Version != managementStateVersion || state.Generation == 0 && len(state.Providers) != 0 {
		return ManagementState{}, errors.New("Provider Management 状态版本或 generation 无效")
	}
	return state, nil
}

func validateManagementPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Ext(path) != ".json" {
		return errors.New("Provider Management Store 必须是规范绝对 JSON 路径")
	}
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("Provider Management Store 目录必须不可被 group/other 写入")
	}
	return nil
}

type StateCatalog struct{ Store ManagementStore }

func (c StateCatalog) Bind(ctx context.Context, host sdk.Host, call *contractv1.CallContext) Catalog {
	return StateCatalog{Store: bindManagementStore(ctx, c.Store, host, call)}
}

func (c StateCatalog) Load() (authenticationv1.AuthenticationProviderCatalog, error) {
	state, err := c.Store.LoadState()
	if err != nil {
		return authenticationv1.AuthenticationProviderCatalog{}, err
	}
	if state.Catalog == nil {
		return authenticationv1.AuthenticationProviderCatalog{}, ErrCatalogNotPublished
	}
	return *state.Catalog, nil
}

func (c StateCatalog) ResolveTestProfile(profileID, methodID string) (authenticationv1.ProviderCatalogEntry, bool, error) {
	state, err := c.Store.LoadState()
	if err != nil {
		return authenticationv1.ProviderCatalogEntry{}, false, err
	}
	for _, provider := range state.Providers {
		if provider.Profile.ID != profileID || provider.Lifecycle.State != authenticationv1.ProviderValidated {
			continue
		}
		for _, method := range provider.Profile.Methods {
			if method == methodID {
				return authenticationv1.ProviderCatalogEntry{Profile: provider.Lifecycle.Profile, ContributionID: provider.Profile.ContributionID, Purposes: append([]authenticationv1.ProviderPurpose(nil), provider.Profile.Purposes...), Methods: append([]string(nil), provider.Profile.Methods...), SubjectNamespace: provider.Profile.SubjectNamespace, RequiredCapabilities: append([]string(nil), provider.Profile.RequiredCapabilities...)}, true, nil
			}
		}
	}
	return authenticationv1.ProviderCatalogEntry{}, false, nil
}
