// Package nativepolicy implements the first-party bounded Approval Policy
// interpreter. It belongs to the Provider plugin, never to the generic SDK.
package nativepolicy

import (
	"errors"
	"fmt"
	"sort"

	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
)

type compiledProfile struct {
	profile approvalv2.PolicyProfile
	ref     approvalv2.ProfileRef
}

type Provider struct {
	profiles map[approvalv2.ProfileRef]compiledProfile
}

func New(profiles []approvalv2.PolicyProfile) (*Provider, error) {
	if len(profiles) == 0 || len(profiles) > 256 {
		return nil, errors.New("Native Approval Provider profiles 数量必须为 1 至 256")
	}
	provider := &Provider{profiles: make(map[approvalv2.ProfileRef]compiledProfile, len(profiles))}
	seenRevision := map[string]struct{}{}
	for _, profile := range profiles {
		ref, err := approvalv2.RefForProfile(profile)
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s@%d", ref.ID, ref.Revision)
		if _, exists := seenRevision[key]; exists {
			return nil, fmt.Errorf("Native Approval Profile ID/revision 重复: %s", key)
		}
		seenRevision[key] = struct{}{}
		profile.Rules = sortedRules(profile.Rules)
		provider.profiles[ref] = compiledProfile{profile: profile, ref: ref}
	}
	return provider, nil
}

func (p *Provider) Evaluate(request approvalv2.EvaluateRequest) (approvalv2.Decision, error) {
	if err := approvalv2.ValidateProfileRef(request.Profile); err != nil {
		return approvalv2.Decision{}, err
	}
	if err := approvalv2.ValidateInput(request.Input); err != nil {
		return approvalv2.Decision{}, err
	}
	profile, ok := p.profiles[request.Profile]
	if !ok {
		return approvalv2.Decision{}, errors.New("Approval Policy Profile 不存在或摘要不匹配")
	}
	return evaluate(profile, request.Input)
}

func (p *Provider) EvaluateBatch(request approvalv2.EvaluateBatchRequest) ([]approvalv2.Decision, error) {
	if len(request.Inputs) == 0 || len(request.Inputs) > 256 {
		return nil, errors.New("Approval Policy batch 数量必须为 1 至 256")
	}
	decisions := make([]approvalv2.Decision, 0, len(request.Inputs))
	for _, input := range request.Inputs {
		decision, err := p.Evaluate(approvalv2.EvaluateRequest{Profile: request.Profile, Input: input})
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, decision)
	}
	return decisions, nil
}

func (p *Provider) Health() approvalv2.HealthResult {
	refs := make([]approvalv2.ProfileRef, 0, len(p.profiles))
	for ref := range p.profiles {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ID != refs[j].ID {
			return refs[i].ID < refs[j].ID
		}
		return refs[i].Revision < refs[j].Revision
	})
	return approvalv2.HealthResult{Ready: true, Profiles: refs}
}

func sortedRules(rules []approvalv2.Rule) []approvalv2.Rule {
	result := append([]approvalv2.Rule(nil), rules...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority > result[j].Priority
		}
		return result[i].ID < result[j].ID
	})
	return result
}
