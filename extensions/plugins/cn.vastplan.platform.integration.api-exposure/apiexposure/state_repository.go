package apiexposure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	sharedstatesdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/sharedstate"
)

const (
	sharedStateNamespace = "api-exposure.control"
	sharedStateKey       = "active"
)

var (
	ErrStoreUnavailable = errors.New("API Exposure Shared State 不可用")
	ErrStoreConflict    = errors.New("API Exposure Shared State 并发冲突")
)

type apiExposureStateSession struct {
	ctx        context.Context
	call       *contractv1.CallContext
	repository *apiExposureStateRepository
	revision   uint64
}

type apiExposureStateRepository struct{ client *sharedstatesdk.Client }

func newAPIExposureStateRepository(host sdk.Host) (*apiExposureStateRepository, error) {
	client, err := sharedstatesdk.NewFenced(host, "service", sharedStateNamespace)
	if err != nil {
		return nil, err
	}
	return &apiExposureStateRepository{client: client}, nil
}

func (r *apiExposureStateRepository) load(ctx context.Context, call *contractv1.CallContext) (persistedState, uint64, error) {
	entry, err := r.client.Get(ctx, call, sharedStateKey)
	if sharedstatesdk.IsNotFound(err) {
		return emptyPersistedState(), 0, nil
	}
	if err != nil {
		return persistedState{}, 0, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	state, err := decodePersistedState(entry.Value)
	if err != nil {
		return persistedState{}, 0, err
	}
	return state, entry.Revision, nil
}

func (r *apiExposureStateRepository) save(ctx context.Context, call *contractv1.CallContext, state persistedState, expected uint64) (uint64, error) {
	raw, err := json.Marshal(state)
	if err != nil {
		return 0, err
	}
	if len(raw) > maximumStateBytes {
		return 0, errors.New("API Exposure 治理聚合超过 Shared State 单值 1 MiB 上限")
	}
	var entry sharedstatesdk.Entry
	if expected == 0 {
		entry, err = r.client.Create(ctx, call, sharedStateKey, raw)
	} else {
		entry, err = r.client.Update(ctx, call, sharedStateKey, raw, expected)
	}
	if sharedstatesdk.IsConflict(err) {
		return 0, ErrStoreConflict
	}
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return entry.Revision, nil
}

func emptyPersistedState() persistedState {
	return persistedState{FormatVersion: stateFormatVersion, CatalogGeneration: 1, Tombstones: map[string]time.Time{}}
}

func (s *Service) withSharedState(ctx context.Context, host sdk.Host, call *contractv1.CallContext, work func() error) error {
	s.workflowMu.Lock()
	defer s.workflowMu.Unlock()
	if s.testSave != nil {
		return work()
	}
	repository, err := newAPIExposureStateRepository(host)
	if err != nil {
		return err
	}
	state, revision, err := repository.load(ctx, call)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.state = state
	s.stateSession = &apiExposureStateSession{ctx: ctx, call: call, repository: repository, revision: revision}
	if !s.catalogRestored || s.state.CatalogDirty {
		// Persist the dirty fact before replacing the derived file. A crash can
		// only cause another rebuild, never an untracked catalog publication.
		s.state.CatalogDirty = true
		err = s.saveLocked()
		if err == nil {
			err = s.publishCatalogLocked()
		}
		if err == nil {
			s.state.CatalogDirty = false
			err = s.saveLocked()
		}
		if err == nil {
			s.catalogRestored = true
		}
	}
	s.mu.Unlock()
	if err != nil {
		s.closeStateSession()
		return err
	}
	defer s.closeStateSession()
	return work()
}

func (s *Service) closeStateSession() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stateSession = nil
	s.state = emptyPersistedState()
}
