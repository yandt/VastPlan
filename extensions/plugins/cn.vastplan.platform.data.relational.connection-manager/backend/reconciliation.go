package main

import (
	"context"
	"errors"
	"fmt"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfig"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *service) reconcilePublications(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenantID string) error {
	s.mu.RLock()
	items := make(map[string]runtimePublication, len(s.data.Publications[tenantID]))
	for name, item := range s.data.Publications[tenantID] {
		items[name] = item
	}
	s.mu.RUnlock()
	for name, item := range items {
		var err error
		switch item.Action {
		case databasev1.OperationActivate:
			err = callRuntime(ctx, host, call, databasev1.OperationActivate, databasev1.ActivateRequest{Connection: connectionSpec(item.Connection)}, nil)
		case databasev1.OperationRetire:
			err = callRuntime(ctx, host, call, databasev1.OperationRetire, databasev1.RetireRequest{Connection: connectionSpec(item.Connection).Ref}, nil)
		default:
			err = errors.New("数据库 Runtime publication action 无效")
		}
		if err != nil {
			return fmt.Errorf("发布数据库连接 %q: %w", name, err)
		}
		s.mu.Lock()
		current, ok := s.publications(tenantID)[name]
		if ok && current.Action == item.Action && current.Connection.Revision == item.Connection.Revision {
			delete(s.publications(tenantID), name)
			retireLength := len(s.data.Retire[tenantID])
			if item.RetireCredential != nil && item.RetireCredential.Handle != "" {
				s.data.Retire[tenantID] = append(s.data.Retire[tenantID], *item.RetireCredential)
			}
			if saveErr := s.save(); saveErr != nil {
				s.publications(tenantID)[name] = current
				s.data.Retire[tenantID] = s.data.Retire[tenantID][:retireLength]
				s.mu.Unlock()
				return saveErr
			}
		}
		s.mu.Unlock()
	}
	return nil
}

func (s *service) reconcilePending(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenantID string) error {
	s.mu.RLock()
	items := make(map[string]pendingDefinition, len(s.data.Pending[tenantID]))
	for name, item := range s.data.Pending[tenantID] {
		items[name] = item
	}
	s.mu.RUnlock()
	for name, item := range items {
		if err := callCredential(ctx, host, call, "activateManaged", map[string]string{"stageId": item.Staged.ID}, nil); err != nil {
			return fmt.Errorf("恢复数据库连接 %q 的凭证候选: %w", name, err)
		}
		s.mu.Lock()
		current, ok := s.pending(tenantID)[name]
		if ok && current.Staged.ID == item.Staged.ID {
			old, oldExists := s.definitions(tenantID)[name]
			oldPublication, publicationExists := s.publications(tenantID)[name]
			s.definitions(tenantID)[name] = item.Desired
			delete(s.pending(tenantID), name)
			publication := runtimePublication{Action: databasev1.OperationActivate, Connection: item.Desired}
			if item.Previous != nil && item.Previous.CredentialRef.Handle != "" {
				ref := item.Previous.CredentialRef
				publication.RetireCredential = &ref
			}
			s.publications(tenantID)[name] = publication
			if err := s.save(); err != nil {
				s.pending(tenantID)[name] = current
				if publicationExists {
					s.publications(tenantID)[name] = oldPublication
				} else {
					delete(s.publications(tenantID), name)
				}
				if oldExists {
					s.definitions(tenantID)[name] = old
				} else {
					delete(s.definitions(tenantID), name)
				}
				s.mu.Unlock()
				return err
			}
		}
		s.mu.Unlock()
	}
	return nil
}

func (s *service) reconcileRetire(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenantID string) error {
	s.mu.RLock()
	refs := append([]pluginconfig.ManagedCredentialRef(nil), s.data.Retire[tenantID]...)
	s.mu.RUnlock()
	for _, ref := range refs {
		if err := callCredential(ctx, host, call, "retireManaged", map[string]string{"handle": ref.Handle}, nil); err != nil {
			return err
		}
		s.mu.Lock()
		queued := s.data.Retire[tenantID]
		for index := range queued {
			if queued[index].Handle == ref.Handle {
				s.data.Retire[tenantID] = append(queued[:index], queued[index+1:]...)
				break
			}
		}
		if err := s.save(); err != nil {
			s.mu.Unlock()
			return err
		}
		s.mu.Unlock()
	}
	return nil
}
