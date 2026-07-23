package otpprovider

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
)

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
	p.stateFile = strings.TrimSpace(configuration.StateFile)
	if p.stateFile == "" {
		return nil
	}
	if !filepath.IsAbs(p.stateFile) || filepath.Clean(p.stateFile) != p.stateFile {
		return errors.New("OTP configuration controller stateFile 必须是规范绝对路径")
	}
	if err := p.loadControllerState(); err != nil {
		return err
	}
	p.profiles = cloneProfiles(p.controller.Active.Configuration.Profiles)
	return nil
}

func (p *Provider) loadControllerState() error {
	info, err := os.Lstat(p.stateFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() <= 0 || info.Size() > maxControllerStateSize {
		return errors.New("OTP configuration controller 状态必须是仅属主可访问且大小受限的普通文件")
	}
	raw, err := os.ReadFile(p.stateFile)
	if err != nil {
		return err
	}
	var state controllerState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("解析 OTP configuration controller 状态: %w", err)
	}
	if err := validateControllerState(state, p.stateFile); err != nil {
		return err
	}
	p.controller = state
	return nil
}

func validateControllerState(state controllerState, stateFile string) error {
	if state.FormatVersion != controllerStateVersion || state.Active.Revision == 0 || !validHex(state.Active.Digest, 64) || !json.Valid(state.Active.Values) {
		return errors.New("OTP configuration controller 状态身份无效")
	}
	configuration, err := state.Active.Configuration.normalized()
	if err != nil || configuration.StateFile != stateFile {
		return errors.New("OTP configuration controller Active 配置无效")
	}
	values, _ := json.Marshal(configuration)
	digest, err := configurationv1.DigestConfiguration(values, nil)
	if err != nil || digest != state.Active.Digest || !jsonEqual(values, state.Active.Values) {
		return errors.New("OTP configuration controller Active 摘要无效")
	}
	if state.ConfigurationID != "" && !validPrefixedHex(state.ConfigurationID, "cfg_", 24) {
		return errors.New("OTP configuration controller 配置身份无效")
	}
	if (state.SchemaDigest == "") != (state.ArtifactSHA256 == "") || (state.SchemaDigest != "" && (!validHex(state.SchemaDigest, 64) || !validHex(state.ArtifactSHA256, 64))) {
		return errors.New("OTP configuration controller 制品绑定无效")
	}
	if state.Candidate == nil {
		return nil
	}
	candidate := state.Candidate
	if !validPrefixedHex(candidate.CandidateID, "pcfg_", 32) || !validHex(candidate.RequestDigest, 64) || !validHex(candidate.ConfigurationDigest, 64) || !json.Valid(candidate.Values) {
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
	if err != nil || configuration.StateFile != stateFile {
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

func (p *Provider) saveControllerStateLocked() error {
	if p.stateFile == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p.stateFile), 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(filepath.Dir(p.stateFile))
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o022 != 0 {
		return errors.New("OTP configuration controller 状态目录不安全")
	}
	raw, err := json.Marshal(p.controller)
	if err != nil || len(raw) > maxControllerStateSize {
		return errors.New("OTP configuration controller 状态过大")
	}
	temporary, err := os.CreateTemp(filepath.Dir(p.stateFile), ".otp-configuration-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, p.stateFile); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(p.stateFile))
	if err != nil {
		return err
	}
	syncErr, closeErr := directory.Sync(), directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
