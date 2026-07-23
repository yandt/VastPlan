package pluginsettings

import (
	"context"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type interruptedCandidate struct {
	tenant    string
	candidate pluginconfiguration.Candidate
	stages    []credentialStage
}

func (s *Service) recoverInterrupted(ctx context.Context, host sdk.Host, call *contractv1.CallContext) error {
	tenant, actor, err := tenantAndActor(call)
	if err != nil {
		return err
	}
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	interrupted := make([]interruptedCandidate, 0)
	for _, candidate := range state.Candidates {
		if candidate.Status == pluginconfiguration.CandidatePreparing || candidate.Status == pluginconfiguration.CandidateRollingBack {
			interrupted = append(interrupted, interruptedCandidate{tenant: tenant, candidate: cloneCandidate(candidate), stages: cloneStages(state.CredentialStages[candidate.ID])})
		}
	}
	s.mu.Unlock()
	for _, item := range interrupted {
		switch item.candidate.Status {
		case pluginconfiguration.CandidatePreparing:
			if allCredentialFieldsCheckpointed(item.candidate) {
				if _, err := s.finishPreparing(item.tenant, item.candidate.ID, actor); err != nil {
					return err
				}
				continue
			}
			abortErr := abortCredentialStages(ctx, host, call, item.candidate.ID, item.stages)
			if err := s.failPreparing(item.tenant, item.candidate.ID, actor, abortErr); err != nil {
				return errors.Join(abortErr, err)
			}
		case pluginconfiguration.CandidateRollingBack:
			if err := abortCredentialStages(ctx, host, call, item.candidate.ID, item.stages); err != nil {
				return err
			}
			if _, err := s.completeRollback(item.tenant, item.candidate.ID, actor); err != nil {
				return err
			}
		}
	}
	return nil
}

func allCredentialFieldsCheckpointed(candidate pluginconfiguration.Candidate) bool {
	for _, field := range candidate.ManagedCredentials {
		if field.State == "Pending" {
			return false
		}
	}
	return len(candidate.ManagedCredentials) > 0
}
