package otpprovider

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	configurationcontrollersdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/configurationcontroller"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	controllerStateVersion = 1
	maxControllerStateSize = 2 << 20
)

type controllerConfiguration struct {
	Revision      uint64          `json:"revision"`
	Digest        string          `json:"digest"`
	Values        json.RawMessage `json:"values"`
	Configuration Configuration   `json:"configuration"`
}

type controllerCandidate struct {
	CandidateID         string                          `json:"candidateId"`
	RequestDigest       string                          `json:"requestDigest"`
	ConfigurationDigest string                          `json:"configurationDigest"`
	Status              configurationv1.CandidateStatus `json:"status"`
	Ready               bool                            `json:"ready"`
	Values              json.RawMessage                 `json:"values"`
	Configuration       Configuration                   `json:"configuration"`
}

type controllerState struct {
	FormatVersion   int                     `json:"formatVersion"`
	ConfigurationID string                  `json:"configurationId,omitempty"`
	SchemaDigest    string                  `json:"schemaDigest,omitempty"`
	ArtifactSHA256  string                  `json:"artifactSha256,omitempty"`
	Active          controllerConfiguration `json:"active"`
	Candidate       *controllerCandidate    `json:"candidate,omitempty"`
}

func (p *Provider) Prepare(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, request configurationv1.PrepareRequest) (configurationv1.Observation, error) {
	normalizedRequest, err := configurationv1.NormalizePrepareRequest(request)
	if err != nil || len(normalizedRequest.ManagedCredentials) != 0 {
		return configurationv1.Observation{}, errors.New("OTP configuration controller 不接受托管凭证字段")
	}
	requestDigest, err := configurationv1.DigestPrepareRequest(normalizedRequest)
	if err != nil {
		return configurationv1.Observation{}, err
	}
	var configuration Configuration
	if err := json.Unmarshal(normalizedRequest.Values, &configuration); err != nil {
		return configurationv1.Observation{}, err
	}
	configuration, err = configuration.normalized()
	if err != nil {
		return configurationv1.Observation{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if configuration.StateFile != p.controller.Active.Configuration.StateFile || configuration.Capacity != p.controller.Active.Configuration.Capacity {
		return configurationv1.Observation{}, errors.New("OTP stateFile 与 capacity 是不可热变更的运行参数")
	}
	if p.controller.ConfigurationID != "" && p.controller.ConfigurationID != request.ConfigurationID {
		return configurationv1.Observation{}, errors.New("OTP configuration controller 已绑定其他配置资源")
	}
	if existing := p.controller.Candidate; existing != nil && existing.CandidateID == request.CandidateID {
		if existing.RequestDigest != requestDigest {
			return configurationv1.Observation{}, errors.New("OTP configuration controller Candidate 请求摘要冲突")
		}
		return p.observationLocked(), nil
	}
	if request.ExpectedActive.Revision != p.controller.Active.Revision || request.ExpectedActive.Digest != p.controller.Active.Digest {
		return configurationv1.Observation{}, errors.New("OTP configuration controller Active CAS 冲突")
	}
	if existing := p.controller.Candidate; existing != nil && existing.Status == configurationv1.StatusPrepared {
		return configurationv1.Observation{}, errors.New("OTP configuration controller 已有待提交 Candidate")
	}
	values, _ := json.Marshal(configuration)
	configurationDigest, err := configurationv1.DigestConfiguration(values, nil)
	if err != nil {
		return configurationv1.Observation{}, err
	}
	previous := cloneControllerState(p.controller)
	p.controller.ConfigurationID, p.controller.SchemaDigest, p.controller.ArtifactSHA256 = request.ConfigurationID, request.SchemaDigest, request.ArtifactSHA256
	p.controller.Candidate = &controllerCandidate{
		CandidateID: request.CandidateID, RequestDigest: requestDigest, ConfigurationDigest: configurationDigest,
		Status: configurationv1.StatusPrepared, Ready: true, Values: values, Configuration: configuration,
	}
	if err := p.saveControllerStateLocked(); err != nil {
		p.controller = previous
		return configurationv1.Observation{}, err
	}
	return p.observationLocked(), nil
}

func (p *Provider) Commit(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, request configurationv1.CandidateRequest) (configurationv1.Observation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	candidate, err := p.boundCandidateLocked(request)
	if err != nil {
		return configurationv1.Observation{}, err
	}
	if candidate.Status == configurationv1.StatusCommitted {
		return p.observationLocked(), nil
	}
	if candidate.Status != configurationv1.StatusPrepared || !candidate.Ready {
		return configurationv1.Observation{}, errors.New("OTP configuration controller Candidate 尚未 Ready")
	}
	previous := cloneControllerState(p.controller)
	p.controller.Active = controllerConfiguration{
		Revision: p.controller.Active.Revision + 1, Digest: candidate.ConfigurationDigest,
		Values: append(json.RawMessage(nil), candidate.Values...), Configuration: candidate.Configuration,
	}
	p.controller.Candidate.Status = configurationv1.StatusCommitted
	if err := p.saveControllerStateLocked(); err != nil {
		p.controller = previous
		return configurationv1.Observation{}, err
	}
	p.profiles = cloneProfiles(candidate.Configuration.Profiles)
	return p.observationLocked(), nil
}

func (p *Provider) Abort(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, request configurationv1.CandidateRequest) (configurationv1.Observation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	candidate, err := p.boundCandidateLocked(request)
	if err != nil {
		return configurationv1.Observation{}, err
	}
	if candidate.Status == configurationv1.StatusAborted {
		return p.observationLocked(), nil
	}
	if candidate.Status != configurationv1.StatusPrepared {
		return configurationv1.Observation{}, errors.New("已提交 OTP configuration Candidate 不得 abort")
	}
	previous := cloneControllerState(p.controller)
	p.controller.Candidate.Status, p.controller.Candidate.Ready = configurationv1.StatusAborted, false
	if err := p.saveControllerStateLocked(); err != nil {
		p.controller = previous
		return configurationv1.Observation{}, err
	}
	return p.observationLocked(), nil
}

func (p *Provider) Status(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, request configurationv1.StatusRequest) (configurationv1.Observation, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.controller.ConfigurationID != "" && p.controller.ConfigurationID != request.ConfigurationID {
		return configurationv1.Observation{}, errors.New("OTP configuration controller 配置资源不匹配")
	}
	if request.CandidateID != "" {
		if _, err := p.boundCandidateLocked(configurationv1.CandidateRequest{CandidateID: request.CandidateID, RequestDigest: request.RequestDigest}); err != nil {
			return configurationv1.Observation{}, err
		}
	}
	return p.observationForConfigurationLocked(request.ConfigurationID), nil
}

func (p *Provider) boundCandidateLocked(request configurationv1.CandidateRequest) (*controllerCandidate, error) {
	candidate := p.controller.Candidate
	if candidate == nil || candidate.CandidateID != request.CandidateID || candidate.RequestDigest != request.RequestDigest {
		return nil, errors.New("OTP configuration controller Candidate 不存在或摘要不匹配")
	}
	return candidate, nil
}

func (p *Provider) observationLocked() configurationv1.Observation {
	return p.observationForConfigurationLocked(p.controller.ConfigurationID)
}

func (p *Provider) observationForConfigurationLocked(configurationID string) configurationv1.Observation {
	observation := configurationv1.Observation{
		Protocol: configurationv1.Protocol, ConfigurationID: configurationID,
		Active: configurationv1.ActiveReference{Revision: p.controller.Active.Revision, Digest: p.controller.Active.Digest}, ObservedAt: p.now(),
	}
	if candidate := p.controller.Candidate; candidate != nil {
		observation.Candidate = &configurationv1.CandidateObservation{
			CandidateID: candidate.CandidateID, RequestDigest: candidate.RequestDigest, ConfigurationDigest: candidate.ConfigurationDigest,
			Status: candidate.Status, Ready: candidate.Ready,
		}
	}
	return observation
}

func (p *Provider) ConfigurationContribution() (sdk.Contribution, error) {
	return configurationcontrollersdk.Contribution(PluginID, p)
}

func cloneControllerState(state controllerState) controllerState {
	raw, _ := json.Marshal(state)
	var clone controllerState
	_ = json.Unmarshal(raw, &clone)
	return clone
}

func cloneProfiles(profiles map[string]Profile) map[string]Profile {
	raw, _ := json.Marshal(profiles)
	clone := map[string]Profile{}
	_ = json.Unmarshal(raw, &clone)
	for id, profile := range clone {
		profile.maxResends = 3
		if profile.MaxResends != nil {
			profile.maxResends = *profile.MaxResends
		}
		clone[id] = profile
	}
	return clone
}

func jsonEqual(left, right []byte) bool {
	var a, b any
	return json.Unmarshal(left, &a) == nil && json.Unmarshal(right, &b) == nil && reflect.DeepEqual(a, b)
}

func validPrefixedHex(value, prefix string, length int) bool {
	return strings.HasPrefix(value, prefix) && validHex(value[len(prefix):], length)
}

func validHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
