package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfig"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

// testConnection validates the current form values without publishing a
// definition or creating a long-lived Runtime pool. New password material uses
// a random managed resource whose retirement is persisted before the probe.
func (s *service) testConnection(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenantID string, input defineInput) (databasev1.ProbeResult, error) {
	pool, err := validateDefinitionInput(input)
	if err != nil {
		return databasev1.ProbeResult{}, err
	}
	ref, cleanup, err := s.testCredential(ctx, host, call, tenantID, input)
	if err != nil {
		return databasev1.ProbeResult{}, err
	}
	if cleanup != nil {
		defer func() {
			if cleanup != nil {
				_ = s.cleanupTestCredential(ctx, host, call, tenantID, *cleanup)
			}
		}()
	}
	candidate := definition{
		Name: input.Name, ResourceID: connectionResourceID(tenantID, input.Name), Revision: 1,
		ProviderID: input.Connection.ProviderID, Endpoint: input.Connection.Endpoint, Database: input.Connection.Database,
		Options: append(json.RawMessage(nil), input.Connection.Options...), Pool: pool, CredentialRef: ref,
	}
	if err := databasev1.ValidateConnectionSpec(connectionSpec(candidate)); err != nil {
		return databasev1.ProbeResult{}, err
	}
	var result databasev1.ProbeResult
	if err := callRuntime(ctx, host, call, databasev1.OperationProbe, databasev1.ProbeRequest{Connection: connectionSpec(candidate)}, &result); err != nil {
		return databasev1.ProbeResult{}, err
	}
	if cleanup != nil {
		if err := s.cleanupTestCredential(ctx, host, call, tenantID, *cleanup); err != nil {
			return databasev1.ProbeResult{}, fmt.Errorf("清理临时测试凭证: %w", err)
		}
		cleanup = nil
	}
	return result, nil
}

func (s *service) testCredential(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenantID string, input defineInput) (pluginconfig.ManagedCredentialRef, *testCredentialCleanup, error) {
	if input.CredentialValue == "" {
		s.mu.RLock()
		existing, ok := s.data.Tenants[tenantID][input.Name]
		s.mu.RUnlock()
		if !ok || existing.CredentialRef.Handle == "" {
			return pluginconfig.ManagedCredentialRef{}, nil, errors.New("测试新连接必须输入密码")
		}
		return existing.CredentialRef, nil, nil
	}
	resource, err := randomTestCredentialResource()
	if err != nil {
		return pluginconfig.ManagedCredentialRef{}, nil, err
	}
	var staged pluginconfig.StagedCredential
	if err := callCredential(ctx, host, call, "stageManaged", map[string]string{
		"purpose": "database.connection", "resource": resource, "value": input.CredentialValue,
	}, &staged); err != nil {
		return pluginconfig.ManagedCredentialRef{}, nil, err
	}
	if staged.ID == "" || staged.Ref.Handle == "" || staged.Ref.Owner != id || staged.Ref.Purpose != "database.connection" || staged.Ref.Scope != "tenant" || staged.Ref.Version < 1 {
		_ = callCredential(ctx, host, call, "abortManaged", map[string]string{"stageId": staged.ID}, nil)
		return pluginconfig.ManagedCredentialRef{}, nil, errors.New("凭证插件返回了无效的测试引用")
	}
	cleanup := testCredentialCleanup{StageID: staged.ID, Ref: staged.Ref}
	s.mu.Lock()
	s.data.TestCleanup[tenantID] = append(s.data.TestCleanup[tenantID], cleanup)
	if err := s.save(); err != nil {
		s.data.TestCleanup[tenantID] = s.data.TestCleanup[tenantID][:len(s.data.TestCleanup[tenantID])-1]
		s.mu.Unlock()
		_ = callCredential(ctx, host, call, "abortManaged", map[string]string{"stageId": staged.ID}, nil)
		return pluginconfig.ManagedCredentialRef{}, nil, err
	}
	s.mu.Unlock()
	if err := callCredential(ctx, host, call, "activateManaged", map[string]string{"stageId": staged.ID}, nil); err != nil {
		_ = s.cleanupTestCredential(ctx, host, call, tenantID, cleanup)
		return pluginconfig.ManagedCredentialRef{}, nil, err
	}
	return staged.Ref, &cleanup, nil
}

func (s *service) cleanupTestCredential(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenantID string, item testCredentialCleanup) error {
	abortErr := callCredential(ctx, host, call, "abortManaged", map[string]string{"stageId": item.StageID}, nil)
	if abortErr != nil {
		if retireErr := callCredential(ctx, host, call, "retireManaged", map[string]string{"handle": item.Ref.Handle}, nil); retireErr != nil {
			return errors.Join(abortErr, retireErr)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	queue := s.data.TestCleanup[tenantID]
	previous := append([]testCredentialCleanup(nil), queue...)
	for index := range queue {
		if queue[index].StageID == item.StageID && queue[index].Ref.Handle == item.Ref.Handle {
			s.data.TestCleanup[tenantID] = append(queue[:index], queue[index+1:]...)
			if err := s.save(); err != nil {
				s.data.TestCleanup[tenantID] = previous
				return err
			}
			break
		}
	}
	return nil
}

func randomTestCredentialResource() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return "connection-test-" + hex.EncodeToString(nonce[:]), nil
}
