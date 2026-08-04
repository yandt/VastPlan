package otpprovider

import (
	"context"
	"encoding/json"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	sharedstatesdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/sharedstate"
)

const controllerStateNamespace = "authentication.otp.configuration.v1"
const controllerStateKey = "state"

func (p *Provider) configureController(configuration Configuration) error {
	values, err := json.Marshal(configuration)
	if err != nil {
		return err
	}
	digest, err := configurationv1.DigestConfiguration(values, nil)
	if err != nil {
		return err
	}
	p.controller = controllerState{FormatVersion: controllerStateVersion, Active: controllerConfiguration{Revision: 1, Digest: digest, Values: values, Configuration: configuration}}
	return nil
}

func (p *Provider) ensureControllerState(ctx context.Context, host sdk.Host, call *contractv1.CallContext) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.loadControllerStateLocked(ctx, host, call)
}

func (p *Provider) loadControllerStateLocked(ctx context.Context, host sdk.Host, call *contractv1.CallContext) error {
	if p.controllerLoaded {
		return nil
	}
	client, err := sharedstatesdk.NewFenced(host, "service", controllerStateNamespace)
	if err != nil {
		return err
	}
	entry, err := client.Get(ctx, call, controllerStateKey)
	if sharedstatesdk.IsNotFound(err) {
		p.controllerLoaded = true
		return nil
	}
	if err != nil {
		return err
	}
	if len(entry.Value) == 0 || len(entry.Value) > maxControllerStateSize {
		return errors.New("OTP configuration controller Shared State 大小无效")
	}
	var state controllerState
	if err := json.Unmarshal(entry.Value, &state); err != nil {
		return err
	}
	if err := validateControllerState(state); err != nil {
		return err
	}
	p.controller, p.controllerRevision, p.controllerLoaded = state, entry.Revision, true
	p.profiles = cloneProfiles(state.Active.Configuration.Profiles)
	return nil
}

func validateControllerState(state controllerState) error {
	if state.FormatVersion != controllerStateVersion || state.Active.Revision == 0 || !commonv1.IsSHA256(state.Active.Digest) || !json.Valid(state.Active.Values) {
		return errors.New("OTP configuration controller 状态身份无效")
	}
	configuration, err := state.Active.Configuration.normalized()
	if err != nil {
		return errors.New("OTP configuration controller Active 配置无效")
	}
	values, _ := json.Marshal(configuration)
	digest, err := configurationv1.DigestConfiguration(values, nil)
	if err != nil || digest != state.Active.Digest || !jsonEqual(values, state.Active.Values) {
		return errors.New("OTP configuration controller Active 摘要无效")
	}
	if state.ConfigurationID != "" && !commonv1.IsPrefixedLowerHex(state.ConfigurationID, "cfg_", 24) {
		return errors.New("OTP configuration controller 配置身份无效")
	}
	if (state.SchemaDigest == "") != (state.ArtifactSHA256 == "") || state.SchemaDigest != "" && (!commonv1.IsSHA256(state.SchemaDigest) || !commonv1.IsSHA256(state.ArtifactSHA256)) {
		return errors.New("OTP configuration controller 制品绑定无效")
	}
	if state.Candidate == nil {
		return nil
	}
	candidate := state.Candidate
	if !commonv1.IsPrefixedLowerHex(candidate.CandidateID, "pcfg_", 32) || !commonv1.IsSHA256(candidate.RequestDigest) || !commonv1.IsSHA256(candidate.ConfigurationDigest) || !json.Valid(candidate.Values) {
		return errors.New("OTP configuration controller Candidate 身份无效")
	}
	switch candidate.Status {
	case configurationv1.StatusPrepared, configurationv1.StatusCommitted, configurationv1.StatusAborted:
	default:
		return errors.New("OTP configuration controller Candidate 状态无效")
	}
	if candidate.Status == configurationv1.StatusAborted && candidate.Ready {
		return errors.New("OTP configuration controller Aborted Candidate 不得 Ready")
	}
	configuration, err = candidate.Configuration.normalized()
	if err != nil {
		return errors.New("OTP configuration controller Candidate 配置无效")
	}
	values, _ = json.Marshal(configuration)
	digest, err = configurationv1.DigestConfiguration(values, nil)
	if err != nil || digest != candidate.ConfigurationDigest || !jsonEqual(values, candidate.Values) {
		return errors.New("OTP configuration controller Candidate 摘要无效")
	}
	if candidate.Status == configurationv1.StatusPrepared && !candidate.Ready {
		return errors.New("OTP configuration controller Prepared Candidate 必须 Ready")
	}
	if candidate.Status == configurationv1.StatusCommitted && (state.Active.Digest != candidate.ConfigurationDigest || !candidate.Ready) {
		return errors.New("OTP configuration controller Committed Candidate 未成为 Active")
	}
	return nil
}

func (p *Provider) saveControllerStateLocked(ctx context.Context, host sdk.Host, call *contractv1.CallContext) error {
	raw, err := json.Marshal(p.controller)
	if err != nil || len(raw) > maxControllerStateSize {
		return errors.New("OTP configuration controller 状态过大")
	}
	client, err := sharedstatesdk.NewFenced(host, "service", controllerStateNamespace)
	if err != nil {
		return err
	}
	var entry sharedstatesdk.Entry
	if p.controllerRevision == 0 {
		entry, err = client.Create(ctx, call, controllerStateKey, raw)
	} else {
		entry, err = client.Update(ctx, call, controllerStateKey, raw, p.controllerRevision)
	}
	if err != nil {
		return err
	}
	p.controllerRevision, p.controllerLoaded = entry.Revision, true
	return nil
}
