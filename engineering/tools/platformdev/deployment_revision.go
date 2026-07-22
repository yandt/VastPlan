package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
)

const developmentDeploymentRevisionStateVersion = 1

type developmentDeploymentRevisionState struct {
	Version      int    `json:"version"`
	SourceDigest string `json:"sourceDigest"`
	Revision     uint64 `json:"revision"`
}

// platformManagementSourceDigest fingerprints the stable inputs that produce
// the local platform Deployment. Runtime paths are normalized, while the
// selected listen address remains part of the desired configuration.
func platformManagementSourceDigest(template, portalCatalog []byte, artifactListen string) (string, error) {
	rendered, err := renderPlatformProfile(
		template,
		portalCatalog,
		"/__vastplan_dev_run__",
		"/__vastplan_dev_state__",
		artifactListen,
	)
	if err != nil {
		return "", fmt.Errorf("生成开发平台部署指纹: %w", err)
	}
	profile, err := backendcompositionv1.ParsePlatformProfile(rendered)
	if err != nil {
		return "", fmt.Errorf("解析开发平台部署指纹: %w", err)
	}
	canonical, err := json.Marshal(profile)
	if err != nil {
		return "", fmt.Errorf("规范化开发平台部署指纹: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

// materializeDevelopmentDeploymentRevision gives each distinct local desired
// state a monotonic business revision. It keeps normal restarts idempotent and
// turns source/config changes into a new immutable revision without clearing
// the persistent development control plane.
func materializeDevelopmentDeploymentRevision(rendered []byte, sourceDigest, stateFile string) ([]byte, uint64, error) {
	if err := validateDevelopmentSourceDigest(sourceDigest); err != nil {
		return nil, 0, err
	}
	profile, err := backendcompositionv1.ParsePlatformProfile(rendered)
	if err != nil {
		return nil, 0, fmt.Errorf("解析开发 Platform Profile: %w", err)
	}
	if profile.Revision == 0 {
		return nil, 0, errors.New("开发 Platform Profile revision 必须大于 0")
	}

	state, exists, err := readDevelopmentDeploymentRevisionState(stateFile)
	if err != nil {
		return nil, 0, err
	}
	revision := profile.Revision
	if exists {
		switch {
		case state.SourceDigest == sourceDigest && state.Revision >= revision:
			revision = state.Revision
		case state.Revision == math.MaxUint64:
			return nil, 0, errors.New("开发平台部署 revision 已耗尽")
		case state.Revision >= revision:
			revision = state.Revision + 1
		}
	}
	profile.Revision = revision
	materialized, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return nil, 0, fmt.Errorf("序列化开发 Platform Profile: %w", err)
	}
	materialized = append(materialized, '\n')

	state = developmentDeploymentRevisionState{
		Version: developmentDeploymentRevisionStateVersion, SourceDigest: sourceDigest, Revision: revision,
	}
	if err := writeDevelopmentDeploymentRevisionState(stateFile, state); err != nil {
		return nil, 0, err
	}
	return materialized, revision, nil
}

func validateDevelopmentSourceDigest(digest string) error {
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size {
		return errors.New("开发平台部署 source digest 必须是 SHA-256")
	}
	return nil
}

func readDevelopmentDeploymentRevisionState(path string) (developmentDeploymentRevisionState, bool, error) {
	var state developmentDeploymentRevisionState
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, false, nil
	}
	if err != nil {
		return state, false, fmt.Errorf("读取开发平台部署 revision 状态: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return state, false, fmt.Errorf("解析开发平台部署 revision 状态: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return state, false, errors.New("开发平台部署 revision 状态包含多余内容")
	}
	if state.Version != developmentDeploymentRevisionStateVersion || state.Revision == 0 {
		return state, false, errors.New("开发平台部署 revision 状态版本或 revision 无效")
	}
	if err := validateDevelopmentSourceDigest(state.SourceDigest); err != nil {
		return state, false, fmt.Errorf("开发平台部署 revision 状态无效: %w", err)
	}
	return state, true, nil
}

func writeDevelopmentDeploymentRevisionState(path string, state developmentDeploymentRevisionState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建开发平台部署 revision 状态目录: %w", err)
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".platform-management-revision-*.tmp")
	if err != nil {
		return fmt.Errorf("创建开发平台部署 revision 临时状态: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
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
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("提交开发平台部署 revision 状态: %w", err)
	}
	return nil
}
