package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfig"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *service) define(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenantID string, input defineInput) (definition, error) {
	pool, err := validateDefinitionInput(input)
	if err != nil {
		return definition{}, err
	}

	s.mu.RLock()
	old, exists := s.data.Tenants[tenantID][input.Name]
	identity, identityExists := s.data.Revisions[tenantID][input.Name]
	s.mu.RUnlock()
	revision, resourceID := uint64(1), connectionResourceID(tenantID, input.Name)
	if identityExists {
		revision, resourceID = identity.LastRevision+1, identity.ResourceID
	} else if exists {
		revision, resourceID = old.Revision+1, old.ResourceID
	}
	makeDesired := func(ref pluginconfig.ManagedCredentialRef) (definition, error) {
		desired := definition{
			Name: input.Name, ResourceID: resourceID, Revision: revision, ProviderID: input.Connection.ProviderID,
			Endpoint: input.Connection.Endpoint, Database: input.Connection.Database, Options: append(json.RawMessage(nil), input.Connection.Options...),
			Pool: pool, CredentialRef: ref,
		}
		if err := databasev1.ValidateConnectionSpec(connectionSpec(desired)); err != nil {
			return definition{}, err
		}
		return desired, nil
	}
	if input.CredentialValue == "" {
		if !exists || old.CredentialRef.Handle == "" {
			return definition{}, errors.New("新连接必须在当前页面输入凭证")
		}
		updated, err := makeDesired(old.CredentialRef)
		if err != nil {
			return definition{}, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.definitions(tenantID)[input.Name] = updated
		previousIdentity, previousIdentityExists := s.revisions(tenantID)[input.Name]
		s.revisions(tenantID)[input.Name] = connectionIdentity{ResourceID: updated.ResourceID, LastRevision: updated.Revision}
		previousPublication, publicationExists := s.publications(tenantID)[input.Name]
		s.publications(tenantID)[input.Name] = runtimePublication{Action: databasev1.OperationActivate, Connection: updated}
		if err := s.save(); err != nil {
			s.definitions(tenantID)[input.Name] = old
			if previousIdentityExists {
				s.revisions(tenantID)[input.Name] = previousIdentity
			} else {
				delete(s.revisions(tenantID), input.Name)
			}
			if publicationExists {
				s.publications(tenantID)[input.Name] = previousPublication
			} else {
				delete(s.publications(tenantID), input.Name)
			}
			return definition{}, err
		}
		return updated, nil
	}

	var staged pluginconfig.StagedCredential
	if err := callCredential(ctx, host, call, "stageManaged", map[string]string{
		"purpose": "database.connection", "resource": input.Name, "value": input.CredentialValue,
	}, &staged); err != nil {
		return definition{}, err
	}
	if staged.ID == "" || staged.Ref.Handle == "" || staged.Ref.Owner != id || staged.Ref.Purpose != "database.connection" || staged.Ref.Scope != "tenant" || staged.Ref.Version < 1 {
		_ = callCredential(ctx, host, call, "abortManaged", map[string]string{"stageId": staged.ID}, nil)
		return definition{}, errors.New("凭证插件返回了不符合当前业务插件边界的引用")
	}
	desired, err := makeDesired(staged.Ref)
	if err != nil {
		_ = callCredential(ctx, host, call, "abortManaged", map[string]string{"stageId": staged.ID}, nil)
		return definition{}, err
	}
	pending := pendingDefinition{Desired: desired, Staged: staged}
	if exists {
		previous := old
		pending.Previous = &previous
	}
	s.mu.Lock()
	s.pending(tenantID)[input.Name] = pending
	previousIdentity, previousIdentityExists := s.revisions(tenantID)[input.Name]
	s.revisions(tenantID)[input.Name] = connectionIdentity{ResourceID: desired.ResourceID, LastRevision: desired.Revision}
	if err := s.save(); err != nil {
		delete(s.pending(tenantID), input.Name)
		if previousIdentityExists {
			s.revisions(tenantID)[input.Name] = previousIdentity
		} else {
			delete(s.revisions(tenantID), input.Name)
		}
		s.mu.Unlock()
		_ = callCredential(ctx, host, call, "abortManaged", map[string]string{"stageId": staged.ID}, nil)
		return definition{}, err
	}
	s.mu.Unlock()
	if err := callCredential(ctx, host, call, "activateManaged", map[string]string{"stageId": staged.ID}, nil); err != nil {
		return definition{}, err
	}
	s.mu.Lock()
	s.definitions(tenantID)[input.Name] = desired
	delete(s.pending(tenantID), input.Name)
	previousPublication, publicationExists := s.publications(tenantID)[input.Name]
	publication := runtimePublication{Action: databasev1.OperationActivate, Connection: desired}
	if exists && old.CredentialRef.Handle != "" {
		ref := old.CredentialRef
		publication.RetireCredential = &ref
	}
	s.publications(tenantID)[input.Name] = publication
	if err := s.save(); err != nil {
		s.pending(tenantID)[input.Name] = pending
		if publicationExists {
			s.publications(tenantID)[input.Name] = previousPublication
		} else {
			delete(s.publications(tenantID), input.Name)
		}
		if exists {
			s.definitions(tenantID)[input.Name] = old
		} else {
			delete(s.definitions(tenantID), input.Name)
		}
		s.mu.Unlock()
		return definition{}, err
	}
	s.mu.Unlock()
	return desired, nil
}

func validateDefinitionInput(input defineInput) (databasev1.PoolPolicy, error) {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 160 || len(input.CredentialValue) > 4<<20 {
		return databasev1.PoolPolicy{}, errors.New("数据库连接字段为空或超过长度上限")
	}
	if err := databasev1.ValidateConnectionCandidate(input.Connection); err != nil {
		return databasev1.PoolPolicy{}, err
	}
	return input.Connection.Pool, nil
}
